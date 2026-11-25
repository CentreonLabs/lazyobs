package logsview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFastDeletionWhileTyping(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyPressMsg
		want string
	}{
		{"shift+backspace", tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModShift}, "service_name:auth AND "},
		{"alt+backspace", tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}, "service_name:auth AND "},
		{"ctrl+w", tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}, "service_name:auth AND "},
		{"cmd+backspace", tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper}, ""},
		{"ctrl+u", tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, ""},
		{"backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}, "service_name:auth AND severity_text:ERRO"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, Options{})
			h.d.Keys("/")
			h.d.Type("service_name:auth AND severity_text:ERROR")
			h.d.Send(tc.key)
			if got := h.view.input.Value(); got != tc.want {
				t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestCmdArrowsJumpToTheEnds(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("/")
	h.d.Type("a:1 AND b:2")
	h.d.Send(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModSuper})
	if h.view.input.Position() != 0 {
		t.Fatalf("cmd+left: cursor at %d", h.view.input.Position())
	}
	h.d.Send(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModSuper})
	if h.view.input.Position() != len("a:1 AND b:2") {
		t.Fatalf("cmd+right: cursor at %d", h.view.input.Position())
	}
}
