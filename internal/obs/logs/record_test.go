package logs_test

import (
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
)

func TestSeverityFromNumber(t *testing.T) {
	cases := map[int]logs.Severity{
		0: logs.SeverityUnknown, 1: logs.SeverityTrace, 4: logs.SeverityTrace,
		5: logs.SeverityDebug, 9: logs.SeverityInfo, 13: logs.SeverityWarn,
		17: logs.SeverityError, 21: logs.SeverityFatal, 24: logs.SeverityFatal,
		25: logs.SeverityUnknown,
	}
	for n, want := range cases {
		if got := logs.SeverityFromNumber(n); got != want {
			t.Errorf("SeverityFromNumber(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestResolveSeverity(t *testing.T) {
	if got := logs.ResolveSeverity(13, "ERROR"); got != logs.SeverityWarn {
		t.Errorf("number must win over text, got %v", got)
	}
	if got := logs.ResolveSeverity(0, " warning "); got != logs.SeverityWarn {
		t.Errorf("text fallback, got %v", got)
	}
	if got := logs.ResolveSeverity(0, "chatty"); got != logs.SeverityUnknown {
		t.Errorf("unknown text, got %v", got)
	}
	if got := logs.Severity(42).String(); got != "UNKNOWN" {
		t.Errorf("out of range String() = %q", got)
	}
}

func TestFingerprint(t *testing.T) {
	ts := time.Unix(1793713556, 297395112)
	a := logs.Record{Timestamp: ts, Raw: []byte(`{"body":{"message":"a"}}`)}
	b := logs.Record{Timestamp: ts, Raw: []byte(`{"body":{"message":"a"}}`)}
	c := logs.Record{Timestamp: ts, Raw: []byte(`{"body":{"message":"b"}}`)}
	d := logs.Record{Timestamp: ts.Add(time.Nanosecond), Raw: a.Raw}

	if a.Fingerprint() != b.Fingerprint() {
		t.Error("identical records must share a fingerprint")
	}
	if a.Fingerprint() == c.Fingerprint() || a.Fingerprint() == d.Fingerprint() {
		t.Error("different records must not share a fingerprint")
	}

	noRaw1 := logs.Record{Timestamp: ts, Service: "api", Body: "x"}
	noRaw2 := logs.Record{Timestamp: ts, Service: "ap", Body: "ix"}
	if noRaw1.Fingerprint() == noRaw2.Fingerprint() {
		t.Error("field boundaries must be part of the fingerprint")
	}
}
