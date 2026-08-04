package pgindex

import (
	"strings"
	"testing"
)

func TestPublicNumericCategoryClauseSupportsRootsAndExactIDs(t *testing.T) {
	clause, args := publicNumericCategoryClause([]int{2000, 2040, 2040, -1}, 3)
	if !strings.Contains(clause, "BETWEEN $3 AND $4") || !strings.Contains(clause, "= $5") {
		t.Fatalf("unexpected category clause: %s", clause)
	}
	if len(args) != 3 || args[0] != 2000 || args[1] != 2999 || args[2] != 2040 {
		t.Fatalf("unexpected category arguments: %v", args)
	}
}
