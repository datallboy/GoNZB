package pgindex

import (
	"errors"
	"testing"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
)

func TestValidateFederationChainAcceptsKnownPredecessor(t *testing.T) {
	previous := "evt_previous"
	decision, err := validateFederationChain(&events.SignedEvent{
		EventID:         "evt_current",
		AuthorNodeID:    "node_author",
		Sequence:        2,
		PreviousEventID: &previous,
	}, federationChainState{
		Predecessor: &federationChainLink{EventID: previous, AuthorNodeID: "node_author", Sequence: 1},
	})
	if err != nil || decision.Gap {
		t.Fatalf("expected valid contiguous chain, decision=%+v err=%v", decision, err)
	}
}

func TestValidateFederationChainAllowsPartialSyncGap(t *testing.T) {
	previous := "evt_not_received"
	decision, err := validateFederationChain(&events.SignedEvent{
		EventID:         "evt_current",
		AuthorNodeID:    "node_author",
		Sequence:        8,
		PreviousEventID: &previous,
	}, federationChainState{})
	if err != nil || !decision.Gap || decision.ExpectedSequence != 7 {
		t.Fatalf("expected tracked sequence gap, decision=%+v err=%v", decision, err)
	}
}

func TestValidateFederationChainRejectsKnownPredecessorMismatch(t *testing.T) {
	wrong := "evt_wrong"
	_, err := validateFederationChain(&events.SignedEvent{
		EventID:         "evt_current",
		AuthorNodeID:    "node_author",
		Sequence:        2,
		PreviousEventID: &wrong,
	}, federationChainState{
		Predecessor: &federationChainLink{EventID: "evt_previous", AuthorNodeID: "node_author", Sequence: 1},
	})
	if !errors.Is(err, ErrFederationForkDetected) {
		t.Fatalf("expected fork detection, got %v", err)
	}
}

func TestValidateFederationChainRejectsMismatchedKnownSuccessor(t *testing.T) {
	_, err := validateFederationChain(&events.SignedEvent{
		EventID:      "evt_current",
		AuthorNodeID: "node_author",
		Sequence:     2,
	}, federationChainState{
		Successor: &federationChainLink{EventID: "evt_next", AuthorNodeID: "node_author", Sequence: 3, PreviousEventID: "evt_other"},
	})
	if !errors.Is(err, ErrFederationForkDetected) {
		t.Fatalf("expected successor fork detection, got %v", err)
	}
}

func TestValidateFederationChainRejectsSameSequenceFork(t *testing.T) {
	_, err := validateFederationChain(&events.SignedEvent{
		EventID:      "evt_current",
		AuthorNodeID: "node_author",
		Sequence:     2,
	}, federationChainState{
		SameSequence: &federationChainLink{EventID: "evt_other", AuthorNodeID: "node_author", Sequence: 2},
	})
	if !errors.Is(err, ErrFederationSequenceConflict) {
		t.Fatalf("expected sequence conflict, got %v", err)
	}
}
