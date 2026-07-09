package pgindex

import (
	"testing"

	"github.com/datallboy/gonzb/internal/gonzbnet/capability"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
)

func TestPoolMemberCapabilityAllowed(t *testing.T) {
	if !poolMemberCapabilityAllowed(pools.RoleAdmin, nil, capability.RequiredForEvent("Tombstone")) {
		t.Fatalf("admin should satisfy contribution capabilities")
	}
	if poolMemberCapabilityAllowed(pools.RoleMember, nil, capability.RequiredForEvent("ReleaseCard")) {
		t.Fatalf("member without allowed capabilities should be consumer-only")
	}
	if !poolMemberCapabilityAllowed(pools.RoleMember, []string{capability.Scanner}, capability.RequiredForEvent("ReleaseCard")) {
		t.Fatalf("scanner member should be allowed to publish release cards")
	}
	if !poolMemberCapabilityAllowed(pools.RoleMember, []string{capability.ManifestCache}, capability.RequiredForEvent("ResolutionManifest")) {
		t.Fatalf("manifest cache member should be allowed to publish manifests")
	}
	if poolMemberCapabilityAllowed(pools.RoleMember, []string{capability.Validator}, capability.RequiredForEvent("ReleaseCard")) {
		t.Fatalf("validator-only member should not publish release cards")
	}
}

func TestDefaultAllowedCapabilities(t *testing.T) {
	if got := defaultAllowedCapabilities(pools.RoleMember, nil); len(got) != 0 {
		t.Fatalf("member default capabilities = %v, want none", got)
	}
	got := defaultAllowedCapabilities(pools.RoleAdmin, nil)
	if !capability.HasAny(got, capability.Admin) || !capability.HasAny(got, capability.Scanner) {
		t.Fatalf("admin defaults should include admin and scanner, got %v", got)
	}
	got = defaultAllowedCapabilities(pools.RoleAdmin, []string{capability.Validator, capability.Validator, " "})
	if len(got) != 1 || got[0] != capability.Validator {
		t.Fatalf("explicit capabilities should be normalized and preserved, got %v", got)
	}
}
