package logsview

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

func TestSnapshotRestore(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("/")
	h.d.Type("service_name:auth")
	h.d.Keys("enter", "z", "v", "L")
	h.view.hidden["DEBUG"] = true
	h.view.lastPrompt = "auth errors"

	b, err := json.Marshal(h.view.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	want := State{Query: "service_name:auth", Range: DefaultRange, Live: true, UTC: true, Compact: true, Hidden: []string{"DEBUG"}, Prompt: "auth errors"}
	if s.Query != want.Query || s.Range != want.Range || !s.Live || !s.UTC || !s.Compact || len(s.Hidden) != 1 || s.Prompt != want.Prompt {
		t.Fatalf("snapshot = %+v (json %s)", s, b)
	}

	// Restoring starts the search and, for a live tab, the live mode.
	r := newHarnessFrom(t, s)
	if r.view.query != "service_name:auth" || !r.view.utc || !r.view.compact || !r.view.hidden["DEBUG"] || r.view.lastPrompt != "auth errors" {
		t.Fatalf("restored view = %+v", r.view.Snapshot())
	}
	if r.view.tail == nil || !strings.Contains(r.d.Screen(), "LIVE") {
		t.Error("a live tab must resume live mode")
	}
}

func TestRestoreAbsoluteRangeIsNotLive(t *testing.T) {
	past := obs.Between(time.Date(2026, 11, 3, 13, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 14, 0, 0, 0, time.UTC))
	r := newHarnessFrom(t, State{Range: past, Live: true})
	if r.view.live || r.view.rng != past {
		t.Fatalf("live %v, range %v", r.view.live, r.view.rng)
	}
}

func TestDetailPanelIsRestored(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("enter", "tab")
	h.selectField(t, "service_name")
	s := h.view.Snapshot()
	if !s.Detail || s.DetailField != "service_name" {
		t.Fatalf("snapshot = %+v", s)
	}

	r := newHarnessFrom(t, s)
	if !r.view.showDetail || !strings.Contains(r.d.Screen(), "Details") {
		t.Fatalf("detail panel not reopened:\n%s", r.d.Screen())
	}
	if len(r.view.detailEntries) == 0 || r.view.detailEntries[r.view.detailField].field != "service_name" {
		t.Errorf("selected field not restored: %+v", r.view.detailEntries)
	}
	if r.view.compl.fields == nil {
		t.Error("searchable fields must be loaded for the restored panel")
	}

	h.d.Keys("esc")
	if h.view.Snapshot().Detail {
		t.Error("a closed panel must be saved closed")
	}
}
