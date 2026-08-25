package uploader

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/datallboy/gonzb/internal/nzb"
)

const SourceName = "uploader"

type State string

const (
	StatePendingReview State = "pending_review"
	StateApproved      State = "approved"
	StateRejected      State = "rejected"
)

type IntakeKind string

const (
	IntakeHTTP   IntakeKind = "http"
	IntakeInbox  IntakeKind = "inbox"
	IntakeManual IntakeKind = "manual"
)

var (
	ErrNotFound          = errors.New("uploader submission not found")
	ErrInvalidTransition = errors.New("invalid uploader state transition")
	ErrConflict          = errors.New("uploader submission conflict")
)

type Metadata struct {
	Title       string `json:"title,omitempty"`
	CategoryID  int    `json:"category_id,omitempty"`
	PostedAt    string `json:"posted_at,omitempty"`
	Password    string `json:"password,omitempty"`
	ExternalIDs struct {
		IMDBID string `json:"imdb_id,omitempty"`
		TMDBID int64  `json:"tmdb_id,omitempty"`
		TVDBID int64  `json:"tvdb_id,omitempty"`
	} `json:"external_ids,omitempty"`
	Media struct {
		Year       int    `json:"year,omitempty"`
		Resolution string `json:"resolution,omitempty"`
		Source     string `json:"source,omitempty"`
		VideoCodec string `json:"video_codec,omitempty"`
		AudioCodec string `json:"audio_codec,omitempty"`
	} `json:"media,omitempty"`
	Flags struct {
		ObfuscatedSubjects bool `json:"obfuscated_subjects,omitempty"`
		EncryptedNames     bool `json:"encrypted_names,omitempty"`
		HasPAR2            bool `json:"has_par2,omitempty"`
	} `json:"flags,omitempty"`
	Provenance struct {
		Tool       string `json:"tool,omitempty"`
		Version    string `json:"version,omitempty"`
		ExternalID string `json:"external_id,omitempty"`
	} `json:"provenance,omitempty"`
	Artifacts []ArtifactDescriptor `json:"artifacts,omitempty"`
}

type ArtifactKind string

const (
	ArtifactNFO        ArtifactKind = "nfo"
	ArtifactScreenshot ArtifactKind = "screenshot"
	ArtifactSample     ArtifactKind = "sample"
	ArtifactSubtitle   ArtifactKind = "subtitle"
	ArtifactMetadata   ArtifactKind = "metadata"
	ArtifactOther      ArtifactKind = "other"
)

type ArtifactDescriptor struct {
	Filename string       `json:"filename"`
	Kind     ArtifactKind `json:"kind"`
	Label    string       `json:"label,omitempty"`
}

type ArtifactInput struct {
	Filename          string
	DeclaredMediaType string
	Payload           []byte
}

type Artifact struct {
	ID                string       `json:"id"`
	SubmissionID      string       `json:"submission_id"`
	Kind              ArtifactKind `json:"kind"`
	OriginalFilename  string       `json:"original_filename"`
	Label             string       `json:"label,omitempty"`
	DeclaredMediaType string       `json:"declared_media_type,omitempty"`
	DetectedMediaType string       `json:"detected_media_type"`
	SizeBytes         int64        `json:"size_bytes"`
	SHA256            string       `json:"sha256"`
	DisplayOrder      int          `json:"display_order"`
	BlobKey           string       `json:"-"`
	Payload           []byte       `json:"-"`
	CreatedAt         time.Time    `json:"created_at"`
}

