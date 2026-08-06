package autopilot

import (
	"fmt"
	"testing"
)

func TestProposalLibraryPutGetList(t *testing.T) {
	lib := NewProposalLibrary(NewMemProposalStore())
	prop := Proposal{
		ID: "ap-1", Namespace: "payments", ActionID: ActionRestartDeployment,
		Decision: DecisionProposed, TargetName: "api", Confidence: 0.9,
	}
	if err := lib.Put(prop); err != nil {
		t.Fatal(err)
	}
	got, err := lib.Get("payments", "ap-1")
	if err != nil || got.ActionID != ActionRestartDeployment {
		t.Fatalf("%v %+v", err, got)
	}
	snap, err := lib.List("payments")
	if err != nil || len(snap.Proposals) != 1 {
		t.Fatalf("%v %+v", err, snap)
	}
	prop.Decision = DecisionApplied
	prop.Applied = true
	if err := lib.Put(prop); err != nil {
		t.Fatal(err)
	}
	got, err = lib.Get("payments", "ap-1")
	if err != nil || !got.Applied || got.Decision != DecisionApplied {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestProposalLibraryCap(t *testing.T) {
	lib := NewProposalLibrary(NewMemProposalStore())
	for i := 0; i < ProposalsMax+5; i++ {
		p := Proposal{ID: fmt.Sprintf("ap-%d", i), Namespace: "ns", ActionID: ActionEvictPod}
		if err := lib.Put(p); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := lib.List("ns")
	if err != nil || len(snap.Proposals) != ProposalsMax {
		t.Fatalf("cap: len=%d err=%v", len(snap.Proposals), err)
	}
}
