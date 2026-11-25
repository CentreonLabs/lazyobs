// Package secret provides a string type that refuses to reveal its value
// through formatting, logging or serialization.
package secret

import (
	"fmt"
	"log/slog"
)

const redacted = "[REDACTED]"

// String holds a sensitive value such as an API token.
//
// Every textual representation (fmt verbs, slog, JSON, YAML, text marshaling)
// prints a placeholder. The only way to read the value is Reveal, which must be
// called solely where the value is sent to its destination (the HTTP transport).
type String struct {
	value string
}

// New wraps a sensitive value.
func New(v string) String { return String{value: v} }

// Reveal returns the raw value. Call it only at the point of use.
func (s String) Reveal() string { return s.value }

// IsZero reports whether no value is set.
func (s String) IsZero() bool { return s.value == "" }

// String implements fmt.Stringer.
func (s String) String() string { return s.placeholder() }

// GoString implements fmt.GoStringer so %#v is redacted too.
func (s String) GoString() string { return s.placeholder() }

// Format implements fmt.Formatter so every verb (%s, %v, %q, %x, %+v, ...) is redacted.
func (s String) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(s.placeholder())) }

// LogValue implements slog.LogValuer.
func (s String) LogValue() slog.Value { return slog.StringValue(s.placeholder()) }

// MarshalText implements encoding.TextMarshaler (used by JSON and YAML encoders).
func (s String) MarshalText() ([]byte, error) { return []byte(s.placeholder()), nil }

func (s String) placeholder() string {
	if s.value == "" {
		return ""
	}
	return redacted
}
