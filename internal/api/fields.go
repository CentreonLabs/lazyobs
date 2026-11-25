package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Term is a value found in a field, with its number of occurrences.
type Term struct {
	Value any   `json:"value"`
	Count int64 `json:"count"`
}

// LogFieldTerms returns the most frequent values of a log field over [from, to),
// keeping those starting with prefix when it is not empty. The field must be
// aggregatable (see LogFieldCapabilities).
func (c *Client) LogFieldTerms(ctx context.Context, field, prefix string, from, to time.Time, maxTerms int) ([]Term, error) {
	return c.fieldTerms(ctx, "logs", field, prefix, from, to, maxTerms)
}

func (c *Client) fieldTerms(ctx context.Context, signal, field, prefix string, from, to time.Time, maxTerms int) ([]Term, error) {
	q := url.Values{}
	if prefix != "" {
		q.Set("filter", prefix)
	}
	q.Set("startTimestamp", strconv.FormatInt(from.Unix(), 10))
	q.Set("endTimestamp", strconv.FormatInt(to.Unix(), 10))
	q.Set("maxTerms", strconv.Itoa(min(max(maxTerms, 1), 100)))
	var terms []Term
	endpoint := c.orgURL(signal, "fields", field, "terms") + "?" + q.Encode()
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &terms); err != nil {
		return nil, err
	}
	return terms, nil
}

// FieldCapability describes one indexed type of a field.
type FieldCapability struct {
	Type         string `json:"type"`
	Searchable   bool   `json:"searchable"`
	Aggregatable bool   `json:"aggregatable"`
}

// LogFieldCapabilities lists the log fields seen over [from, to) (best
// effort: the window may be widened) with their capabilities per type.
func (c *Client) LogFieldCapabilities(ctx context.Context, from, to time.Time) (map[string]map[string]FieldCapability, error) {
	return c.fieldCapabilities(ctx, "logs", from, to)
}

func (c *Client) fieldCapabilities(ctx context.Context, signal string, from, to time.Time) (map[string]map[string]FieldCapability, error) {
	q := url.Values{}
	q.Set("startTimestamp", strconv.FormatInt(from.Unix(), 10))
	q.Set("endTimestamp", strconv.FormatInt(to.Unix(), 10))
	var resp struct {
		Fields map[string]map[string]FieldCapability `json:"fields"`
	}
	if err := c.do(ctx, http.MethodGet, c.orgURL(signal, "fields", "capabilities")+"?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Fields, nil
}

// String renders a term value.
func (t Term) String() string {
	if s, ok := t.Value.(string); ok {
		return s
	}
	return fmt.Sprint(t.Value)
}
