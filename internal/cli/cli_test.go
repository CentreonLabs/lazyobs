package cli_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CentreonLabs/lazyobs/internal/cli"
	"github.com/CentreonLabs/lazyobs/internal/config"
)

const token = "cli-secret-token"

type result struct {
	out, err string
	runErr   error
}

func run(t *testing.T, vars map[string]string, args ...string) result {
	t.Helper()
	env := config.Env{
		Getenv:     func(k string) string { return vars[k] },
		RunCommand: func(context.Context, string) ([]byte, error) { return []byte(token), nil },
	}
	var out, errOut bytes.Buffer
	cmd := cli.NewRootCommand(env, &out, &errOut)
	cmd.SetArgs(args)
	runErr := cmd.ExecuteContext(context.Background())
	r := result{out.String(), errOut.String(), runErr}
	for _, s := range []string{r.out, r.err, errString(runErr)} {
		if strings.Contains(s, token) {
			t.Fatalf("output leaks the token: %q", s)
		}
	}
	return r
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestContextLifecycle(t *testing.T) {
	dir := t.TempDir()
	vars := map[string]string{config.EnvConfig: filepath.Join(dir, "config.yaml")}

	r := run(t, vars, "config", "get-contexts")
	if r.runErr != nil || !strings.Contains(r.out, "No context defined") {
		t.Fatalf("%+v", r)
	}

	r = run(t, vars, "config", "set-context", "prod", "--org", "prod-org", "--token-command", "pass show x")
	if r.runErr != nil {
		t.Fatal(r.runErr)
	}
	r = run(t, vars, "config", "set-context", "dev", "--org", "dev-org", "--url", "https://dev.example.com", "--token-command", "pass show y")
	if r.runErr != nil {
		t.Fatal(r.runErr)
	}

	r = run(t, vars, "config", "get-contexts")
	if list := strings.Join(strings.Fields(r.out), " "); !strings.Contains(list, "* prod prod-org "+config.DefaultURL) ||
		!strings.Contains(list, "dev dev-org https://dev.example.com") {
		t.Fatalf("get-contexts:\n%s", r.out)
	}

	if r = run(t, vars, "config", "use-context", "dev"); r.runErr != nil {
		t.Fatal(r.runErr)
	}
	r = run(t, vars, "config", "view")
	view := strings.Join(strings.Fields(r.out), " ")
	for _, want := range []string{"context dev", "org dev-org (context dev)", "token set (token-command)"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, r.out)
		}
	}

	if r = run(t, vars, "config", "use-context", "nope"); r.runErr == nil {
		t.Error("use-context on an unknown context must fail")
	}
	if r = run(t, vars, "config", "set-context", "x", "--token-file", "a", "--token-command", "b"); r.runErr == nil {
		t.Error("token-file and token-command together must fail")
	}
}

func TestConfigPath(t *testing.T) {
	r := run(t, map[string]string{config.EnvConfig: "/tmp/lazyobs-test.yaml"}, "config", "path")
	if strings.TrimSpace(r.out) != "/tmp/lazyobs-test.yaml" {
		t.Errorf("path = %q", r.out)
	}
	r = run(t, map[string]string{config.EnvConfig: "/tmp/a.yaml"}, "--config", "/tmp/b.yaml", "config", "path")
	if strings.TrimSpace(r.out) != "/tmp/b.yaml" {
		t.Errorf("flag must win over env, got %q", r.out)
	}
}

func TestCheck(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Api-Key")
		if r.URL.Path != "/v1/orgs/my-org/logs/search" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"hits":[],"num_hits":0,"errors":[]}`))
	}))
	defer srv.Close()
	vars := map[string]string{
		config.EnvConfig: filepath.Join(t.TempDir(), "none.yaml"),
		config.EnvURL:    srv.URL,
		config.EnvOrg:    "my-org",
		config.EnvToken:  token,
	}

	r := run(t, vars, "config", "check")
	if r.runErr != nil || !strings.HasPrefix(r.out, `OK: organization "my-org" reachable`) || gotKey != token {
		t.Fatalf("%+v key=%q", r, gotKey)
	}

	r = run(t, vars, "--org", "other-org", "config", "check")
	if r.runErr == nil || !strings.Contains(r.runErr.Error(), "404") {
		t.Fatalf("expected a 404 error, got %v", r.runErr)
	}
}

func TestNotConfigured(t *testing.T) {
	r := run(t, map[string]string{config.EnvConfig: filepath.Join(t.TempDir(), "none.yaml")})
	if r.runErr == nil || !strings.Contains(r.runErr.Error(), "not configured") {
		t.Fatalf("err = %v", r.runErr)
	}
}

func TestPermissionWarningIsPrinted(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tokenPath, 0o644); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{config.EnvConfig: filepath.Join(dir, "config.yaml")}
	run(t, vars, "config", "set-context", "p", "--org", "o", "--token-file", tokenPath)
	r := run(t, vars, "config", "view")
	if !strings.Contains(r.err, "warning: token file") {
		t.Errorf("stderr = %q", r.err)
	}
}

func TestVersion(t *testing.T) {
	if r := run(t, nil, "version"); !strings.HasPrefix(r.out, "lazyobs ") {
		t.Errorf("version = %q", r.out)
	}
}