type Submission struct {
	ID                   string          `json:"id"`
	State                State           `json:"state"`
	ReleaseID            string          `json:"release_id"`
	Title                string          `json:"title"`
	NormalizedTitle      string          `json:"normalized_title"`
	CategoryID           int             `json:"category_id"`
	Category             string          `json:"category"`
	SizeBytes            int64           `json:"size_bytes"`
	PostedAt             time.Time       `json:"posted_at"`
	Poster               string          `json:"poster"`
	Groups               []string        `json:"groups"`
	FileCount            int             `json:"file_count"`
	SegmentCount         int             `json:"segment_count"`
	HasPassword          bool            `json:"has_password"`
	Password             string          `json:"password,omitempty"`
	HasPAR2              bool            `json:"has_par2"`
	HasNFO               bool            `json:"has_nfo"`
	ObfuscatedSubjects   bool            `json:"obfuscated_subjects"`
	EncryptedNames       bool            `json:"encrypted_names"`
	IMDBID               string          `json:"imdb_id,omitempty"`
	TMDBID               int64           `json:"tmdb_id,omitempty"`
	TVDBID               int64           `json:"tvdb_id,omitempty"`
	Year                 int             `json:"year,omitempty"`
	Resolution           string          `json:"resolution,omitempty"`
	MediaSource          string          `json:"media_source,omitempty"`
	VideoCodec           string          `json:"video_codec,omitempty"`
	AudioCodec           string          `json:"audio_codec,omitempty"`
	NZBSHA256            string          `json:"nzb_sha256"`
	NZBBlobKey           string          `json:"-"`
	IdempotencyKey       string          `json:"-"`
	IntakeKind           IntakeKind      `json:"intake_kind"`
	ProvenanceTool       string          `json:"provenance_tool,omitempty"`
	ProvenanceVersion    string          `json:"provenance_version,omitempty"`
	ProvenanceExternalID string          `json:"provenance_external_id,omitempty"`
	OriginalFilename     string          `json:"original_filename"`
	SubmittedBy          string          `json:"submitted_by"`
	Reviewer             string          `json:"reviewer,omitempty"`
	ReviewNote           string          `json:"review_note,omitempty"`
	Files                []nzb.FileFacts `json:"files,omitempty"`
	Artifacts            []Artifact      `json:"artifacts,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	ReviewedAt           *time.Time      `json:"reviewed_at,omitempty"`
	ApprovedAt           *time.Time      `json:"approved_at,omitempty"`
	RejectedAt           *time.Time      `json:"rejected_at,omitempty"`
}

type Event struct {
	ID           int64     `json:"id"`
	SubmissionID string    `json:"submission_id"`
	EventType    string    `json:"event_type"`
	Actor        string    `json:"actor"`
	PriorState   State     `json:"prior_state,omitempty"`
	NextState    State     `json:"next_state,omitempty"`
	Note         string    `json:"note,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type PublicationState string

const (
	PublicationRequested           PublicationState = "requested"
	PublicationPublished           PublicationState = "published"
	PublicationWithdrawalRequested PublicationState = "withdrawal_requested"
	PublicationWithdrawn           PublicationState = "withdrawn"
	PublicationFailed              PublicationState = "failed"
)

type FederationPublication struct {
	SubmissionID            string           `json:"submission_id"`
	PoolID                  string           `json:"pool_id"`
	State                   PublicationState `json:"state"`
	ReleaseID               string           `json:"release_id,omitempty"`
	ManifestID              string           `json:"manifest_id,omitempty"`
	CardEventID             string           `json:"card_event_id,omitempty"`
	ManifestEventID         string           `json:"manifest_event_id,omitempty"`
	PublicationStateEventID string           `json:"publication_state_event_id,omitempty"`
	AttemptCount            int              `json:"attempt_count"`
	LastError               string           `json:"last_error,omitempty"`
	NextAttemptAt           *time.Time       `json:"next_attempt_at,omitempty"`
	RequestedBy             string           `json:"requested_by,omitempty"`
	CreatedAt               time.Time        `json:"created_at"`
	UpdatedAt               time.Time        `json:"updated_at"`
}

type PublicationOutcome struct {
	ReleaseID               string
	ManifestID              string
	CardEventID             string
	ManifestEventID         string
	PublicationStateEventID string
}

type ListFilter struct {
	State        State
	Query        string
	CategoryID   int
	Limit        int
	Offset       int
	ApprovedOnly bool
}

type Update struct {
	Title       *string
	CategoryID  *int
	PostedAt    *time.Time
	Password    *string
	IMDBID      *string
	TMDBID      *int64
	TVDBID      *int64
	Year        *int
	Resolution  *string
	MediaSource *string
	VideoCodec  *string
	AudioCodec  *string
	Note        string
	Actor       string
}

type CreateResult struct {
	Submission *Submission
	Created    bool
}

type Store interface {
	CreateSubmission(ctx context.Context, submission Submission, nzbBytes []byte, artifacts []Artifact) (CreateResult, error)
	GetSubmission(ctx context.Context, id string) (*Submission, error)
	GetSubmissionByReleaseID(ctx context.Context, releaseID string) (*Submission, error)
	GetSubmissionBySHA256(ctx context.Context, sha256 string) (*Submission, error)
	ListSubmissions(ctx context.Context, filter ListFilter) ([]Submission, error)
	UpdateSubmission(ctx context.Context, id string, update Update) (*Submission, error)
	TransitionSubmission(ctx context.Context, id string, next State, actor, note string) (*Submission, error)
	ListEvents(ctx context.Context, submissionID string) ([]Event, error)
	ListFederationPublications(ctx context.Context, submissionID string) ([]FederationPublication, error)
	ListDueFederationPublications(ctx context.Context, limit int) ([]FederationPublication, error)
	RequestFederationPublication(ctx context.Context, submissionID, poolID, actor string) (*FederationPublication, error)
	RequestFederationWithdrawal(ctx context.Context, submissionID, poolID, actor, note string) (*FederationPublication, error)
	RequestSubmissionWithdrawals(ctx context.Context, submissionID, actor, note string) error
	CompleteFederationPublication(ctx context.Context, submissionID, poolID string, state PublicationState, outcome PublicationOutcome) (*FederationPublication, error)
	FailFederationPublication(ctx context.Context, submissionID, poolID string, cause error) (*FederationPublication, error)
	OpenNZB(ctx context.Context, id string) (io.ReadCloser, error)
	OpenArtifact(ctx context.Context, submissionID, artifactID string) (*Artifact, io.ReadCloser, error)
	Ping(ctx context.Context) error
	ValidateSchema(ctx context.Context) error
	Close() error
}

// CatalogProjector mirrors approved submissions into a deployment's terminal
// release catalog. The uploader store remains authoritative for review state;
// implementations must not fabricate scrape, binary, or formation records.
type CatalogProjector interface {
	PublishUploaderSubmission(ctx context.Context, submission Submission) error
	WithdrawUploaderSubmission(ctx context.Context, releaseID string) error
	ReconcileUploaderSubmissions(ctx context.Context, submissions []Submission) error
}

type SubmitInput struct {
	NZBBytes         []byte
	OriginalFilename string
	Metadata         Metadata
	IntakeKind       IntakeKind
	SubmittedBy      string
	IdempotencyKey   string
	Artifacts        []ArtifactInput
}
