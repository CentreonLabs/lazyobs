package logsview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/ui/tabset"
	"github.com/CentreonLabs/lazyobs/internal/ui/uitest"
)

// openedChild runs the command of openContext and returns the tab it asks to open.
func openedChild(t *testing.T, cmd tea.Cmd) *Model {
	t.Helper()
	var found *Model
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				walk(sub)
			}
		case tabset.OpenMsg:
			found = msg.Child.(*Model)
		}
	}
	walk(cmd)
	if found == nil {
		t.Fatal("no tab opened")
	}
	return found
}

func TestContextFindsTheLogBeyondTheFirstPage(t *testing.T) {
	h := newHarness(t, Options{PageSize: 5})
	// The last loaded log of a service is far from the newest one of its
	// window, so it sits several pages down in the context tab.
	h.d.Keys("G")
	origin := h.view.Selected()

	child := openedChild(t, h.view.openContext())
	if child.opts.PageSize != 5 || child.anchor != origin.Fingerprint() {
		t.Fatalf("child = page size %d, anchor %x", child.opts.PageSize, child.anchor)
	}
	d := uitest.New(t, app.New("test", h.view.th, app.Tab{Title: "Logs", View: child}), 120, 30)
	if got := child.Selected(); got.Fingerprint() != origin.Fingerprint() {
		t.Fatalf("anchor not selected after %d pages: got %v, want %v", child.anchorPages, got.Timestamp, origin.Timestamp)
	}
	if child.anchorPages == 0 {
		t.Error("the anchor should have needed more than one page")
	}
	if !strings.Contains(d.Screen(), "◆") {
		t.Error("anchor mark missing")
	}
	for _, r := range child.records {
		if r.Service != origin.Service {
			t.Fatalf("context tab shows another service: %s", r.Service)
		}
	}
}

func TestContextWithoutLogs(t *testing.T) {
	h := newHarness(t, Options{Query: "nothing-matches"})
	if h.view.openContext() != nil {
		t.Error("no log, no context")
	}
}
