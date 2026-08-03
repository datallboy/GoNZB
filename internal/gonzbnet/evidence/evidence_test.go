package evidence

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

type testSigner struct {
	public  ed25519.PublicKey
	private ed25519.PrivateKey
}

func (s testSigner) NodeID(context.Context) (string, error) { return "node.test", nil }
func (s testSigner) Sign(_ context.Context, payload []byte) ([]byte, error) {
	return ed25519.Sign(s.private, payload), nil
}

func TestPortableIdentitiesAreStable(t *testing.T) {
	first, canonicalFirst, err := CanonicalYEncIdentity(" Folder\\Movie.MKV ", 100, 1234)
	if err != nil {
		t.Fatal(err)
	}
	second, canonicalSecond, err := CanonicalYEncIdentity("folder/movie.mkv", 100, 1234)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || string(canonicalFirst) != string(canonicalSecond) {
		t.Fatalf("portable identity differs: %q %q", first, second)
	}
	contentA, err := BinaryContentID([]Segment{
		{PartNumber: 2, MessageID: "<two@example>"},
		{PartNumber: 1, MessageID: "<one@example>"},
	})
	if err != nil {
		t.Fatal(err)
	}
	contentB, err := BinaryContentID([]Segment{
		{PartNumber: 1, MessageID: "<one@example>"},
		{PartNumber: 2, MessageID: "<two@example>"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if contentA != contentB {
		t.Fatalf("content identity depends on input order: %q %q", contentA, contentB)
	}
}

func TestValidateSegmentQueryRequiresAnchor(t *testing.T) {
	now := time.Now().UTC()
	query := SegmentQuery{
		SchemaVersion: SchemaVersion, Type: SegmentQueryType,
		RequestID: "request", PoolID: "pool", RequestingNodeID: "node",
		Scheme: "yenc_v1", MatchID: "match", Missing: []PartRange{{Start: 2, End: 3}},
		CreatedAt: now.Format(time.RFC3339),
	}
	if err := ValidateSegmentQuery(query, now); err == nil {
		t.Fatal("expected anchor requirement")
	}
	query.Anchors = []string{"<known@example>"}
	if err := ValidateSegmentQuery(query, now); err != nil {
		t.Fatal(err)
	}
}

func TestSignedBundleBindsPoolRequestAndRecipient(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	bundle := Bundle{
		PoolID: "pool.test", RequestID: "request.test", RequestingNodeID: "node.recipient",
		CreatedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339),
		YEncHeaders: []YEncHeader{{MessageID: "<part@example>", FileName: "file.bin", PartNumber: 1, TotalParts: 2}},
	}
	if err := SignBundle(context.Background(), testSigner{public: public, private: private}, &bundle); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBundle(bundle, public, "pool.test", "request.test", "node.recipient", now); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBundle(bundle, public, "pool.other", "request.test", "node.recipient", now); err == nil {
		t.Fatal("expected pool binding failure")
	}
	bundle.YEncHeaders[0].FileName = "tampered.bin"
	if err := VerifyBundle(bundle, public, "pool.test", "request.test", "node.recipient", now); err == nil {
		t.Fatal("expected tamper failure")
	}
}

func TestValidateYEncQueryBounds(t *testing.T) {
	now := time.Now().UTC()
	query := YEncQuery{
		SchemaVersion: SchemaVersion, Type: YEncQueryType, RequestID: "request",
		PoolID: "pool", RequestingNodeID: "node", MessageIDs: []string{"<one@example>"},
		CreatedAt: now.Format(time.RFC3339),
	}
	if err := ValidateYEncQuery(query, now); err != nil {
		t.Fatal(err)
	}
	query.MessageIDs = []string{"bad"}
	if err := ValidateYEncQuery(query, now); err == nil {
		t.Fatal("expected malformed message id rejection")
	}
}
