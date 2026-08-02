package remember

import (
	"path/filepath"
	"testing"
)

func TestUpsertForgetAndBias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")

	f, err := UpsertPath(path, "owner", "Team A", "payments")
	if err != nil {
		t.Fatal(err)
	}
	if f.Key != "owner" || f.Value != "Team A" {
		t.Fatalf("%+v", f)
	}
	_, err = UpsertPath(path, "owner", "Team B", "payments")
	if err != nil {
		t.Fatal(err)
	}
	s, err := LoadPath(path)
	if err != nil || len(s.Facts) != 1 || s.Facts[0].Value != "Team B" {
		t.Fatalf("%+v %v", s, err)
	}
	n, err := ForgetPath(path, "owner", "payments")
	if err != nil || n != 1 {
		t.Fatalf("removed=%d err=%v", n, err)
	}
}

func TestParseStatement(t *testing.T) {
	k, v, err := ParseStatement("payment ns = Team A")
	if err != nil || k != "payment ns" || v != "Team A" {
		t.Fatalf("%q %q %v", k, v, err)
	}
	k, v, err = ParseStatement("tier: gold")
	if err != nil || k != "tier" || v != "gold" {
		t.Fatalf("%q %q %v", k, v, err)
	}
}
