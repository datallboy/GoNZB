package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadArticleCapturesHeadersAndYEncMetadata(t *testing.T) {
	raw := "Message-ID: <synthetic@example.invalid>\r\n" +
		"Subject: synthetic (1/2)\r\n" +
		"From: test@example.invalid\r\n" +
		"Newsgroups: alt.binaries.gonzb.synthetic\r\n\r\n" +
		"=ybegin part=1 total=2 line=128 size=10 name=Synthetic.txt\r\n" +
		"=ypart begin=1 end=5\r\n" +
		"synthetic-body\r\n" +
		"=yend size=5 part=1\r\n.\r\n"

	article, err := readArticle(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatalf("readArticle: %v", err)
	}
	if article.MessageID != "<synthetic@example.invalid>" || article.Subject != "synthetic (1/2)" {
		t.Fatalf("unexpected captured headers: %+v", article)
	}
	if article.YEncName != "Synthetic.txt" || article.YEncPart != 1 || article.YEncTotal != 2 {
		t.Fatalf("unexpected yEnc metadata: %+v", article)
	}
	if article.YEncPartBytes != 5 {
		t.Fatalf("unexpected yEnc part size: %+v", article)
	}
	if len(article.ArticleSHA) != 64 || len(article.BodySHA) != 64 || article.ArticleBytes == 0 || article.BodyBytes == 0 {
		t.Fatalf("expected bounded body hashes: %+v", article)
	}
}

func TestRecordDeduplicatesMessageIDs(t *testing.T) {
	capturePath := filepath.Join(t.TempDir(), "articles.jsonl")
	f := &fixture{capturePath: capturePath, articles: make(map[string]capturedArticle)}
	article := capturedArticle{MessageID: "<same@example.invalid>"}
	if err := f.record(article); err != nil {
		t.Fatalf("first record: %v", err)
	}
	if err := f.record(article); err != nil {
		t.Fatalf("duplicate record: %v", err)
	}
	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	if lines := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; lines != 1 {
		t.Fatalf("capture lines = %d, want 1", lines)
	}
}
