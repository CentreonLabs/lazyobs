package logsview

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
)

// filterOnDetail adds the selected field of the detail panel to the query,
// as a required term or, with exclude, as an excluded one, and searches.
func (m *Model) filterOnDetail(exclude bool) tea.Cmd {
	if len(m.detailEntries) == 0 {
		return nil
	}
	e := m.detailEntries[m.detailField]
	term := e.field + ":" + quoteValue(e.value)
	if exclude {
		term = "NOT " + term
	}
	query, added := addTerm(m.query, term)
	if !added {
		return app.Notify(app.LevelInfo, "the query already has %s", term)
	}
	m.query = query
	return tea.Batch(m.search(), app.Notify(app.LevelInfo, "added %s · ctrl+t first to keep the previous search in a tab", term))
}

// addTerm combines a term with a query (AND), keeping the query's meaning:
// a query with a top-level OR is wrapped in parentheses. When the opposite
// filter is there ("x:1" for "NOT x:1" and the other way round), it is
// flipped instead of contradicted. added is false when the term is already
// there.
func addTerm(query, term string) (string, bool) {
	q := strings.TrimSpace(query)
	if q == "" || q == "*" {
		return term, true
	}
	bare, negated := strings.CutPrefix(term, "NOT ")
	if _, _, ok := findTerm(q, bare, negated); ok {
		return q, false
	}
	if start, end, ok := findTerm(q, bare, !negated); ok {
		return q[:start] + term + q[end:], true
	}
	if hasTopLevelOr(q) {
		q = "(" + q + ")"
	}
	return q + " AND " + term, true
}

// findTerm locates bare as a whole term of q with the given polarity; the
// span includes the "NOT " of a negated term.
func findTerm(q, bare string, negated bool) (start, end int, ok bool) {
	for from := 0; from < len(q); {
		i := strings.Index(q[from:], bare)
		if i < 0 {
			return 0, 0, false
		}
		i += from
		end := i + len(bare)
		before := i == 0 || isSeparator(rune(q[i-1]))
		after := end == len(q) || isSeparator(rune(q[end]))
		prefix := strings.TrimRight(q[:i], " ")
		isNegated := strings.HasSuffix(prefix, "NOT") && (len(prefix) == 3 || isSeparator(rune(prefix[len(prefix)-4])))
		if before && after && isNegated == negated {
			if negated {
				return len(prefix) - 3, end, true
			}
			return i, end, true
		}
		from = i + 1
	}
	return 0, 0, false
}

// hasTopLevelOr reports an OR outside parentheses and quotes.
func hasTopLevelOr(q string) bool {
	depth, quoted := 0, false
	for i := 0; i < len(q); i++ {
		switch c := q[i]; {
		case c == '"' && (i == 0 || q[i-1] != '\\'):
			quoted = !quoted
		case quoted:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && strings.HasPrefix(q[i:], " OR "):
			return true
		}
	}
	return false
}
