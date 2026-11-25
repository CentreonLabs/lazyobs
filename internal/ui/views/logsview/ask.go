package logsview

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
)

const askPlaceholder = "errors from the checkout service in the last hour"

// openAsk starts typing a natural-language query.
func (m *Model) openAsk() tea.Cmd {
	if m.opts.Translator == nil {
		return app.Notify(app.LevelWarn, "natural-language queries are not available with this source")
	}
	return m.openInput(modeAsk, m.lastPrompt, askPlaceholder)
}

func (m *Model) translate(prompt string) tea.Cmd {
	m.askID++
	m.translating = true
	m.lastPrompt = prompt
	id, tr := m.askID, m.opts.Translator
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		res, err := tr.Translate(ctx, prompt)
		return translateMsg{id: id, prompt: prompt, result: res, err: err}
	}
}

func (m *Model) onTranslate(msg translateMsg) tea.Cmd {
	if msg.id != m.askID {
		return nil
	}
	m.translating = false
	if msg.err != nil {
		return app.Notify(app.LevelError, "could not translate %q: %v", msg.prompt, msg.err)
	}
	m.query = strings.TrimSpace(msg.result.Query)
	if r := msg.result.Range; r != nil {
		m.rng = *r
		if m.live && !r.IsRelative() {
			m.live = false
		}
	}
	notice := app.Notify(app.LevelInfo, "translated to: %s", m.query)
	if msg.result.Warning != "" {
		notice = app.Notify(app.LevelWarn, "partial translation: %s", msg.result.Warning)
	}
	return tea.Batch(m.search(), notice)
}
