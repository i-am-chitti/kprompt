package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmApplyYes(t *testing.T) {
	var out bytes.Buffer
	ok, err := ConfirmApply(strings.NewReader("y\n"), &out)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v out=%q", ok, err, out.String())
	}
	if !strings.Contains(out.String(), "Apply this plan?") {
		t.Fatalf("missing prompt: %q", out.String())
	}
}

func TestConfirmApplyYesWord(t *testing.T) {
	ok, err := ConfirmApply(strings.NewReader("yes\n"), ioDiscard{})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestConfirmApplyNoAndEmpty(t *testing.T) {
	for _, in := range []string{"n\n", "\n", "no\n", "maybe\n"} {
		ok, err := ConfirmApply(strings.NewReader(in), ioDiscard{})
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatalf("expected abort for %q", in)
		}
	}
}

func TestConfirmApplyEOF(t *testing.T) {
	ok, err := ConfirmApply(strings.NewReader(""), ioDiscard{})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("EOF should abort")
	}
}

func TestConfirmPhraseExact(t *testing.T) {
	var out bytes.Buffer
	ok, err := ConfirmPhrase(strings.NewReader("DELETE-ORPHANS\n"), &out, "DELETE-ORPHANS")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v out=%q", ok, err, out.String())
	}
	if !strings.Contains(out.String(), "DELETE-ORPHANS") {
		t.Fatalf("missing phrase prompt: %q", out.String())
	}
}

func TestConfirmPhraseMismatch(t *testing.T) {
	for _, in := range []string{"y\n", "delete-orphans\n", "DELETE ORPHANS\n", "\n", ""} {
		ok, err := ConfirmPhrase(strings.NewReader(in), ioDiscard{}, "DELETE-ORPHANS")
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatalf("expected reject for %q", in)
		}
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
