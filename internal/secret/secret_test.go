package secret_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/CentreonLabs/lazyobs/internal/secret"
)

const raw = "s3cr3t-token-value"

func TestStringNeverLeaks(t *testing.T) {
	s := secret.New(raw)
	wrapper := struct {
		Token secret.String
		Ptr   *secret.String
	}{s, &s}

	outputs := map[string]string{}
	for _, verb := range []string{"%s", "%v", "%+v", "%#v", "%q", "%x", "%X", "%d"} {
		outputs["fmt "+verb] = fmt.Sprintf(verb, s)
		outputs["fmt struct "+verb] = fmt.Sprintf(verb, wrapper)
	}
	outputs["String"] = s.String()

	j, err := json.Marshal(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	outputs["json"] = string(j)

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("msg", "token", s, "wrapper", wrapper)
	outputs["slog"] = buf.String()

	for name, out := range outputs {
		if strings.Contains(out, raw) {
			t.Errorf("%s leaked the secret: %q", name, out)
		}
	}
	if !strings.Contains(outputs["json"], "[REDACTED]") {
		t.Errorf("json output should contain the placeholder, got %s", outputs["json"])
	}
}

func TestReveal(t *testing.T) {
	if got := secret.New(raw).Reveal(); got != raw {
		t.Fatalf("Reveal() = %q, want the raw value", got)
	}
}

func TestUnmarshalText(t *testing.T) {
	var v struct{ Token secret.String }
	if err := json.Unmarshal([]byte(`{"Token":"`+raw+`"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.Token.Reveal() != raw {
		t.Fatalf("decoded %q", v.Token.Reveal())
	}
}

func TestZero(t *testing.T) {
	var s secret.String
	if !s.IsZero() || s.String() != "" {
		t.Fatalf("zero value should be empty, got %q", s.String())
	}
	if secret.New(raw).IsZero() {
		t.Fatal("non-empty secret reported as zero")
	}
}
