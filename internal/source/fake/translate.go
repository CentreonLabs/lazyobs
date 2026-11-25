package fake

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
)

// Translator is a keyword-based stand-in for the platform's natural-language
// translation, good enough for the demo and tests: it recognizes levels,
// service names, words in quotes and "last N minutes/hours/days".
type Translator struct{}

var _ obs.QueryTranslator = Translator{}

var lastPattern = regexp.MustCompile(`(?:last|past)\s+(\d+)?\s*(minute|min|hour|day|week)s?`)

// Translate implements obs.QueryTranslator.
func (Translator) Translate(ctx context.Context, prompt string) (obs.Translation, error) {
	if err := ctx.Err(); err != nil {
		return obs.Translation{}, err
	}
	p := strings.ToLower(strings.TrimSpace(prompt))
	if p == "" {
		return obs.Translation{}, errors.New("empty prompt")
	}
	var terms []string
	for _, word := range []struct {
		words []string
		sev   logs.Severity
	}{
		{[]string{"fatal", "crash"}, logs.SeverityFatal},
		{[]string{"error", "fail"}, logs.SeverityError},
		{[]string{"warn"}, logs.SeverityWarn},
		{[]string{"debug"}, logs.SeverityDebug},
	} {
		for _, w := range word.words {
			if strings.Contains(p, w) {
				terms = append(terms, "severity_text:"+word.sev.String())
				break
			}
		}
		if len(terms) > 0 {
			break
		}
	}
	for _, svc := range services {
		if strings.Contains(p, svc.name) {
			terms = append(terms, "service_name:"+svc.name)
		}
	}
	for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(prompt, -1) {
		terms = append(terms, fmt.Sprintf("%q", q[1]))
	}

	tr := obs.Translation{Query: strings.Join(terms, " AND ")}
	if tr.Query == "" {
		tr.Query = "*"
		tr.Warning = "nothing in the prompt matched a level, a service or a quoted text"
	}
	if m := lastPattern.FindStringSubmatch(p); m != nil {
		n := 1
		if m[1] != "" {
			n, _ = strconv.Atoi(m[1])
		}
		unit := map[string]time.Duration{"minute": time.Minute, "min": time.Minute, "hour": time.Hour, "day": 24 * time.Hour, "week": 7 * 24 * time.Hour}[m[2]]
		r := obs.Last(time.Duration(n) * unit)
		tr.Range = &r
	}
	return tr, nil
}
