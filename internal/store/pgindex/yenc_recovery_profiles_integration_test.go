package pgindex

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestYEncRecoveryProfilesLimitAdmissionAndSelection(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	groupName := fmt.Sprintf("alt.test.yenc-profiles.%d", now.UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	headers := []ArticleHeader{
		{
			ArticleNumber: 1,
			MessageID:     "<profile-priority-0@test>",
			Subject:       `"profile-priority-0.bin" yEnc (1/2)`,
			Poster:        "profile@test",
			DateUTC:       &now,
			Bytes:         100,
			Lines:         10,
		},
		{
			ArticleNumber: 2,
			MessageID:     "<profile-priority-1@test>",
			Subject:       `"profile-priority-1.bin" yEnc (1/2)`,
			Poster:        "profile@test",
			DateUTC:       &now,
			Bytes:         100,
			Lines:         10,
		},
	}
	if _, err := store.InsertArticleHeaders(ctx, testProviderID, newsgroupID, headers); err != nil {
		t.Fatalf("insert article headers: %v", err)
	}

	type sourceArticle struct {
		id             int64
		sourcePostedAt time.Time
		messageID      string
	}
	articles := make([]sourceArticle, 0, len(headers))
	rows, err := store.DB().QueryContext(ctx, `
		SELECT id, source_posted_at, message_id
		FROM article_headers
		WHERE provider_id = $1
		  AND newsgroup_id = $2
		ORDER BY article_number`,
		testProviderID,
		newsgroupID,
	)
	if err != nil {
		t.Fatalf("load article headers: %v", err)
	}
	for rows.Next() {
		var article sourceArticle
		if err := rows.Scan(&article.id, &article.sourcePostedAt, &article.messageID); err != nil {
			rows.Close()
			t.Fatalf("scan article header: %v", err)
		}
		articles = append(articles, article)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close article rows: %v", err)
	}
	if len(articles) != 2 {
		t.Fatalf("loaded %d article headers, want 2", len(articles))
	}

	for priorityRank, article := range articles {
		binaryID, err := upsertTestBinary(t, store, ctx, BinaryRecord{
			ProviderID:       testProviderID,
			NewsgroupID:      newsgroupID,
			ReleaseFamilyKey: fmt.Sprintf("profile-family-%d", priorityRank),
			FileFamilyKey:    fmt.Sprintf("profile-family-%d::file", priorityRank),
			FamilyKind:       "opaque_set",
			ReleaseKey:       fmt.Sprintf("profile-family-%d", priorityRank),
			ReleaseName:      fmt.Sprintf("Profile family %d", priorityRank),
			BinaryKey:        fmt.Sprintf("profile-binary-%d", priorityRank),
			BinaryName:       fmt.Sprintf("profile-priority-%d.bin", priorityRank),
			FileName:         fmt.Sprintf("profile-priority-%d.bin", priorityRank),
			TotalParts:       2,
			PostedAt:         &article.sourcePostedAt,
		})
		if err != nil {
			t.Fatalf("upsert priority-%d binary: %v", priorityRank, err)
		}
		if _, err := store.DB().ExecContext(ctx, `
			INSERT INTO yenc_recovery_work_items (
				source_posted_at, binary_id, article_header_id, provider_id,
				newsgroup_id, newsgroup_name, message_id, article_number,
				subject, poster, date_utc, priority_rank, status, ready_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'profile@test', $1, $10, 'ready', NOW())`,
			article.sourcePostedAt,
			binaryID,
			article.id,
			testProviderID,
			newsgroupID,
			groupName,
			article.messageID,
			priorityRank+1,
			fmt.Sprintf("profile priority %d", priorityRank),
			priorityRank,
		); err != nil {
			t.Fatalf("insert priority-%d recovery work: %v", priorityRank, err)
		}
	}

	for _, tt := range []struct {
		profile   string
		wantReady int64
	}{
		{profile: "balanced", wantReady: 1},
		{profile: "exhaustive", wantReady: 2},
		{profile: "header_only", wantReady: 0},
	} {
		if err := store.ConfigureYEncRecoveryAdmission(ctx, YEncRecoveryAdmissionConfig{
			RecoveryProfile: tt.profile,
		}); err != nil {
			t.Fatalf("configure %s admission: %v", tt.profile, err)
		}
		snapshot, err := store.RefreshYEncRecoveryAdmissionSnapshot(ctx)
		if err != nil {
			t.Fatalf("refresh %s admission: %v", tt.profile, err)
		}
		if snapshot.RecoveryProfile != tt.profile || snapshot.OpenReady != tt.wantReady {
			t.Fatalf("%s snapshot = %+v, want profile=%s open_ready=%d", tt.profile, snapshot, tt.profile, tt.wantReady)
		}
	}

	candidates, err := store.ListYEncRecoveryCandidatesWithOptions(ctx, 2, YEncRecoverySelectionOptions{
		Priority0Only:      true,
		DisableGenericSeed: true,
	})
	if err != nil {
		t.Fatalf("list balanced candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].PriorityRank != 0 {
		t.Fatalf("balanced candidates = %+v, want only priority 0", candidates)
	}

	candidates, err = store.ListYEncRecoveryCandidatesWithOptions(ctx, 2, YEncRecoverySelectionOptions{})
	if err != nil {
		t.Fatalf("list exhaustive candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].PriorityRank != 1 {
		t.Fatalf("exhaustive candidates = %+v, want remaining priority 1", candidates)
	}
}
