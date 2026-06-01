package pgindex

import (
	"fmt"
	"strings"
)

type ReleaseReadyPolicy struct {
	MinMatchConfidence float64
	MinCompletionPct   float64
	MinIdentityStatus  string
	RequireInspection  bool
	RequireEnrichment  bool
}

func DefaultReleaseReadyPolicy() ReleaseReadyPolicy {
	return ReleaseReadyPolicy{
		MinMatchConfidence: 0.55,
		MinCompletionPct:   100,
		MinIdentityStatus:  "probable",
		RequireInspection:  false,
		RequireEnrichment:  false,
	}
}

func NormalizeReleaseReadyPolicy(in ReleaseReadyPolicy) ReleaseReadyPolicy {
	out := DefaultReleaseReadyPolicy()
	if in.MinMatchConfidence >= 0 && in.MinMatchConfidence <= 1 {
		out.MinMatchConfidence = in.MinMatchConfidence
	}
	if in.MinCompletionPct >= 0 && in.MinCompletionPct <= 100 {
		out.MinCompletionPct = in.MinCompletionPct
	}
	switch strings.TrimSpace(strings.ToLower(in.MinIdentityStatus)) {
	case "identified":
		out.MinIdentityStatus = "identified"
	case "probable":
		out.MinIdentityStatus = "probable"
	}
	out.RequireInspection = in.RequireInspection
	out.RequireEnrichment = in.RequireEnrichment
	return out
}

func releaseReadyVisibilityClause(alias string, policy ReleaseReadyPolicy) string {
	policy = NormalizeReleaseReadyPolicy(policy)

	identityClause := fmt.Sprintf("COALESCE(%s.identity_status, '') IN ('identified', 'probable')", alias)
	if policy.MinIdentityStatus == "identified" {
		identityClause = fmt.Sprintf("COALESCE(%s.identity_status, '') = 'identified'", alias)
	}

	clauses := []string{
		fmt.Sprintf("COALESCE(%s.search_title, '') <> ''", alias),
		fmt.Sprintf("LOWER(BTRIM(COALESCE(NULLIF(ro.display_title, ''), %s.title, ''))) <> 'unknown-release'", alias),
		fmt.Sprintf("COALESCE(%s.match_confidence, 0) >= %.4f", alias, policy.MinMatchConfidence),
		fmt.Sprintf("COALESCE(%s.completion_pct, 0) >= %.4f", alias, policy.MinCompletionPct),
		identityClause,
		fmt.Sprintf("COALESCE(%s.size_bytes, 0) > 0", alias),
		fmt.Sprintf("COALESCE(%s.category_id, 8010) <> 8010", alias),
		fmt.Sprintf(`(
			COALESCE(%[1]s.expected_file_count, 0) <= 1
			OR COALESCE(%[1]s.file_count, 0) >= 2
		)`, alias),
		fmt.Sprintf(`NOT (
			COALESCE(%[1]s.search_title, '') ~* '(^|[^a-z0-9])(seed|test)([^a-z0-9]|$)'
			OR COALESCE(%[1]s.group_name, '') ~* '(^|[._-])(seed|test)([._-]|$)'
		)`, alias),
		"COALESCE(ro.hidden, FALSE) = FALSE",
	}

	if policy.RequireInspection {
		clauses = append(clauses, fmt.Sprintf(
			"(%s.runtime_seconds > 0 OR %s.primary_resolution <> '' OR %s.primary_video_codec <> '' OR %s.primary_audio_codec <> '' OR %s.has_nfo = TRUE OR %s.has_par2 = TRUE)",
			alias, alias, alias, alias, alias, alias,
		))
	}
	if policy.RequireEnrichment {
		clauses = append(clauses, fmt.Sprintf(
			"(%s.tmdb_id > 0 OR %s.tvdb_id > 0 OR %s.external_media_type <> '' OR %s.matched_media_title <> '')",
			alias, alias, alias, alias,
		))
	}

	return strings.Join(clauses, "\n\t\tAND ")
}
