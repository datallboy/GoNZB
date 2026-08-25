package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/uploader"
)

type Request struct {
	JobID         string
	NZBPath       string
	ReleaseName   string
	TorrentHash   string
	SourceTracker string
	SourceSize    int64
	PostedAt      time.Time
	WorkerNodeID  string
	Password      string
	PestoVersion  string
	Obfuscated    bool
	Encrypted     bool
	HasPAR2       bool
}

type Result struct {
	SubmissionID string
	ReleaseID    string
	Created      bool
}

type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

func New(rawURL, token string, timeout time.Duration) (*Client, error) {
	base, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("invalid GoNZB URL %q", rawURL)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, errors.New("GoNZB URL must use http or https")
	}
	return &Client{
		endpoint: strings.TrimRight(base.String(), "/") + "/api/v1/uploader/submissions",
		token:    strings.TrimSpace(token), http: &http.Client{Timeout: timeout},
	}, nil
}

func (c *Client) Submit(ctx context.Context, input Request) (*Result, error) {
	nzb, err := os.Open(input.NZBPath)
	if err != nil {
		return nil, fmt.Errorf("open generated NZB: %w", err)
	}
	defer nzb.Close()

	metadata := uploader.Metadata{
		Title: input.ReleaseName, PostedAt: input.PostedAt.UTC().Format(time.RFC3339), Password: input.Password,
	}
	metadata.Flags.ObfuscatedSubjects = input.Obfuscated
	metadata.Flags.EncryptedNames = input.Encrypted
	metadata.Flags.HasPAR2 = input.HasPAR2
	metadata.Provenance.Tool = "gonzb-worker/pesto"
	metadata.Provenance.Version = input.PestoVersion
	metadata.Provenance.ExternalID = input.TorrentHash
	metadata.Artifacts = []uploader.ArtifactDescriptor{{
		Filename: "gonzb-worker.json", Kind: uploader.ArtifactMetadata, Label: "GoNZB worker provenance",
	}}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode GoNZB submission metadata: %w", err)
	}
	provenanceBytes, err := json.Marshal(map[string]any{
		"job_id": input.JobID, "torrent_hash": input.TorrentHash,
		"source_tracker": input.SourceTracker, "original_size_bytes": input.SourceSize,
		"posted_at": input.PostedAt.UTC().Format(time.RFC3339), "worker_node_id": input.WorkerNodeID,
	})
	if err != nil {
		return nil, fmt.Errorf("encode worker provenance: %w", err)
	}

	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	writeDone := make(chan error, 1)
	go func() {
		defer close(writeDone)
		if err := writeMultipart(multipartWriter, nzb, filepath.Base(input.NZBPath), metadataBytes, provenanceBytes); err != nil {
			_ = writer.CloseWithError(err)
			writeDone <- err
			return
		}
		if err := multipartWriter.Close(); err != nil {
			_ = writer.CloseWithError(err)
			writeDone <- err
			return
		}
		writeDone <- writer.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, reader)
	if err != nil {
		_ = reader.Close()
		return nil, fmt.Errorf("build GoNZB submission request: %w", err)
	}
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Idempotency-Key", input.JobID)
	resp, requestErr := c.http.Do(req)
	if requestErr != nil {
		_ = reader.CloseWithError(requestErr)
		<-writeDone
		return nil, fmt.Errorf("submit NZB to GoNZB: %w", requestErr)
	}
	writerErr := <-writeDone
	defer resp.Body.Close()
	if writerErr != nil {
		return nil, fmt.Errorf("stream GoNZB submission: %w", writerErr)
	}
	body := io.LimitReader(resp.Body, 1<<20)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, body)
		return nil, fmt.Errorf("GoNZB submission returned HTTP %d", resp.StatusCode)
	}
	var response struct {
		Created    bool `json:"created"`
		Submission struct {
			ID        string `json:"id"`
			ReleaseID string `json:"release_id"`
		} `json:"submission"`
	}
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode GoNZB submission response: %w", err)
	}
	if response.Submission.ID == "" || response.Submission.ReleaseID == "" {
		return nil, errors.New("GoNZB submission response omitted identifiers")
	}
	return &Result{SubmissionID: response.Submission.ID, ReleaseID: response.Submission.ReleaseID, Created: response.Created}, nil
}

func writeMultipart(writer *multipart.Writer, nzb io.Reader, filename string, metadata, provenance []byte) error {
	part, err := writer.CreateFormFile("nzb", filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, nzb); err != nil {
		return err
	}
	part, err = writer.CreateFormField("metadata")
	if err != nil {
		return err
	}
	if _, err := part.Write(metadata); err != nil {
		return err
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="artifact"; filename="gonzb-worker.json"`)
	header.Set("Content-Type", "application/json")
	part, err = writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(provenance)
	return err
}
