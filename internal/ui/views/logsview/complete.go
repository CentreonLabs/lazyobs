package logsview

import (
	"sort"
	"strings"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

// tokenKind tells what is being typed at the cursor.
type tokenKind int

const (
	tokenNone  tokenKind = iota
	tokenField           // a field name (or an operator)
	tokenValue           // the value after "field:"
)

// token is the part of the query under the cursor, in rune offsets.
type token struct {
	kind       tokenKind
	start, end int    // replaced span
	field      string // for tokenValue
	prefix     string // what is typed so far (value without its opening quote)
}

// isSeparator ends a token.
func isSeparator(r rune) bool { return r == ' ' || r == '(' || r == ')' || r == '\t' }

// tokenAt finds the token around pos in the query.
func tokenAt(query []rune, pos int) token {
	pos = min(max(pos, 0), len(query))
	start := pos
	inQuote := strings.Count(string(query[:pos]), `"`)%2 == 1
	for start > 0 {
		r := query[start-1]
		if r == '"' {
			inQuote = !inQuote
		}
		if !inQuote && isSeparator(r) {
			break
		}
		start--
	}
	end := pos
	for end < len(query) && !isSeparator(query[end]) {
		end++
	}
	for start < pos && (query[start] == '-' || query[start] == '+') { // negation / requirement
		start++
	}
	text := string(query[start:pos])
	if text == "" {
		return token{kind: tokenNone, start: start, end: end}
	}
	if field, value, ok := strings.Cut(text, ":"); ok && field != "" {
		return token{kind: tokenValue, start: start, end: end, field: field, prefix: strings.TrimPrefix(value, `"`)}
	}
	return token{kind: tokenField, start: start, end: end, prefix: text}
}

// suggestion is an entry of the completion list.
type suggestion struct {
	label  string // shown and inserted
	detail string // type or count, shown dimmed
	kind   tokenKind
	op     bool // an operator (AND, OR, NOT)
}

var operators = []string{"AND", "OR", "NOT"}

// maxSuggestions bounds the completion list.
const maxSuggestions = 8

// fieldSuggestions ranks the fields matching prefix: name prefix first, then
// a segment prefix ("version" for "resource_attributes.service.version"),
// then a substring; operators come first when they match.
func fieldSuggestions(fields []obs.Field, prefix string) []suggestion {
	p := strings.ToLower(prefix)
	var out []suggestion
	for _, op := range operators {
		if strings.HasPrefix(op, strings.ToUpper(prefix)) && len(prefix) >= 2 && op != prefix {
			out = append(out, suggestion{label: op, detail: "operator", kind: tokenField, op: true})
		}
	}
	type ranked struct {
		s    suggestion
		rank int
	}
	var matches []ranked
	for _, f := range fields {
		name := strings.ToLower(f.Name)
		rank := -1
		switch {
		case strings.HasPrefix(name, p):
			rank = 0
		case strings.Contains(name, "."+p) || strings.Contains(name, "_"+p):
			rank = 1
		case strings.Contains(name, p):
			rank = 2
		}
		if rank >= 0 && f.Name != prefix {
			matches = append(matches, ranked{suggestion{label: f.Name, detail: f.Type, kind: tokenField}, rank})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].rank != matches[j].rank {
			return matches[i].rank < matches[j].rank
		}
		return len(matches[i].s.label) < len(matches[j].s.label)
	})
	for _, m := range matches {
		out = append(out, m.s)
	}
	if len(out) > maxSuggestions {
		out = out[:maxSuggestions]
	}
	return out
}

func valueSuggestions(values []obs.FieldValue, prefix string) []suggestion {
	out := make([]suggestion, 0, len(values))
	for _, v := range values {
		if v.Value == prefix {
			continue // already complete
		}
		out = append(out, suggestion{label: v.Value, detail: humanCount(v.Count), kind: tokenValue})
	}
	if len(out) > maxSuggestions {
		out = out[:maxSuggestions]
	}
	return out
}

// complete inserts s in place of tok and returns the new query and cursor.
func complete(query []rune, tok token, s suggestion) (string, int) {
	var insert string
	switch {
	case s.kind == tokenValue:
		insert = tok.field + ":" + quoteValue(s.label)
	case s.op:
		insert = s.label
	default:
		insert = s.label + ":"
	}
	// A completed value or operator is followed by a space, ready for the next term.
	if s.kind == tokenValue || s.op {
		if tok.end >= len(query) || query[tok.end] != ' ' {
			insert += " "
		} else {
			tok.end++
			insert += " "
		}
	}
	out := string(query[:tok.start]) + insert + string(query[tok.end:])
	return out, tok.start + len([]rune(insert))
}

// quoteValue quotes values the query syntax would otherwise split or misread.
func quoteValue(v string) string {
	if v != "" && strings.IndexFunc(v, needsQuote) < 0 {
		return v
	}
	return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
}

// needsQuote reports characters that cannot appear in a bare value.
func needsQuote(r rune) bool {
	plain := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_-./@", r)
	return !plain
}
