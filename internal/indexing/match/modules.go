package match

import "path/filepath"

func runNormalizedSubjectModule(state *matchState) {
	if state.normalizedSubject == "" {
		return
	}

	if state.releaseName == "" {
		state.releaseName = deriveReleaseName(state.cleanSubject, state.bestFileName())
	}

	score := 0.16
	if len(state.normalizedSubject) >= 12 {
		score = 0.24
	}

	state.addEvidence("normalized_subject", score, map[string]any{
		"value":        state.normalizedSubject,
		"release_name": state.releaseName,
	})
}

func runQuotedFilenameModule(state *matchState) {
	if state.quotedFilename == "" {
		return
	}

	if state.fileName == "" {
		state.fileName = state.quotedFilename
	}
	if state.releaseName == "" {
		state.releaseName = deriveReleaseName(state.cleanSubject, state.quotedFilename)
	}

	score := 0.42
	if filepath.Ext(state.quotedFilename) != "" {
		score = 0.5
	}

	state.addEvidence("quoted_filename", score, map[string]any{
		"value":     state.quotedFilename,
		"extension": filepath.Ext(state.quotedFilename),
	})
}

func runYEncModule(state *matchState) {
	hasYEnc := state.cleanSubject != state.subjectWithoutYEnc
	if !hasYEnc {
		return
	}

	state.addEvidence("yenc_markers", 0.12, map[string]any{
		"present": true,
	})
}

func runStructuredModule(state *matchState) {
	if state.structured.Name == "" && state.structured.Part == 0 && state.structured.Total == 0 && state.structured.Size == 0 {
		return
	}

	if state.fileName == "" && state.structured.Name != "" {
		state.fileName = state.structured.Name
	}
	if state.releaseName == "" {
		state.releaseName = deriveReleaseName(state.cleanSubject, state.bestFileName())
	}
	if state.structured.Part > 0 {
		state.partNumber = state.structured.Part
	}
	if state.structured.Total > state.totalParts {
		state.totalParts = state.structured.Total
	}

	score := 0.08
	if state.structured.Name != "" {
		score += 0.22
	}
	if state.structured.Size > 0 {
		score += 0.04
	}
	if state.structured.Total > 1 {
		score += 0.04
	}

	state.addEvidence("structured_markers", score, map[string]any{
		"name":  state.structured.Name,
		"part":  state.structured.Part,
		"total": state.structured.Total,
		"size":  state.structured.Size,
	})
}

func runPosterModule(state *matchState) {
	poster := normalizePoster(state.candidate.Poster)
	if poster == "" {
		return
	}

	state.addEvidence("poster", 0.05, map[string]any{
		"value": poster,
	})
}

func runPostingWindowModule(state *matchState) {
	window := derivePostingWindow(state.candidate.PostedAt)
	if window == "" {
		return
	}

	state.addEvidence("posting_window", 0.03, map[string]any{
		"value": window,
	})
}

func runArticleProximityModule(state *matchState) {
	bucket := deriveArticleBucket(state.candidate.ArticleNumber)
	if bucket == 0 {
		return
	}

	state.addEvidence("article_proximity", 0.02, map[string]any{
		"bucket": bucket,
	})
}

func runXrefModule(state *matchState) {
	groups := parseXrefGroups(state.candidate.Xref)
	if len(groups) == 0 {
		return
	}

	state.addEvidence("xref_overlap", 0.05, map[string]any{
		"groups": groups,
	})
}

func runMessageHostModule(state *matchState) {
	host := extractMessageHost(state.candidate.MessageID)
	if host == "" {
		return
	}

	state.addEvidence("message_host", 0.05, map[string]any{
		"value": host,
	})
}

func runExtensionModule(state *matchState) {
	ext := state.bestExtension()
	if ext == "" {
		return
	}

	score := 0.05
	kind := "file_extension"
	if parFileRE.MatchString(ext) {
		score = 0.12
		kind = "par2"
	}

	state.addEvidence("extension_hints", score, map[string]any{
		"kind":  kind,
		"value": ext,
	})
}
