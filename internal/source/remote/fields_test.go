package remote_test

import (
	"context"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/source/remote"
)

type fieldsStub struct{ prefix string }

func (s *fieldsStub) LogFieldCapabilities(context.Context, time.Time, time.Time) (map[string]map[string]api.FieldCapability, error) {
	return map[string]map[string]api.FieldCapability{
		"service_name":    {"text": {Searchable: true, Aggregatable: true}, "keyword": {Searchable: true, Aggregatable: true}},
		"body.message":    {"text": {Searchable: true}},
		"timestamp_nanos": {"date_nanos": {Aggregatable: true}}, // not searchable
		"severity_number": {"long": {Searchable: true, Aggregatable: true}},
	}, nil
}

func (s *fieldsStub) LogFieldTerms(_ context.Context, _, prefix string, _, _ time.Time, _ int) ([]api.Term, error) {
	s.prefix = prefix
	return []api.Term{{Value: "auth", Count: 4}, {Value: 13.0, Count: 2}}, nil
}

func TestFields(t *testing.T) {
	stub := &fieldsStub{}
	f := remote.NewFields(stub)
	fields, err := f.Fields(context.Background(), now, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields[0].Name != "body.message" || fields[1].Name != "service_name" {
		t.Fatalf("fields = %+v", fields)
	}
	if fields[0].Aggregatable || fields[0].Type != "text" || fields[1].Type != "keyword" || !fields[1].Aggregatable || fields[2].Type != "long" ||
		!fields[0].FullText || !fields[1].FullText || fields[2].FullText {
		t.Errorf("types/aggregatable = %+v", fields)
	}
	values, err := f.Values(context.Background(), "service_name", "au", now, now, 10)
	if err != nil || stub.prefix != "au" || len(values) != 2 || values[0].Value != "auth" || values[1].Value != "13" {
		t.Errorf("values = %+v (prefix %q, err %v)", values, stub.prefix, err)
	}
}
