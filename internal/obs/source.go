package obs

import (
	"context"
	"time"
)

// Searcher returns pages of items of one signal matching a query.
type Searcher[T any] interface {
	Search(ctx context.Context, q Query) (Page[T], error)
}

// Histogrammer computes time series over the items matching a query,
// typically their count per time bucket.
type Histogrammer interface {
	Histogram(ctx context.Context, q Query, buckets int) ([]Series, error)
}

// Series is a named time series.
type Series struct {
	Name   string
	Unit   string
	Labels map[string]string
	Points []Point
}

// Point is a single value of a Series.
type Point struct {
	Time  time.Time
	Value float64
}

// Translation is a natural-language prompt turned into a query.
type Translation struct {
	Query string
	// Range is the time hint found in the prompt, nil when there is none.
	Range *TimeRange
	// Warning names what the query could not express, if anything.
	Warning string
}

// QueryTranslator turns natural language into the backend query language.
type QueryTranslator interface {
	Translate(ctx context.Context, prompt string) (Translation, error)
}

// Field is a searchable field of a signal.
type Field struct {
	Name string
	Type string // keyword, text, long, double, boolean, ip, date_nanos, binary
	// Aggregatable fields can list their most frequent values.
	Aggregatable bool
	// FullText fields are tokenized: multi-word values must be searched word
	// by word when the backend does not index word positions.
	FullText bool
}

// FieldValue is a value of a field with its number of occurrences.
type FieldValue struct {
	Value string
	Count int64
}

// FieldSuggester lists fields and their frequent values over a time window,
// to help writing queries.
type FieldSuggester interface {
	Fields(ctx context.Context, from, to time.Time) ([]Field, error)
	// Values returns the most frequent values of field starting with prefix.
	Values(ctx context.Context, field, prefix string, from, to time.Time, limit int) ([]FieldValue, error)
}

// Breakdown is the distribution of a field's values among the items
// matching a query.
type Breakdown struct {
	Values []FieldValue // most frequent first
	Total  int64        // all matching items, including those of values not listed
}

// Breakdowner counts the items matching a query per value of a field.
type Breakdowner interface {
	Breakdown(ctx context.Context, q Query, field string) (Breakdown, error)
}
