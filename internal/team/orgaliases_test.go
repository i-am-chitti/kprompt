package team

import "testing"

func TestMergeAliasesOrgWins(t *testing.T) {
	got := MergeAliases(
		map[string]string{"prod": "gke_org"},
		map[string]string{"prod": "gke_local", "dev": "kind-dev"},
	)
	if got["prod"] != "gke_org" {
		t.Fatalf("org should win: %v", got)
	}
	if got["dev"] != "kind-dev" {
		t.Fatalf("local-only kept: %v", got)
	}
}
