package logsview

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/ui/draw"
)

const (
	// suggestDelay waits for a pause in typing before asking for values.
	suggestDelay = 150 * time.Millisecond
	// fieldsTTL is how long the field list is reused.
	fieldsTTL      = 5 * time.Minute
	queryPrompt    = "query › "
	valuesPerField = 10
)

// completion is the state of the query autocompletion.
type completion struct {
	tok   token
	items []suggestion
	// selected is the highlighted suggestion, -1 for none: nothing is
	// preselected when the typed word is already a known field or value, so
	// that enter runs the query instead of replacing what was typed.
	selected  int
	exact     bool
	dismissed bool // closed with esc until the token changes

	fields        []obs.Field
	fieldsAt      time.Time
	loadingFields bool

	values  map[string][]obs.FieldValue // by valuesKey
	seq     int                         // last value request scheduled
	pending string                      // valuesKey being waited for
}

type (
	fieldsMsg struct {
		owner
		fields []obs.Field
		err    error
	}
	suggestTickMsg struct {
		owner
		seq           int
		field, prefix string
	}
	valuesMsg struct {
		owner
		key    string
		values []obs.FieldValue
		err    error
	}
)

func valuesKey(field, prefix string) string { return field + "\x00" + strings.ToLower(prefix) }

// open reports whether the suggestion list is shown.
func (c *completion) open() bool { return !c.dismissed && len(c.items) > 0 }

// startCompletion prepares completion when the query input opens.
func (m *Model) startCompletion() tea.Cmd {
	m.compl.dismissed = false
	m.compl.items = nil
	if m.opts.Fields == nil || m.compl.loadingFields || (m.compl.fields != nil && m.opts.Now().Sub(m.compl.fieldsAt) < fieldsTTL) {
		return m.refreshCompletion()
	}
	m.compl.loadingFields = true
	src, viewID := m.opts.Fields, m.id
	from, to := m.rng.Resolve(m.opts.Now())
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		fields, err := src.Fields(ctx, from, to)
		return fieldsMsg{owner: owner{viewID}, fields: fields, err: err}
	}
}

func (m *Model) onFields(msg fieldsMsg) tea.Cmd {
	m.compl.loadingFields = false
	if msg.err != nil {
		return nil // completion is a convenience: stay silent
	}
	m.compl.fields, m.compl.fieldsAt = msg.fields, m.opts.Now()
	return m.refreshCompletion()
}

// refreshCompletion recomputes the suggestions for the token at the cursor,
// scheduling a value request when needed.
func (m *Model) refreshCompletion() tea.Cmd {
	if m.mode != modeQuery || m.opts.Fields == nil {
		m.compl.items = nil
		return nil
	}
	tok := tokenAt([]rune(m.input.Value()), m.input.Position())
	changed := tok.kind != m.compl.tok.kind || tok.start != m.compl.tok.start || tok.field != m.compl.tok.field || tok.prefix != m.compl.tok.prefix
	if changed {
		m.compl.dismissed = false
	}
	m.compl.tok = tok
	m.compl.items = nil
	defer func() {
		if changed || m.compl.selected >= len(m.compl.items) {
			m.compl.selected = 0
			if m.compl.exact {
				m.compl.selected = -1
			}
		}
	}()
	m.compl.exact = false

	switch tok.kind {
	case tokenField:
		m.compl.items = fieldSuggestions(m.compl.fields, tok.prefix)
		_, m.compl.exact = m.field(tok.prefix)
	case tokenValue:
		if f, ok := m.field(tok.field); ok && !f.Aggregatable {
			return nil // the platform cannot list values of this field
		}
		k := valuesKey(tok.field, tok.prefix)
		if values, ok := m.compl.values[k]; ok {
			m.compl.items = valueSuggestions(values, tok.prefix)
			for _, v := range values {
				m.compl.exact = m.compl.exact || v.Value == tok.prefix
			}
			return nil
		}
		if k == m.compl.pending {
			return nil // already asked
		}
		m.compl.seq++
		m.compl.pending = k
		return m.opts.After(suggestDelay, suggestTickMsg{owner: owner{m.id}, seq: m.compl.seq, field: tok.field, prefix: tok.prefix})
	}
	return nil
}

