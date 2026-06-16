package pgindex

import "testing"

func TestDynamicBatchSizesStayBelowPostgresBindParameterSoftLimit(t *testing.T) {
	cases := map[string]int{
		"archive purge binary id staging":          archivePurgeBinaryIDStageChunk,
		"binary identity lock":                     binaryIdentityLockBatchSize * 3,
		"binary completion key sync":               binaryCompletionKeySyncChunkSize,
		"binary part article lookup":               binaryPartArticleLookupChunk,
		"binary part upsert":                       binaryPartUpsertBatchRecords * 7,
		"predb entry id lookup":                    predbEntryIDLookupChunk,
		"release family dirty keys":                releaseFamilyDirtyBatchSize * 4,
		"release family summary merge":             releaseFamilySummaryMergeRowsMax * 24,
		"release file stale binary id delete":      releaseFileBinaryIDDeleteChunk + 1,
		"release file insert":                      releaseFileInsertBatchSize * 10,
		"release newsgroup insert":                 releaseNewsgroupInsertBatchSize * 2,
		"release summary refresh requested values": releaseFamilySummaryRefreshQueryBatchCap * 4,
		"release title candidate lookup":           releaseTitleCandidateLookupChunk,
		"yenc recovery work item sync":             yencRecoveryWorkItemSyncChunk,
	}

	for name, bindArgs := range cases {
		if bindArgs > postgresBindParameterSoftLimit {
			t.Fatalf("%s uses %d bind args; keep dynamic batches below soft limit %d", name, bindArgs, postgresBindParameterSoftLimit)
		}
	}
}
