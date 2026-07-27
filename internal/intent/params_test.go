package intent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStringParam(t *testing.T) {
	in := Intent{Params: map[string]any{"chart": "bitnami/redis", "empty": "", "n": 42}}
	if v, ok := in.StringParam("chart"); !ok || v != "bitnami/redis" {
		t.Fatalf("chart: %v %v", v, ok)
	}
	if _, ok := in.StringParam("empty"); ok {
		t.Fatal("empty string should be missing")
	}
	if _, ok := in.StringParam("missing"); ok {
		t.Fatal("missing key")
	}
	if v, ok := in.StringParam("n"); !ok || v != "42" {
		t.Fatalf("coerced: %v %v", v, ok)
	}
}

func TestPortLimitTimeoutCoercion(t *testing.T) {
	in := Intent{Params: map[string]any{
		"port":    float64(8080),
		"limit":   json.Number("50"),
		"timeout": "30s",
		"tail":    100,
	}}
	if p, ok := in.Port(); !ok || p != 8080 {
		t.Fatalf("port=%v ok=%v", p, ok)
	}
	if lim, ok := in.Limit(); !ok || lim != 50 {
		t.Fatalf("limit=%v ok=%v", lim, ok)
	}
	if d, ok := in.Timeout(); !ok || d != 30*time.Second {
		t.Fatalf("timeout=%v ok=%v", d, ok)
	}
	if tail, ok := in.TailLines(); !ok || tail != 100 {
		t.Fatalf("tail=%v ok=%v", tail, ok)
	}
	if _, ok := (Intent{}).Port(); ok {
		t.Fatal("missing port")
	}
}

func TestWantService(t *testing.T) {
	if !(Intent{Params: map[string]any{"port": float64(80)}}).WantService() {
		t.Fatal("port implies service")
	}
	if !(Intent{Params: map[string]any{"createService": true}}).WantService() {
		t.Fatal("createService true")
	}
	if !(Intent{Params: map[string]any{"createService": "true"}}).WantService() {
		t.Fatal("createService string")
	}
	if (Intent{Params: map[string]any{"createService": false}}).WantService() {
		t.Fatal("createService false")
	}
}

func TestWantGPU(t *testing.T) {
	if !(Intent{Params: map[string]any{"gpu": true}}).WantGPU() {
		t.Fatal("gpu true")
	}
	if (Intent{}).WantGPU() {
		t.Fatal("missing gpu")
	}
}

func TestStringSliceParam(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want []string
		ok   bool
	}{
		{"nil", nil, nil, false},
		{"[]string", []string{"a", "b"}, []string{"a", "b"}, true},
		{"[]any", []any{"x", 1}, []string{"x", "1"}, true},
		{"string", "solo", []string{"solo"}, true},
		{"empty []string", []string{}, nil, false},
		{"blank string", "  ", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := stringSliceParam(tc.v)
			if ok != tc.ok {
				t.Fatalf("ok=%v want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("%v vs %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("%v vs %v", got, tc.want)
				}
			}
		})
	}
	in := Intent{Params: map[string]any{"command": []any{"python", "train.py"}, "args": "--epochs 2"}}
	cmd, ok := in.Command()
	if !ok || len(cmd) != 2 || cmd[0] != "python" {
		t.Fatalf("command=%v ok=%v", cmd, ok)
	}
	args, ok := in.Args()
	if !ok || len(args) != 1 || args[0] != "--epochs 2" {
		t.Fatalf("args=%v ok=%v", args, ok)
	}
}
