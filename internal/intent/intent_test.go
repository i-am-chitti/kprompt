package intent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseStructuredScale(t *testing.T) {
	raw := []byte(`{"kind":"scale","target":{"name":"api","namespace":"prod","kind":"Deployment"},"params":{"replicas":10},"confidence":0.9}`)
	in, err := ParseStructured(raw)
	if err != nil {
		t.Fatal(err)
	}
	if in.Kind != KindScale {
		t.Fatalf("kind=%s", in.Kind)
	}
	rep, ok := in.Replicas()
	if !ok || rep != 10 {
		t.Fatalf("replicas=%v ok=%v", rep, ok)
	}
}

func TestParseStructuredCoercesInventedKindsToUnknown(t *testing.T) {
	for _, kind := range []string{"hpascaleup", "autoscale", "patch", ""} {
		raw, err := json.Marshal(map[string]any{
			"kind":   kind,
			"target": map[string]string{"name": "redis"},
		})
		if err != nil {
			t.Fatal(err)
		}
		in, err := ParseStructured(raw)
		if err != nil {
			t.Fatalf("kind %q: %v", kind, err)
		}
		if in.Kind != KindUnknown {
			t.Fatalf("kind %q → %s, want unknown", kind, in.Kind)
		}
	}
}

func TestParseStructuredFoldsHPACase(t *testing.T) {
	in, err := ParseStructured([]byte(`{"kind":"HPA","target":{"name":"redis"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if in.Kind != KindHPA {
		t.Fatalf("kind=%s", in.Kind)
	}
}

func TestNormalizeKind(t *testing.T) {
	if NormalizeKind(KindScale) != KindScale {
		t.Fatal("scale should stay")
	}
	if NormalizeKind(Kind("HPA")) != KindHPA {
		t.Fatal("HPA should fold to hpa")
	}
	if NormalizeKind(Kind("hpascaleup")) != KindUnknown {
		t.Fatal("hpascaleup should become unknown")
	}
}

func TestSchemaIsValidJSON(t *testing.T) {
	if !json.Valid([]byte(SchemaJSON)) {
		t.Fatal("SchemaJSON invalid")
	}
}

func TestSchemaEnumMatchesExtractKinds(t *testing.T) {
	var schema struct {
		Properties struct {
			Kind struct {
				Enum []string `json:"enum"`
			} `json:"kind"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(SchemaJSON), &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Properties.Kind.Enum) != len(ExtractKinds) {
		t.Fatalf("schema enum len=%d ExtractKinds=%d", len(schema.Properties.Kind.Enum), len(ExtractKinds))
	}
	for _, k := range schema.Properties.Kind.Enum {
		if _, ok := ExtractKinds[Kind(k)]; !ok {
			t.Fatalf("schema kind %q missing from ExtractKinds", k)
		}
	}
	for k := range ExtractKinds {
		found := false
		for _, e := range schema.Properties.Kind.Enum {
			if e == string(k) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ExtractKind %q missing from SchemaJSON enum", k)
		}
	}
	// System prompt closed list should include every extract kind (order may differ).
	for k := range ExtractKinds {
		if !strings.Contains(systemPrompt, string(k)) {
			t.Fatalf("systemPrompt missing kind %q", k)
		}
	}
}
