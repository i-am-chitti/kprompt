package recipe

import (
	"strings"
	"testing"
)

func TestMatchHarden(t *testing.T) {
	r, ok := Match("please harden production in payments")
	if !ok || r.ID != "harden-production" {
		t.Fatalf("got ok=%v id=%s", ok, r.ID)
	}
}

func TestMatchBlackFriday(t *testing.T) {
	r, ok := Match("prepare for black friday")
	if !ok || r.ID != "prepare-black-friday" {
		t.Fatalf("got %+v ok=%v", r, ok)
	}
}

func TestExpandNamespace(t *testing.T) {
	r, _ := Lookup("harden-production")
	steps, err := r.Expand("payments", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 || !strings.Contains(steps[0], "payments") {
		t.Fatalf("%v", steps)
	}
}

func TestExpandWorkloadRequired(t *testing.T) {
	r, _ := Lookup("crashloop-rca")
	_, err := r.Expand("payments", "")
	if err == nil {
		t.Fatal("expected workload error")
	}
	steps, err := r.Expand("payments", "api")
	if err != nil || !strings.Contains(steps[0], "api") {
		t.Fatalf("%v %v", steps, err)
	}
}

func TestTryRoute(t *testing.T) {
	steps, r, ok, err := TryRoute("harden my cluster", "shop", "")
	if err != nil || !ok || r.ID != "harden-production" || len(steps) != 3 {
		t.Fatalf("steps=%v r=%s ok=%v err=%v", steps, r.ID, ok, err)
	}
	_, _, ok, err = TryRoute("crashloop recipe", "payments", "")
	if !ok || err == nil {
		t.Fatalf("expected workload err: ok=%v err=%v", ok, err)
	}
	steps, _, ok, err = TryRoute("crashloop recipe for api", "payments", "")
	if err != nil || !ok || len(steps) != 3 {
		t.Fatalf("steps=%v ok=%v err=%v", steps, ok, err)
	}
}

func TestCatalogStable(t *testing.T) {
	c := Catalog()
	if len(c) < 6 {
		t.Fatalf("len=%d", len(c))
	}
	if c[0].ID > c[len(c)-1].ID {
		t.Fatal("not sorted")
	}
}
