package pgindex

import (
	"fmt"
	"strings"
)

type ReleaseReadyPolicy struct {
	MinMatchConfidence                   float64
	MinCompletionPct                     float64
	MinIdentityStatus                    string
	RequireInspection                    bool
	RequireEnrichment                    bool
	RequireClearTitle                    bool
	RequirePayloadComplete               bool
	RequireExpectedFileCountComplete     bool
	RequirePAR2                          bool
	RequireNFO                           bool
	RequireSFV                           bool
	RetainUntilExpectedFileCountComplete bool
	RetainRequirePAR2                    bool
	RetainRequireNFO                     bool
	RetainRequireSFV                     bool
}

func DefaultReleaseReadyPolicy() ReleaseReadyPolicy {
	return ReleaseReadyPolicy{
		MinMatchConfidence:                   0.55,
		MinCompletionPct:                     100,
		MinIdentityStatus:                    "probable",
		RequireInspection:                    true,
		RequireEnrichment:                    false,
		RequireClearTitle:                    true,
		RequirePayloadComplete:               true,
		RequireExpectedFileCountComplete:     false,
		RequirePAR2:                          false,
		RequireNFO:                           false,
		RequireSFV:                           false,
		RetainUntilExpectedFileCountComplete: false,
		RetainRequirePAR2:                    false,
		RetainRequireNFO:                     false,
		RetainRequireSFV:                     false,
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
	out.RequireClearTitle = in.RequireClearTitle
	out.RequirePayloadComplete = in.RequirePayloadComplete
	out.RequireExpectedFileCountComplete = in.RequireExpectedFileCountComplete
	out.RequirePAR2 = in.RequirePAR2
	out.RequireNFO = in.RequireNFO
	out.RequireSFV = in.RequireSFV
	out.RetainUntilExpectedFileCountComplete = in.RetainUntilExpectedFileCountComplete
	out.RetainRequirePAR2 = in.RetainRequirePAR2
	out.RetainRequireNFO = in.RetainRequireNFO
	out.RetainRequireSFV = in.RetainRequireSFV
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
		fmt.Sprintf(`(
			EXISTS (
				SELECT 1
				FROM release_files rf
				WHERE rf.release_id = %[1]s.release_id
			)
			OR EXISTS (
				SELECT 1
				FROM release_archive_state ras
				WHERE ras.release_id = %[1]s.release_id
				  AND ras.archive_status IN ('archived', 'purge_pending', 'purged')
				  AND COALESCE(ras.object_key, '') <> ''
			)
		)`, alias),
		fmt.Sprintf("LOWER(BTRIM(COALESCE(NULLIF(ro.display_title, ''), %s.title, ''))) <> 'unknown-release'", alias),
		fmt.Sprintf("COALESCE(%s.match_confidence, 0) >= %.4f", alias, policy.MinMatchConfidence),
		fmt.Sprintf("COALESCE(%s.completion_pct, 0) >= %.4f", alias, policy.MinCompletionPct),
		identityClause,
		fmt.Sprintf("COALESCE(%s.size_bytes, 0) > 0", alias),
		fmt.Sprintf(`(
			COALESCE(%[1]s.expected_file_count, 0) <= 1
			OR COALESCE(%[1]s.file_count, 0) >= 2
		)`, alias),
		fmt.Sprintf(`NOT (
			COALESCE(%[1]s.search_title, '') ~* '(^|[^a-z0-9])(seed|test)([^a-z0-9]|$)'
			OR COALESCE(%[1]s.group_name, '') ~* '(^|[._-])(seed|test)([._-]|$)'
		)`, alias),
		probableWeakTitleClause(alias),
		opaqueTitleNeedsEvidenceClause(alias),
		fmt.Sprintf("%s IN ('not_passworded', 'password_known')", releasePasswordStateSQL(alias)),
		"COALESCE(ro.hidden, FALSE) = FALSE",
	}

	if policy.RequireClearTitle {
		clauses = append(clauses, clearTextReleaseTitleClause(alias))
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
	if policy.RequirePayloadComplete {
		clauses = append(clauses, payloadCompleteClause(alias))
	}
	if policy.RequireExpectedFileCountComplete {
		clauses = append(clauses, expectedFileCountCompleteClause(alias))
	}
	if policy.RequirePAR2 {
		clauses = append(clauses, fmt.Sprintf("COALESCE(%s.has_par2, FALSE) = TRUE", alias))
	}
	if policy.RequireNFO {
		clauses = append(clauses, fmt.Sprintf("COALESCE(%s.has_nfo, FALSE) = TRUE", alias))
	}
	if policy.RequireSFV {
		clauses = append(clauses, hasSFVClause(alias))
	}

	return strings.Join(clauses, "\n\t\tAND ")
}

func clearTextReleaseTitleClause(alias string) string {
	effectiveTitle := fmt.Sprintf("LOWER(BTRIM(COALESCE(NULLIF(ro.display_title, ''), %s.title, '')))", alias)
	return fmt.Sprintf(`(
		%[2]s <> ''
		AND %[2]s <> 'unknown-release'
		AND %[2]s NOT IN ('vip', 'vip only', 'private', 'private release', 'exclusive', 'unknown')
		AND %[2]s !~ '^[a-z0-9]{12,}([[:space:]]+(part|vol)[0-9]+([+._-][0-9]+)?)?$'
		AND COALESCE(%[1]s.title_source, '') <> 'source_obfuscated'
	)`, alias, effectiveTitle)
}

func probableWeakTitleClause(alias string) string {
	effectiveTitle := fmt.Sprintf("LOWER(BTRIM(COALESCE(NULLIF(ro.display_title, ''), %s.title, '')))", alias)
	return fmt.Sprintf(`NOT (
		COALESCE(%[1]s.identity_status, '') = 'probable'
		AND COALESCE(%[1]s.category_id, 8010) = 8010
		AND (
			%[2]s ~ '(^|[[:space:]])(part|vol)[0-9]+([+._-][0-9]+)?$'
			OR %[2]s ~ '^[a-z0-9]{12,}([[:space:]]+(part|vol)[0-9]+([+._-][0-9]+)?)?$'
		)
	)`, alias, effectiveTitle)
}

func opaqueTitleNeedsEvidenceClause(alias string) string {
	effectiveTitle := fmt.Sprintf("LOWER(BTRIM(COALESCE(NULLIF(ro.display_title, ''), %s.title, '')))", alias)
	return fmt.Sprintf(`NOT (
		%[2]s ~ '^[a-z0-9]{16,}([[:space:]]+(part|vol)[0-9]+([+._-][0-9]+)?)?$'
		AND COALESCE(%[1]s.deobfuscated_title, '') = ''
		AND COALESCE(%[1]s.matched_media_title, '') = ''
		AND COALESCE(%[1]s.original_media_title, '') = ''
		AND COALESCE(%[1]s.tmdb_id, 0) <= 0
		AND COALESCE(%[1]s.tvdb_id, 0) <= 0
		AND COALESCE(%[1]s.external_media_type, '') = ''
		AND COALESCE(%[1]s.runtime_seconds, 0) <= 0
		AND COALESCE(%[1]s.primary_resolution, '') = ''
		AND COALESCE(%[1]s.primary_video_codec, '') = ''
		AND COALESCE(%[1]s.primary_audio_codec, '') = ''
		AND COALESCE(%[1]s.has_nfo, FALSE) = FALSE
	)`, alias, effectiveTitle)
}

func payloadCompleteClause(alias string) string {
	return fmt.Sprintf(`(
		(
			COALESCE(%[1]s.archive_count, 0) > 0
			AND COALESCE(%[1]s.expected_archive_file_count, 0) > 0
			AND GREATEST(COALESCE(%[1]s.file_count, 0) - COALESCE(%[1]s.par_file_count, 0), 0) >= COALESCE(%[1]s.expected_archive_file_count, 0)
		)
		OR (
			COALESCE(%[1]s.archive_count, 0) <= 0
			AND (
				COALESCE(%[1]s.expected_archive_file_count, 0) <= 0
				OR GREATEST(COALESCE(%[1]s.file_count, 0) - COALESCE(%[1]s.par_file_count, 0), 0) >= COALESCE(%[1]s.expected_archive_file_count, 0)
			)
		)
	)`, alias)
}

func expectedFileCountCompleteClause(alias string) string {
	return fmt.Sprintf(`(
		COALESCE(%[1]s.expected_file_count, 0) <= 0
		OR COALESCE(%[1]s.file_count, 0) >= COALESCE(%[1]s.expected_file_count, 0)
	)`, alias)
}

func hasSFVClause(alias string) string {
	return fmt.Sprintf(`EXISTS (
		SELECT 1
		FROM release_catalog_files cf
		WHERE cf.release_id = %s.release_id
		  AND LOWER(COALESCE(cf.file_name, '')) LIKE '%%.sfv'
	)`, alias)
}
