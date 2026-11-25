package logs

import "strings"

// Severity is a normalized log level, following the OpenTelemetry severity ranges.
type Severity int

// Severity levels, ordered from least to most severe.
const (
	SeverityUnknown Severity = iota
	SeverityTrace
	SeverityDebug
	SeverityInfo
	SeverityWarn
	SeverityError
	SeverityFatal
)

var severityNames = [...]string{"UNKNOWN", "TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"}

func (s Severity) String() string {
	if s < 0 || int(s) >= len(severityNames) {
		return severityNames[0]
	}
	return severityNames[s]
}

// SeverityFromNumber maps an OpenTelemetry severity number (1–24) to a level.
func SeverityFromNumber(n int) Severity {
	if n < 1 || n > 24 {
		return SeverityUnknown
	}
	return Severity((n-1)/4 + 1)
}

// ParseSeverity maps a free-form severity text to a level.
func ParseSeverity(text string) Severity {
	switch strings.ToUpper(strings.TrimSpace(text)) {
	case "TRACE", "TRC":
		return SeverityTrace
	case "DEBUG", "DBG":
		return SeverityDebug
	case "INFO", "INFORMATION", "NOTICE", "INF":
		return SeverityInfo
	case "WARN", "WARNING", "WRN":
		return SeverityWarn
	case "ERROR", "ERR":
		return SeverityError
	case "FATAL", "CRITICAL", "CRIT", "ALERT", "EMERGENCY", "PANIC":
		return SeverityFatal
	default:
		return SeverityUnknown
	}
}

// ResolveSeverity prefers the severity number and falls back to the text.
func ResolveSeverity(number int, text string) Severity {
	if s := SeverityFromNumber(number); s != SeverityUnknown {
		return s
	}
	return ParseSeverity(text)
}
