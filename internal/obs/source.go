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
