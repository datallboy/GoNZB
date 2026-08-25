package uploader

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/datallboy/gonzb/internal/aggregator"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/sqliteuploader"
	uploaderdomain "github.com/datallboy/gonzb/internal/uploader"
)

const syntheticSourceNZB = `<?xml version="1.0"?><nzb><file poster="fixture@example.invalid" date="1700000000" subject="synthetic.bin yEnc"><groups><group>alt.test.gonzb</group></groups><segments><segment bytes="32" number="1">fixture@example.invalid</segment></segments></file></nzb>`

func TestSourceOnlyExposesApprovedSubmissionsAndReauthorizesNZB(t *testing.T) {
	root := t.TempDir()
	store, err := sqliteuploader.NewStore(filepath.Join(root, "metadata.db"), filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := uploaderdomain.NewService(store, nzb.DefaultLimits())
	metadata := uploaderdomain.Metadata{Title: "Synthetic Movie", CategoryID: 2040}
	metadata.ExternalIDs.IMDBID = "tt1234567"
	created, err := service.Submit(t.Context(), uploaderdomain.SubmitInput{
		NZBBytes: []byte(syntheticSourceNZB), OriginalFilename: "synthetic.nzb", Metadata: metadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	source := New(store)
	if items, err := source.Search(t.Context(), aggregator.SearchRequest{Query: "Synthetic"}); err != nil || len(items) != 0 {
		t.Fatalf("pending release leaked: items=%#v err=%v", items, err)
	}
	if _, err := service.Transition(t.Context(), created.Submission.ID, uploaderdomain.StateApproved, "reviewer", "approved", false); err != nil {
		t.Fatal(err)
	}
	items, err := source.Search(t.Context(), aggregator.SearchRequest{Query: "Synthetic", Categories: []int{2000}, IMDbID: "1234567"})
	if err != nil || len(items) != 1 {
		t.Fatalf("approved release search: items=%#v err=%v", items, err)
	}
	reader, err := source.GetNZB(t.Context(), items[0])
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(payload) != syntheticSourceNZB {
		t.Fatalf("unexpected NZB payload: %q err=%v", payload, err)
	}
	if _, err := service.Transition(t.Context(), created.Submission.ID, uploaderdomain.StatePendingReview, "reviewer", "recheck", false); err != nil {
		t.Fatal(err)
	}
	if err := source.AuthorizeGet(t.Context(), items[0]); err == nil {
		t.Fatal("stale release remained authorized after unapproval")
	}
}
