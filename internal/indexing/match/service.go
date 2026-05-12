package match

import "time"

type Candidate struct {
	ArticleNumber int64
	MessageID     string
	Subject       string
	Poster        string
	PostedAt      *time.Time
	Bytes         int64
	Lines         int
	Xref          string
	RawOverview   map[string]any
}

type Result struct {
	SourceReleaseKey  string
	ReleaseFamilyKey  string
	FileSetKey        string
	FileFamilyKey     string
	IdentityStrength  string
	IdentityReason    string
	SubjectSetToken   string
	SubjectSetKind    string
	FamilyKind        string
	BaseStem          string
	IsAuxiliary       bool
	IsMainPayload     bool
	ReleaseName       string
	ReleaseKey        string
	BinaryName        string
	BinaryKey         string
	FileName          string
	FileIndex         int
	ExpectedFileCount int
	PartNumber        int
	TotalParts        int
	IsPars            bool
	MatchConfidence   float64
	MatchStatus       string
	GroupingEvidence  map[string]any
}

type Options struct {
	HighConfidenceThreshold     float64
	ProbableConfidenceThreshold float64
	ArticleBucketSize           int64
}

type Service struct {
	opts    Options
	modules []matcherModule
}

type matcherModule struct {
	name string
	run  func(*matchState)
}

func NewService(opts ...Options) *Service {
	cfg := Options{
		HighConfidenceThreshold:     0.85,
		ProbableConfidenceThreshold: 0.55,
		ArticleBucketSize:           5000,
	}
	if len(opts) > 0 {
		if opts[0].HighConfidenceThreshold > 0 {
			cfg.HighConfidenceThreshold = opts[0].HighConfidenceThreshold
		}
		if opts[0].ProbableConfidenceThreshold > 0 {
			cfg.ProbableConfidenceThreshold = opts[0].ProbableConfidenceThreshold
		}
		if opts[0].ArticleBucketSize > 0 {
			cfg.ArticleBucketSize = opts[0].ArticleBucketSize
		}
	}
	if cfg.ProbableConfidenceThreshold > cfg.HighConfidenceThreshold {
		cfg.ProbableConfidenceThreshold = cfg.HighConfidenceThreshold
	}

	return &Service{
		opts: cfg,
		modules: []matcherModule{
			{name: "normalized_subject", run: runNormalizedSubjectModule},
			{name: "quoted_filename", run: runQuotedFilenameModule},
			{name: "yenc_markers", run: runYEncModule},
			{name: "structured_markers", run: runStructuredModule},
			{name: "poster", run: runPosterModule},
			{name: "posting_window", run: runPostingWindowModule},
			{name: "article_proximity", run: runArticleProximityModule},
			{name: "xref_overlap", run: runXrefModule},
			{name: "message_host", run: runMessageHostModule},
			{name: "extension_hints", run: runExtensionModule},
		},
	}
}

func (s *Service) Match(candidate Candidate) Result {
	state := newMatchState(candidate, s.opts)

	for _, module := range s.modules {
		module.run(state)
		if state.confidence >= s.opts.HighConfidenceThreshold && state.hasStableIdentity() {
			state.shortCircuitedAfter = module.name
			break
		}
	}

	return state.finalize(s.opts)
}

func (s *Service) MatchSubject(subject, messageID string) Result {
	return s.Match(Candidate{
		Subject:   subject,
		MessageID: messageID,
	})
}
