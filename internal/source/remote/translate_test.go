package remote_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/source/remote"
)

type translatorStub struct {
	resp      *api.TranslateResponse
	termsErr  error
	termCalls int
	got       api.TranslateRequest
}

func (s *translatorStub) TranslateQuery(_ context.Context, req api.TranslateRequest) (*api.TranslateResponse, error) {
	s.got = req
	return s.resp, nil
}

func (s *translatorStub) LogFieldTerms(context.Context, string, string, time.Time, time.Time, int) ([]api.Term, error) {
	s.termCalls++
	if s.termsErr != nil {
		return nil, s.termsErr
	}
	return []api.Term{{Value: "api", Count: 3}, {Value: "checkout", Count: 1}}, nil
}

func TestTranslatorGroundsOnServicesOnce(t *testing.T) {
	warn := "the count could not be expressed"
	stub := &translatorStub{resp: &api.TranslateResponse{Query: "service_name:api", TimeRange: json.RawMessage(`900000`), Warning: &warn}}
	tr := remote.NewTranslator(stub, func() time.Time { return now })

	got, err := tr.Translate(context.Background(), "how many api errors")
	if err != nil {
		t.Fatal(err)
	}
	if got.Query != "service_name:api" || got.Warning != warn || got.Range == nil || got.Range.Last != 15*time.Minute {
		t.Errorf("translation = %+v", got)
	}
	if len(stub.got.AvailableServices) != 2 || stub.got.AvailableServices[1] != "checkout" {
		t.Errorf("services = %v", stub.got.AvailableServices)
	}
	_, _ = tr.Translate(context.Background(), "again")
	if stub.termCalls != 1 {
		t.Errorf("services fetched %d times, want once", stub.termCalls)
	}
}

func TestTranslatorWithoutServices(t *testing.T) {
	stub := &translatorStub{
		resp:     &api.TranslateResponse{Query: "*", TimeRange: json.RawMessage(`{"start":1793709000000,"end":1793714400000}`)},
		termsErr: errors.New("forbidden"),
	}
	got, err := remote.NewTranslator(stub, nil).Translate(context.Background(), "everything on nov 3")
	if err != nil {
		t.Fatal(err)
	}
	if stub.got.AvailableServices != nil {
		t.Error("services must be omitted when they cannot be fetched")
	}
	if got.Range == nil || got.Range.IsRelative() || !got.Range.From.Equal(time.UnixMilli(1793709000000)) {
		t.Errorf("range = %+v", got.Range)
	}
}