func (m *Model) field(name string) (obs.Field, bool) {
	for _, f := range m.compl.fields {
		if f.Name == name {
			return f, true
		}
	}
	return obs.Field{}, false
}

// onSuggestTick fires the value request if typing has paused.
func (m *Model) onSuggestTick(msg suggestTickMsg) tea.Cmd {
	if msg.seq != m.compl.seq || m.mode != modeQuery {
		return nil
	}
	src, viewID, k := m.opts.Fields, m.id, valuesKey(msg.field, msg.prefix)
	from, to := m.rng.Resolve(m.opts.Now())
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		values, err := src.Values(ctx, msg.field, msg.prefix, from, to, valuesPerField)
		return valuesMsg{owner: owner{viewID}, key: k, values: values, err: err}
	}
}

func (m *Model) onValues(msg valuesMsg) tea.Cmd {
	if msg.key == m.compl.pending {
		m.compl.pending = ""
	}
	if msg.err != nil {
		return nil
	}
	if m.compl.values == nil {
		m.compl.values = map[string][]obs.FieldValue{}
	}
	m.compl.values[msg.key] = msg.values
	return m.refreshCompletion()
}

// completionKey handles the keys of an open suggestion list; ok is false
// when the key is not for the list.
func (m *Model) completionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.mode != modeQuery || !m.compl.open() {
		return nil, false
	}
	k, n := m.keys, len(m.compl.items)
	switch {
	case key.Matches(msg, k.SuggestNext):
		m.compl.selected = (m.compl.selected + 1) % n
	case key.Matches(msg, k.SuggestPrev):
		m.compl.selected = (max(m.compl.selected, 0) - 1 + n) % n
	case key.Matches(msg, k.Accept), key.Matches(msg, k.Submit) && m.compl.selected >= 0:
		// tab inserts the highlighted (or first) suggestion; enter only a
		// highlighted one, otherwise it runs the query.
		value, cursor := complete([]rune(m.input.Value()), m.compl.tok, m.compl.items[max(m.compl.selected, 0)])
		m.input.SetValue(value)
		m.input.SetCursor(cursor)
		return m.refreshCompletion(), true
	case key.Matches(msg, k.Cancel):
		m.compl.dismissed = true
	default:
		return nil, false
	}
	return nil, true
}

// overlayCompletion draws the suggestion list below the query bar.
func (m *Model) overlayCompletion(view string) string {
	if m.mode != modeQuery || !m.compl.open() {
		return view
	}
	t := m.th
	labelW := 0
	for _, s := range m.compl.items {
		labelW = max(labelW, ansi.StringWidth(s.label))
	}
	labelW = min(labelW, max(m.width/2, 20))
	rows := make([]string, 0, len(m.compl.items))
	for i, s := range m.compl.items {
		label := fmt.Sprintf("%-*s", labelW, ansi.Truncate(s.label, labelW, "…"))
		detail := fmt.Sprintf("%8s", s.detail)
		if i == m.compl.selected {
			rows = append(rows, t.Selected.Render(label+"  "+detail))
			continue
		}
		rows = append(rows, label+"  "+t.Dim.Render(detail))
	}
	box := draw.Boxed(rows, t.Dim)
	x := ansi.StringWidth(queryPrompt) + ansi.StringWidth(string([]rune(m.input.Value())[:m.compl.tok.start])) - 2
	x = min(max(x, 0), max(m.width-ansi.StringWidth(box[0]), 0))
	lines := strings.Split(view, "\n")
	draw.Overlay(lines, box, x, 1)
	return strings.Join(lines, "\n")
}
