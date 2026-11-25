package config_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CentreonLabs/lazyobs/internal/config"
)

const fileToken = "file-token-value"

// env builds a fake environment from a map.
func env(vars map[string]string) config.Env {
	return config.Env{
		Getenv: func(k string) string { return vars[k] },
		RunCommand: func(_ context.Context, cmd string) ([]byte, error) {
			if cmd == "fail" {
				return nil, errors.New("exit status 1")
			}
			return []byte("cmd-token-value\n"), nil
		},
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // bypass umask
		t.Fatal(err)
	}
}

func setup(t *testing.T) (dir, cfgPath string) {
	t.Helper()
	dir = t.TempDir()
	cfgPath = filepath.Join(dir, "config.yaml")
	writeFile(t, filepath.Join(dir, "token"), fileToken+"\n", 0o600)
	writeFile(t, cfgPath, fmt.Sprintf(`# my lazyobs config
current-context: prod
contexts:
  prod:
    org: prod-org
    token-file: %s
  staging:
    url: https://staging.example.com
    org: staging-org
    token-command: pass show staging
  inline:
    org: inline-org
    token: inline-token-value
`, filepath.Join(dir, "token")), 0o600)
	return dir, cfgPath
}

func TestResolveFromContext(t *testing.T) {
	_, path := setup(t)
	r, err := config.Resolve(context.Background(), config.Overrides{ConfigPath: path}, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if r.Context != "prod" || r.Org != "prod-org" || r.URL != config.DefaultURL || r.Token.Reveal() != fileToken {
		t.Fatalf("unexpected %+v", r)
	}
	if r.URLSource != "default" || r.OrgSource != "context prod" || !strings.HasPrefix(r.TokenSource, "token-file ") {
		t.Errorf("sources: %q %q %q", r.URLSource, r.OrgSource, r.TokenSource)
	}
	if len(r.Warnings) != 0 {
		t.Errorf("unexpected warnings %v", r.Warnings)
	}
}

func TestResolvePrecedence(t *testing.T) {
	_, path := setup(t)

	// Env selects the context; flags win over env.
	r, err := config.Resolve(context.Background(), config.Overrides{ConfigPath: path, Org: "flag-org"}, env(map[string]string{
		config.EnvContext: "staging",
		config.EnvOrg:     "env-org",
		config.EnvURL:     "https://env.example.com",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Context != "staging" || r.Org != "flag-org" || r.URL != "https://env.example.com" || r.Token.Reveal() != "cmd-token-value" {
		t.Fatalf("unexpected %+v (token %q)", r, r.Token.Reveal())
	}

	// Flag --context wins over LAZYOBS_CONTEXT; LAZYOBS_TOKEN wins over the context token.
	r, err = config.Resolve(context.Background(), config.Overrides{ConfigPath: path, Context: "inline"}, env(map[string]string{
		config.EnvContext: "staging",
		config.EnvToken:   " env-token \n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Context != "inline" || r.Token.Reveal() != "env-token" || r.TokenSource != "env LAZYOBS_TOKEN" {
		t.Fatalf("unexpected %+v", r)
	}
}

func TestResolveEnvOnly(t *testing.T) {
	r, err := config.Resolve(context.Background(), config.Overrides{ConfigPath: filepath.Join(t.TempDir(), "missing.yaml")}, env(map[string]string{
		config.EnvOrg: "o", config.EnvToken: "t",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if r.ConfigFound || r.Context != "" || r.Org != "o" {
		t.Fatalf("unexpected %+v", r)
	}
}

func TestResolveErrors(t *testing.T) {
	dir, path := setup(t)

	_, err := config.Resolve(context.Background(), config.Overrides{ConfigPath: filepath.Join(dir, "none.yaml")}, env(nil))
	if !errors.Is(err, config.ErrNotConfigured) || !strings.Contains(err.Error(), "organization and API token") {
		t.Errorf("err = %v", err)
	}

	_, err = config.Resolve(context.Background(), config.Overrides{ConfigPath: path, Context: "nope"}, env(nil))
	if err == nil || !strings.Contains(err.Error(), "inline, prod, staging") {
		t.Errorf("unknown context err = %v", err)
	}

	writeFile(t, filepath.Join(dir, "cmd.yaml"), "current-context: c\ncontexts:\n  c:\n    org: o\n    token-command: fail\n", 0o600)
	_, err = config.Resolve(context.Background(), config.Overrides{ConfigPath: filepath.Join(dir, "cmd.yaml")}, env(nil))
	if err == nil || !strings.Contains(err.Error(), "token-command failed") {
		t.Errorf("token-command err = %v", err)
	}

	writeFile(t, filepath.Join(dir, "two.yaml"), "current-context: c\ncontexts:\n  c:\n    org: o\n    token: x\n    token-file: y\n", 0o600)
	_, err = config.Resolve(context.Background(), config.Overrides{ConfigPath: filepath.Join(dir, "two.yaml")}, env(nil))
	if err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Errorf("two sources err = %v", err)
	}

	writeFile(t, filepath.Join(dir, "bad.yaml"), "contexts: [", 0o600)
	if _, err = config.Resolve(context.Background(), config.Overrides{ConfigPath: filepath.Join(dir, "bad.yaml")}, env(nil)); err == nil {
		t.Error("invalid YAML must fail")
	}
}

func TestResolveDanglingCurrentContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	writeFile(t, path, "current-context: gone\ncontexts: {}\n", 0o600)
	r, err := config.Resolve(context.Background(), config.Overrides{ConfigPath: path}, env(map[string]string{config.EnvOrg: "o", config.EnvToken: "t"}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Context != "" || len(r.Warnings) != 1 {
		t.Fatalf("context=%q warnings=%v", r.Context, r.Warnings)
	}
}

func TestPermissionWarnings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced on Windows")
	}
	dir, path := setup(t)
	if err := os.Chmod(filepath.Join(dir, "token"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := config.Resolve(context.Background(), config.Overrides{ConfigPath: path}, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "chmod 600") {
		t.Errorf("warnings = %v", r.Warnings)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	r, err = config.Resolve(context.Background(), config.Overrides{ConfigPath: path, Context: "inline"}, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "holds a token") {
		t.Errorf("warnings = %v", r.Warnings)
	}
}

func TestResolvedNeverLeaksToken(t *testing.T) {
	_, path := setup(t)
	r, err := config.Resolve(context.Background(), config.Overrides{ConfigPath: path, Context: "inline"}, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if out := fmt.Sprintf(verb, r); strings.Contains(out, "inline-token-value") {
			t.Errorf("%s leaks the token: %s", verb, out)
		}
	}
}

func TestDefaultPath(t *testing.T) {
	got := config.DefaultPath(func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return "/xdg"
		}
		return ""
	})
	if got != filepath.Join("/xdg", "lazyobs", "config.yaml") {
		t.Errorf("DefaultPath = %q", got)
	}
}
