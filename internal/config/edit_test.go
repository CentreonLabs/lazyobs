package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CentreonLabs/lazyobs/internal/config"
)

func ptr(s string) *string { return &s }

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUseContextPreservesFile(t *testing.T) {
	_, path := setup(t)
	if err := config.UseContext(path, "inline"); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	for _, want := range []string{"# my lazyobs config", "current-context: inline", "token: inline-token-value", "token-command: pass show staging"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q after edit:\n%s", want, got)
		}
	}
	if err := config.UseContext(path, "nope"); err == nil {
		t.Error("unknown context must fail")
	}
}

func TestSetContextCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	if err := config.SetContext(path, "dev", config.ContextUpdate{Org: ptr("dev-org"), TokenFile: ptr("~/.config/lazyobs/token")}); err != nil {
		t.Fatal(err)
	}
	f, found, err := config.ReadFile(path)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if f.CurrentContext != "dev" || f.Contexts["dev"].Org != "dev-org" || f.Contexts["dev"].TokenFile != "~/.config/lazyobs/token" {
		t.Fatalf("unexpected %+v", f)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", info.Mode().Perm())
		}
	}
}

func TestSetContextUpdates(t *testing.T) {
	_, path := setup(t)
	// Switching to a token command drops the inline token; an empty URL removes the key.
	err := config.SetContext(path, "inline", config.ContextUpdate{TokenCommand: ptr("op read x"), URL: ptr("https://u.example.com")})
	if err != nil {
		t.Fatal(err)
	}
	if err := config.SetContext(path, "staging", config.ContextUpdate{URL: ptr("")}); err != nil {
		t.Fatal(err)
	}
	f, _, err := config.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	in := f.Contexts["inline"]
	if !in.Token.IsZero() || in.TokenCommand != "op read x" || in.URL != "https://u.example.com" || in.Org != "inline-org" {
		t.Errorf("inline = %+v", in)
	}
	if f.Contexts["staging"].URL != "" || f.CurrentContext != "prod" {
		t.Errorf("staging url=%q current=%q", f.Contexts["staging"].URL, f.CurrentContext)
	}
	if !strings.Contains(read(t, path), "# my lazyobs config") {
		t.Error("comment lost")
	}
}
