package remote

import (
	"context"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
)

// TraceFieldsAPI is the subset of the API client used for span fields.
type TraceFieldsAPI interface {
	TraceFieldCapabilities(ctx context.Context, from, to time.Time) (map[string]map[string]api.FieldCapability, error)
	TraceFieldTerms(ctx context.Context, field, prefix string, from, to time.Time, maxTerms int) ([]api.Term, error)
}

// NewTraceFields returns a field suggester for spans.
func NewTraceFields(client TraceFieldsAPI) Fields { return Fields{api: traceFields{client}} }

// traceFields presents the span field endpoints as FieldsAPI.
type traceFields struct{ c TraceFieldsAPI }

func (t traceFields) LogFieldCapabilities(ctx context.Context, from, to time.Time) (map[string]map[string]api.FieldCapability, error) {
	return t.c.TraceFieldCapabilities(ctx, from, to)
}

func (t traceFields) LogFieldTerms(ctx context.Context, field, prefix string, from, to time.Time, maxTerms int) ([]api.Term, error) {
	return t.c.TraceFieldTerms(ctx, field, prefix, from, to, maxTerms)
}
