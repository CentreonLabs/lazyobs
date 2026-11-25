// Package uitest drives Bubble Tea models headlessly in tests: it sends
// messages, runs the resulting commands synchronously and renders the screen
// as plain text.
package uitest

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// cmdTimeout bounds how long a command may run; longer ones (timers such as
// tea.Tick) are dropped, which keeps tests fast and deterministic.
const cmdTimeout = 300 * time.Millisecond

// Driver runs a model.
type Driver struct {
	t     testing.TB
	Model tea.Model
}

// New starts m: it runs Init and sends a window size.
func New(t testing.TB, m tea.Model, width, height int) *Driver {
	t.Helper()
	d := &Driver{t: t, Model: m}
	d.Run(m.Init())
	d.Send(tea.WindowSizeMsg{Width: width, Height: height})
	return d
}

// Send delivers msg and runs the resulting commands.
func (d *Driver) Send(msg tea.Msg) {
	d.t.Helper()
	m, cmd := d.Model.Update(msg)
	d.Model = m
	d.Run(cmd)
}

// Keys sends key presses, e.g. Keys("j", "j", "enter", "ctrl+d", "space").
func (d *Driver) Keys(keys ...string) {
	d.t.Helper()
	for _, k := range keys {
		d.Send(Key(k))
	}
}

// Type sends each rune of s as a key press.
func (d *Driver) Type(s string) {
	d.t.Helper()
	for _, r := range s {
		d.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// Run executes cmd and feeds its messages back, recursively.
func (d *Driver) Run(cmd tea.Cmd) { d.run(cmd, 0) }

func (d *Driver) run(cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 50 {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(cmdTimeout):
		return
	}
	switch msg := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			d.run(c, depth+1)
		}
	default:
		m, next := d.Model.Update(msg)
		d.Model = m
		d.run(next, depth+1)
	}
}

// Screen renders the current view without colors.
func (d *Driver) Screen() string { return ansi.Strip(d.Model.View().Content) }

// Key builds a key press from its textual form.
func Key(s string) tea.KeyPressMsg {
	special := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "space": tea.KeySpace,
		"up": tea.KeyUp, "down": tea.KeyDown, "backspace": tea.KeyBackspace,
		"pgdown": tea.KeyPgDown, "pgup": tea.KeyPgUp,
	}
	if code, ok := special[s]; ok {
		k := tea.KeyPressMsg{Code: code}
		if s == "space" {
			k.Text = " "
		}
		return k
	}
	if len(s) > 5 && s[:5] == "ctrl+" {
		return tea.KeyPressMsg{Code: rune(s[5]), Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}
