package crdstatus

import "testing"

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("KPROMPT_AGENT_CR", "demo")
	t.Setenv("KPROMPT_AGENT_CR_NAMESPACE", "")
	t.Setenv("POD_NAMESPACE", "payments")
	cfg := FromEnv()
	if cfg.Name != "demo" || cfg.Namespace != "payments" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestEnabled(t *testing.T) {
	var s *Syncer
	if s.Enabled() {
		t.Fatal("nil syncer")
	}
	s = New(nil, Config{Name: "x"})
	if s.Enabled() {
		t.Fatal("nil dyn")
	}
	s = New(nil, Config{})
	if s.Enabled() {
		t.Fatal("empty name")
	}
}
