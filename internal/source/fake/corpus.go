package fake

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
)

type service struct {
	name    string
	version string
	routes  []string
}

var services = []service{
	{"api-gateway", "3.4.1", []string{"/v1/orders", "/v1/users/{id}", "/healthz"}},
	{"checkout", "1.12.0", []string{"/cart", "/cart/checkout", "/payment"}},
	{"inventory", "2.0.3", []string{"/items", "/items/{sku}", "/reservations"}},
	{"auth", "5.1.0", []string{"/login", "/token/refresh", "/logout"}},
	{"notifier", "0.9.7", []string{"/send", "/templates"}},
}

var methods = []string{"GET", "GET", "GET", "POST", "PUT", "DELETE"}

// Severity weights out of 100: mostly INFO, a few errors.
var severityWeights = []struct {
	sev    logs.Severity
	weight int
}{
	{logs.SeverityTrace, 5},
	{logs.SeverityDebug, 20},
	{logs.SeverityInfo, 55},
	{logs.SeverityWarn, 12},
	{logs.SeverityError, 7},
	{logs.SeverityFatal, 1},
}

// pickSeverity draws a level; during an incident a third of the logs are errors.
func pickSeverity(r *rand.Rand, incident bool) logs.Severity {
	if incident && r.IntN(3) == 0 {
		return logs.SeverityError
	}
	n := r.IntN(100)
	for _, w := range severityWeights {
		if n < w.weight {
			return w.sev
		}
		n -= w.weight
	}
	return logs.SeverityInfo
}

var messages = map[logs.Severity][]string{
	logs.SeverityTrace: {"entering handler {route}", "cache lookup key=user:{id}"},
	logs.SeverityDebug: {"query took {ms}ms rows={n}", "retry budget left: {n}", "feature flag new-pricing evaluated to false"},
	logs.SeverityInfo: {
		"request completed in {ms}ms", "user {id} logged in", "order {id} created",
		"published event order.created id={id}", "health check ok",
	},
	logs.SeverityWarn: {"slow query detected: {ms}ms", "retrying upstream call (attempt {n})", "connection pool at {n}0% capacity"},
	logs.SeverityError: {
		"upstream timeout after {ms}ms", "payment declined for order {id}",
		"failed to reserve item {id}: out of stock", "database connection reset by peer",
	},
	logs.SeverityFatal: {"cannot open database: too many connections", "out of memory, shutting down"},
}

func fillTemplate(tmpl string, r *rand.Rand) string {
	return strings.NewReplacer(
		"{ms}", fmt.Sprint(r.IntN(2000)+3),
		"{n}", fmt.Sprint(r.IntN(9)+1),
		"{id}", fmt.Sprintf("%06d", r.IntN(1_000_000)),
		"{route}", "/v1/orders",
	).Replace(tmpl)
}

func statusFor(sev logs.Severity, r *rand.Rand) int {
	switch sev {
	case logs.SeverityError, logs.SeverityFatal:
		return []int{500, 502, 503, 504}[r.IntN(4)]
	case logs.SeverityWarn:
		return []int{200, 429, 404}[r.IntN(3)]
	default:
		return []int{200, 200, 201, 204}[r.IntN(4)]
	}
}
