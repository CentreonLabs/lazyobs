// Package logs is the log signal domain: a log record modeled on the
// OpenTelemetry log data model, and its severity.
package logs

import (
	"encoding/binary"
	"hash/fnv"
	"time"
)

// Record is a single log entry.
type Record struct {
	Timestamp         time.Time
	ObservedTimestamp time.Time

	Severity       Severity
	SeverityText   string // as sent by the producer, may differ from Severity.String()
	SeverityNumber int

	Service string
	Body    string // human-readable message, flattened from the structured body

	Attributes         map[string]any
	ResourceAttributes map[string]any
	ScopeName          string
	ScopeVersion       string

	TraceID    string
	SpanID     string
	TraceFlags int

	// Raw is the document as returned by the backend, used for the detail view
	// and copy actions. It may be nil for records not coming from an API.
	Raw []byte
}

// Fingerprint identifies a record for deduplication. The platform exposes no
// document ID, so it hashes the timestamp with the raw document (or with the
// main fields when no raw document is available).
func (r Record) Fingerprint() uint64 {
	h := fnv.New64a()
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(r.Timestamp.UnixNano())) //nolint:gosec // bit pattern only
	_, _ = h.Write(ts[:])
	if len(r.Raw) > 0 {
		_, _ = h.Write(r.Raw)
	} else {
		for _, s := range []string{r.Service, r.SeverityText, r.Body, r.TraceID, r.SpanID} {
			_, _ = h.Write([]byte(s))
			_, _ = h.Write([]byte{0})
		}
	}
	return h.Sum64()
}
