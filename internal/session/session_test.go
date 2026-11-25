package session_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CentreonLabs/lazyobs/internal/session"
)

func TestSaveAndLoadPerKey(t *testing.T) {
	store := session.Store{Path: filepath.Join(t.TempDir(), "sub", "state.json")}
	if _, ok, err := store.Load("prod"); ok || err != nil {
		t.Fatalf("missing file: ok=%v err=%v", ok, err)
	}
	prod := session.Session{Active: 1, Tabs: []json.RawMessage{json.RawMessage(`{"query":"a"}`), json.RawMessage(`{"query":"b"}`)}}
	if err := store.Save("prod", prod); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("demo", session.Session{Tabs: []json.RawMessage{json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Load("prod")
	var second bytes.Buffer
	if len(got.Tabs) == 2 {
		_ = json.Compact(&second, got.Tabs[1]) // the file is indented for readability
	}
	if err != nil || !ok || got.Active != 1 || second.String() != `{"query":"b"}` {
		t.Fatalf("got %+v ok=%v err=%v", got, ok, err)
	}
	if _, ok, _ := store.Load("other"); ok {
		t.Error("unknown key must not load")
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(store.Path); info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v, want 0600", info.Mode().Perm())
		}
	}
}

func TestInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"corrupt": "{", "future": `{"version":99}`} {
		store := session.Store{Path: filepath.Join(dir, name+".json")}
		if err := os.WriteFile(store.Path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.Load("x"); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		// Saving over an unreadable file starts over instead of failing forever.
		if err := store.Save("x", session.Session{Tabs: []json.RawMessage{json.RawMessage(`{}`)}}); err != nil {
			t.Errorf("%s: save: %v", name, err)
		}
		if _, ok, err := store.Load("x"); !ok || err != nil {
			t.Errorf("%s: reload ok=%v err=%v", name, ok, err)
		}
	}
}

func TestDefaultPath(t *testing.T) {
	got := session.DefaultPath(func(k string) string {
		if k == "XDG_STATE_HOME" {
			return "/state"
		}
		return ""
	})
	if got != filepath.Join("/state", "lazyobs", "state.json") {
		t.Errorf("DefaultPath = %q", got)
	}
	if p := session.DefaultPath(func(string) string { return "" }); !strings.HasSuffix(p, "state.json") {
		t.Errorf("fallback = %q", p)
	}
}
