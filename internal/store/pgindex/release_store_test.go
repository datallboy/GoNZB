package pgindex

import "testing"

func TestBaseStemStaleCleanupRequiresReplacementGroups(t *testing.T) {
	if canDeleteStaleReleasesForCandidate(ReleaseCandidateKeyKindBaseStem, nil) {
		t.Fatal("base-stem fallback must not delete releases without replacement groups")
	}
	if !canDeleteStaleReleasesForCandidate(ReleaseCandidateKeyKindBaseStem, []string{"replacement"}) {
		t.Fatal("base-stem cleanup should be allowed when replacement groups are retained")
	}
	if !canDeleteStaleReleasesForCandidate(ReleaseCandidateKeyKindReleaseFamily, nil) {
		t.Fatal("release-family candidates must retain authoritative stale cleanup")
	}
}
