package cmd

import (
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
)

// nextIndex floors at 1: index 0 is reserved for the implicit main
// checkout (mainWorktreeIndex), so managed worktrees never take the
// main slot even when state is empty.
func TestNextIndex_FloorsAtOneWhenEmpty(t *testing.T) {
	if got := nextIndex(nil); got != 1 {
		t.Errorf("nextIndex(nil) = %d, want 1", got)
	}
}

func TestNextIndex_SkipsReservedMainIndex(t *testing.T) {
	// Legacy state written before the main-at-base fix may still hold
	// a managed index 0 allocation: growth must step over it, never
	// renumber it (the main-port collision guard reports the overlap).
	recs := []ports.WorktreeRecord{{Branch: "legacy", Slug: "legacy", Index: 0}}
	if got := nextIndex(recs); got != 1 {
		t.Errorf("nextIndex(legacy 0) = %d, want 1", got)
	}
}

func TestNextIndex_GrowsFromMax(t *testing.T) {
	recs := []ports.WorktreeRecord{
		{Branch: "a", Slug: "a", Index: 1},
		{Branch: "b", Slug: "b", Index: 3},
		{Branch: "c", Slug: "c", Index: 2},
	}
	if got := nextIndex(recs); got != 4 {
		t.Errorf("nextIndex(max 3) = %d, want 4", got)
	}
}

func TestNextIndex_IgnoresNegativeIndexes(t *testing.T) {
	recs := []ports.WorktreeRecord{{Branch: "x", Slug: "x", Index: -5}}
	if got := nextIndex(recs); got != 1 {
		t.Errorf("nextIndex(negative) = %d, want 1", got)
	}
}
