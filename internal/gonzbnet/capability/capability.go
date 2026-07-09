package capability

import "strings"

const (
	Consumer            = "consumer"
	Scanner             = "scanner"
	Indexer             = "indexer"
	ManifestBuilder     = "manifest_builder"
	ManifestCache       = "manifest_cache"
	Validator           = "validator"
	HealthChecker       = "health_checker"
	Coverage            = "coverage"
	Scheduler           = "scheduler"
	Relay               = "relay"
	CoverageCoordinator = "coverage_coordinator"
	Admin               = "admin"
)

func Normalize(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func HasAny(allowed []string, required ...string) bool {
	allowed = Normalize(allowed)
	if len(required) == 0 {
		return true
	}
	for _, have := range allowed {
		for _, need := range required {
			if have == strings.TrimSpace(need) {
				return true
			}
		}
	}
	return false
}

func RequiredForEvent(eventType string) []string {
	switch strings.TrimSpace(eventType) {
	case "ReleaseCard":
		return []string{Scanner, Indexer}
	case "ResolutionManifest":
		return []string{ManifestBuilder, ManifestCache}
	case "HealthAttestation":
		return []string{Validator, HealthChecker}
	case "Tombstone":
		return []string{Admin}
	default:
		return nil
	}
}
