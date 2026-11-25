// Package inputs builds the text inputs of the views, with the editing
// shortcuts people expect from macOS and shells.
package inputs

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
)

// New returns a text input without prompt, with EditKeys and a steady
// cursor (blinking redraws the screen twice a second for nothing).
func New() textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.KeyMap = EditKeys()
	styles := in.Styles()
	styles.Cursor.Blink = false
	in.SetStyles(styles)
	return in
}

// EditKeys extends the default bindings with word and line deletions.
// Shift and Command combinations are only told apart from a plain backspace
// by terminals with enhanced keyboard reporting (Ghostty, kitty, WezTerm,
// iTerm2 with CSI u); the others work everywhere.
func EditKeys() textinput.KeyMap {
	k := textinput.DefaultKeyMap()
	k.DeleteWordBackward = key.NewBinding(key.WithKeys("alt+backspace", "ctrl+w", "ctrl+backspace", "shift+backspace"))
	k.DeleteCharacterBackward = key.NewBinding(key.WithKeys("backspace", "ctrl+h"))
	k.DeleteBeforeCursor = key.NewBinding(key.WithKeys("ctrl+u", "super+backspace"))
	k.DeleteAfterCursor = key.NewBinding(key.WithKeys("ctrl+k", "super+delete"))
	k.LineStart = key.NewBinding(key.WithKeys("home", "ctrl+a", "super+left"))
	k.LineEnd = key.NewBinding(key.WithKeys("end", "ctrl+e", "super+right"))
	return k
}
