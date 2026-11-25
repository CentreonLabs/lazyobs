package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// TranslateRequest is the body of POST /v1/orgs/{org}/log-analysis/nlquery/translate.
type TranslateRequest struct {
	Prompt string `json:"prompt"`
	// AvailableServices grounds the model on the organization's service names.
	AvailableServices []string `json:"available_services,omitempty"`
}

// TranslateResponse is a natural-language prompt translated to a query.
type TranslateResponse struct {
	Query string `json:"query"`
	// TimeRange is a time hint found in the prompt: a number of milliseconds
	// to look back from now, or {start, end} in epoch milliseconds. Use
	// LookBack and Window to read it.
	TimeRange json.RawMessage `json:"time_range"`
	// Warning names what the query could not express, if anything.
	Warning *string `json:"warning"`
}

// LookBack returns the relative time hint, if the prompt had one.
func (r *TranslateResponse) LookBack() (time.Duration, bool) {
	var ms int64
	if len(r.TimeRange) == 0 || json.Unmarshal(r.TimeRange, &ms) != nil || ms <= 0 {
		return 0, false
	}
	return time.Duration(ms) * time.Millisecond, true
}

// Window returns the absolute time hint, if the prompt had one.
func (r *TranslateResponse) Window() (from, to time.Time, ok bool) {
	var w struct {
		Start *int64 `json:"start"`
		End   *int64 `json:"end"`
	}
	if len(r.TimeRange) == 0 || json.Unmarshal(r.TimeRange, &w) != nil || w.Start == nil || w.End == nil {
		return time.Time{}, time.Time{}, false
	}
	return time.UnixMilli(*w.Start), time.UnixMilli(*w.End), true
}

// TranslateQuery turns a natural-language description into a Quickwit query.
// It requires the Editor role.
func (c *Client) TranslateQuery(ctx context.Context, req TranslateRequest) (*TranslateResponse, error) {
	if req.Prompt == "" {
		return nil, errors.New("api: empty prompt")
	}
	var resp TranslateResponse
	if err := c.do(ctx, http.MethodPost, c.orgURL("log-analysis", "nlquery", "translate"), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
