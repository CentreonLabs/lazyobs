package seed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/secret"
)

// DefaultEndpoint is the OTLP/HTTP ingestion base URL of the platform.
const DefaultEndpoint = "https://otlp.euwest1.obs.mycentreon.com/v1/ingress/otlp"

// Sender posts OTLP/JSON export requests. The organization is the token's.
type Sender struct {
	Endpoint string // base URL, without /v1/logs or /v1/traces
	Token    secret.String
	HTTP     *http.Client
	// Retries is the number of retries after a 429 or a 5xx.
	Retries int
	// Sleep waits between retries; nil uses a timer honoring ctx.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Logs sends a logs export request.
func (s *Sender) Logs(ctx context.Context, req map[string]any) error {
	return s.post(ctx, "/v1/logs", req)
}

// Traces sends a traces export request.
func (s *Sender) Traces(ctx context.Context, req map[string]any) error {
	return s.post(ctx, "/v1/traces", req)
}

var retryAfter = regexp.MustCompile(`retry after (\d+)s`)

func (s *Sender) post(ctx context.Context, path string, req map[string]any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	delay := time.Second
	for attempt := 0; ; attempt++ {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Endpoint+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Api-Key", s.Token.Reveal())
		resp, err := client.Do(r)
		if err != nil {
			return fmt.Errorf("seed: %s: %w", path, err)
		}
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode/100 == 2 {
			return nil
		}
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if !retryable || attempt >= s.Retries {
			return fmt.Errorf("seed: %s: %s: %s", path, resp.Status, bytes.TrimSpace(msg))
		}
		wait := delay
		if v, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			wait = time.Duration(v) * time.Second
		} else if m := retryAfter.FindSubmatch(msg); m != nil {
			v, _ := strconv.Atoi(string(m[1]))
			wait = time.Duration(v) * time.Second
		}
		if err := s.sleep(ctx, max(wait, delay)); err != nil {
			return err
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func (s *Sender) sleep(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
