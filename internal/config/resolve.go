package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/secret"
)

// DefaultURL is used when no URL is configured anywhere.
const DefaultURL = "https://api.euwest1.obs.mycentreon.com"

const tokenCommandTimeout = 15 * time.Second

// Overrides are values given on the command line; they win over everything else.
type Overrides struct {
	ConfigPath string
	Context    string
	URL        string
	Org        string
}

// Resolved is the effective configuration used to reach the platform.
type Resolved struct {
	ConfigPath  string
	ConfigFound bool
	Context     string // name of the context in use, empty when none
	URL         string
	Org         string
	Token       secret.String

	// Where each value comes from, for `lazyobs config view`.
	URLSource, OrgSource, TokenSource string

	// Warnings are non-fatal issues, such as a token file readable by others.
	Warnings []string
}

// Env abstracts the process environment for tests.
type Env struct {
	Getenv func(string) string
	// RunCommand runs a token command and returns its standard output.
	RunCommand func(ctx context.Context, command string) ([]byte, error)
}

// OSEnv is the real process environment.
var OSEnv = Env{Getenv: os.Getenv, RunCommand: runShell}

// ErrNotConfigured means no organization or token could be found.
var ErrNotConfigured = errors.New("lazyobs is not configured")

// Resolve computes the effective configuration.
// Precedence: command-line flags, then environment variables, then the context.
func Resolve(ctx context.Context, o Overrides, env Env) (*Resolved, error) {
	r := &Resolved{ConfigPath: Path(o, env.Getenv)}

	file, found, err := ReadFile(r.ConfigPath)
	if err != nil {
		return nil, err
	}
	r.ConfigFound = found

	r.Context = firstNonEmpty(o.Context, env.Getenv(EnvContext), file.CurrentContext)
	var cur Context
	if r.Context != "" {
		c, ok := file.Contexts[r.Context]
		switch {
		case ok:
			cur = c
		case o.Context != "" || env.Getenv(EnvContext) != "":
			return nil, fmt.Errorf("context %q not found in %s (available: %s)", r.Context, r.ConfigPath, strings.Join(file.Names(), ", "))
		default:
			r.Warnings = append(r.Warnings, fmt.Sprintf("current-context %q is not defined in %s", r.Context, r.ConfigPath))
			r.Context = ""
		}
	}
	ctxSource := "context " + r.Context

	r.URL, r.URLSource = pick(
		value{o.URL, "flag --url"}, value{env.Getenv(EnvURL), "env " + EnvURL},
		value{cur.URL, ctxSource}, value{DefaultURL, "default"})
	r.Org, r.OrgSource = pick(
		value{o.Org, "flag --org"}, value{env.Getenv(EnvOrg), "env " + EnvOrg}, value{cur.Org, ctxSource})

	if tok := env.Getenv(EnvToken); tok != "" {
		r.Token, r.TokenSource = secret.New(strings.TrimSpace(tok)), "env "+EnvToken
	} else if err := r.loadContextToken(ctx, cur, env); err != nil {
		return nil, err
	}

	if r.Org == "" || r.Token.IsZero() {
		var missing []string
		if r.Org == "" {
			missing = append(missing, "organization")
		}
		if r.Token.IsZero() {
			missing = append(missing, "API token")
		}
		return r, fmt.Errorf("%w: missing %s; set %s/%s or create a context with `lazyobs config set-context`",
			ErrNotConfigured, strings.Join(missing, " and "), EnvOrg, EnvToken)
	}
	return r, nil
}

func (r *Resolved) loadContextToken(ctx context.Context, cur Context, env Env) error {
	sources := 0
	for _, set := range []bool{!cur.Token.IsZero(), cur.TokenFile != "", cur.TokenCommand != ""} {
		if set {
			sources++
		}
	}
	if sources > 1 {
		return fmt.Errorf("context %q sets more than one of token, token-file and token-command", r.Context)
	}

	switch {
	case !cur.Token.IsZero():
		r.Token, r.TokenSource = cur.Token, "context "+r.Context+" (inline)"
		if open, mode := tooOpen(r.ConfigPath); open {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s holds a token but has mode %04o; run: chmod 600 %s", r.ConfigPath, mode, r.ConfigPath))
		}
	case cur.TokenFile != "":
		path := ExpandHome(cur.TokenFile)
		b, err := os.ReadFile(path) //nolint:gosec // path chosen by the user
		if err != nil {
			return fmt.Errorf("context %q: read token-file: %w", r.Context, err)
		}
		r.Token, r.TokenSource = secret.New(strings.TrimSpace(string(b))), "token-file "+path
		if open, mode := tooOpen(path); open {
			r.Warnings = append(r.Warnings, fmt.Sprintf("token file %s has mode %04o; run: chmod 600 %s", path, mode, path))
		}
	case cur.TokenCommand != "":
		cctx, cancel := context.WithTimeout(ctx, tokenCommandTimeout)
		defer cancel()
		out, err := env.RunCommand(cctx, cur.TokenCommand)
		if err != nil {
			// Never include the command output: it may contain the token.
			return fmt.Errorf("context %q: token-command failed: %w", r.Context, err)
		}
		r.Token, r.TokenSource = secret.New(strings.TrimSpace(string(out))), "token-command"
	}
	return nil
}

type value struct{ v, source string }

func pick(values ...value) (v, source string) {
	for _, c := range values {
		if c.v != "" {
			return c.v, c.source
		}
	}
	return "", ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func runShell(ctx context.Context, command string) ([]byte, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command) //nolint:gosec // token-command is user configuration, run by design
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command) //nolint:gosec // token-command is user configuration, run by design
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr // lets password managers prompt the user
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}
