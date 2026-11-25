package remote

import (
	"context"
	"sync"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/obs"
)

// serviceLookBack is how far back service names are collected to ground translations.
const serviceLookBack = 7 * 24 * time.Hour

// TranslatorAPI is the subset of the API client used by Translator.
type TranslatorAPI interface {
	TranslateQuery(ctx context.Context, req api.TranslateRequest) (*api.TranslateResponse, error)
	LogFieldTerms(ctx context.Context, field string, from, to time.Time, maxTerms int) ([]api.Term, error)
}

// Translator turns natural language into log queries with the platform's
// translation service, grounded on the organization's service names.
type Translator struct {
	api TranslatorAPI
	now func() time.Time

	mu       sync.Mutex
	services []string // cached once fetched successfully
}

var _ obs.QueryTranslator = (*Translator)(nil)

// NewTranslator returns a translator. A nil now uses time.Now.
func NewTranslator(client TranslatorAPI, now func() time.Time) *Translator {
	if now == nil {
		now = time.Now
	}
	return &Translator{api: client, now: now}
}

// Translate implements obs.QueryTranslator.
func (t *Translator) Translate(ctx context.Context, prompt string) (obs.Translation, error) {
	resp, err := t.api.TranslateQuery(ctx, api.TranslateRequest{Prompt: prompt, AvailableServices: t.knownServices(ctx)})
	if err != nil {
		return obs.Translation{}, err
	}
	tr := obs.Translation{Query: resp.Query}
	if resp.Warning != nil {
		tr.Warning = *resp.Warning
	}
	if d, ok := resp.LookBack(); ok {
		r := obs.Last(d)
		tr.Range = &r
	} else if from, to, ok := resp.Window(); ok {
		r := obs.Between(from, to)
		tr.Range = &r
	}
	return tr, nil
}

// knownServices returns the service names seen recently. Grounding is a
// best effort: on failure the translation proceeds without it.
func (t *Translator) knownServices(ctx context.Context) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.services != nil {
		return t.services
	}
	now := t.now()
	terms, err := t.api.LogFieldTerms(ctx, "service_name", now.Add(-serviceLookBack), now, 100)
	if err != nil {
		return nil
	}
	t.services = make([]string, 0, len(terms))
	for _, term := range terms {
		t.services = append(t.services, term.String())
	}
	return t.services
}
