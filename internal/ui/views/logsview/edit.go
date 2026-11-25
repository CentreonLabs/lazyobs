package logsview

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
)

// editKeys extends the text input bindings with the deletions people expect
// from macOS and shells. Shift and Command combinations are only told apart
// from a plain backspace by terminals with enhanced keyboard reporting
// (Ghostty, kitty, WezTerm, iTerm2 with CSI u); the others work everywhere.
func editKeys() textinput.KeyMap {
	k := textinput.DefaultKeyMap()
	k.DeleteWordBackward = key.NewBinding(key.WithKeys("alt+backspace", "ctrl+w", "ctrl+backspace", "shift+backspace"))
	k.DeleteCharacterBackward = key.NewBinding(key.WithKeys("backspace", "ctrl+h"))
	k.DeleteBeforeCursor = key.NewBinding(key.WithKeys("ctrl+u", "super+backspace"))
	k.DeleteAfterCursor = key.NewBinding(key.WithKeys("ctrl+k", "super+delete"))
	k.LineStart = key.NewBinding(key.WithKeys("home", "ctrl+a", "super+left"))
	k.LineEnd = key.NewBinding(key.WithKeys("end", "ctrl+e", "super+right"))
	return k
}
