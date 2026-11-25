package remote

import (
	"context"
	"sort"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/obs"
)

// FieldsAPI is the subset of the API client used by Fields.
type FieldsAPI interface {
	LogFieldCapabilities(ctx context.Context, from, to time.Time) (map[string]map[string]api.FieldCapability, error)
	LogFieldTerms(ctx context.Context, field, prefix string, from, to time.Time, maxTerms int) ([]api.Term, error)
}

// Fields suggests log fields and values from the platform.
type Fields struct{ api FieldsAPI }

var _ obs.FieldSuggester = Fields{}

// NewFields returns a field suggester backed by client.
func NewFields(client FieldsAPI) Fields { return Fields{api: client} }

// typePreference orders the types of a field when it has several.
var typePreference = []string{"keyword", "long", "double", "boolean", "ip", "text", "date_nanos", "binary"}

// Fields implements obs.FieldSuggester: searchable fields, sorted by name.
func (f Fields) Fields(ctx context.Context, from, to time.Time) ([]obs.Field, error) {
	caps, err := f.api.LogFieldCapabilities(ctx, from, to)
	if err != nil {
		return nil, err
	}
	fields := make([]obs.Field, 0, len(caps))
	for name, byType := range caps {
		field := obs.Field{Name: name}
		searchable := false
		for _, t := range typePreference {
			c, ok := byType[t]
			if !ok {
				continue
			}
			if field.Type == "" {
				field.Type = t
			}
			searchable = searchable || c.Searchable
			field.Aggregatable = field.Aggregatable || c.Aggregatable
		}
		if searchable {
			fields = append(fields, field)
		}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields, nil
}

// Values implements obs.FieldSuggester.
func (f Fields) Values(ctx context.Context, field, prefix string, from, to time.Time, limit int) ([]obs.FieldValue, error) {
	terms, err := f.api.LogFieldTerms(ctx, field, prefix, from, to, limit)
	if err != nil {
		return nil, err
	}
	values := make([]obs.FieldValue, 0, len(terms))
	for _, t := range terms {
		values = append(values, obs.FieldValue{Value: t.String(), Count: t.Count})
	}
	return values, nil
}
