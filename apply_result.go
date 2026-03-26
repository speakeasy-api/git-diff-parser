package git_diff_parser

import (
	"errors"
	"fmt"
)

var ErrPatchConflict = errors.New("patch conflict")

// ApplyResult captures the patched content and the type of misses encountered
// while attempting to apply it.
type applyResult struct {
	Content        []byte
	Reject         []byte
	DirectMisses   int
	MergeConflicts int
}

type applyOutcome struct {
	content    []fileLine
	conflicts  []applyConflict
	rejectHead string
}

type applyConflict struct {
	offset int
	hunk   patchHunk
	ours   []fileLine
	theirs []fileLine
}

// applyError reports the aggregate apply outcome.
type applyError struct {
	DirectMisses   int
	MergeConflicts int
	// ConflictingHunks keeps the legacy count available for callers that still
	// reason about conflict hunks rather than the new miss/conflict split.
	ConflictingHunks int
}

func (e *applyError) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.MergeConflicts > 0 || e.ConflictingHunks > 0 {
		count := e.MergeConflicts
		if count == 0 {
			count = e.ConflictingHunks
		}
		if count == 1 {
			return "patch conflict in 1 hunk"
		}
		return fmt.Sprintf("patch conflict in %d hunks", count)
	}

	if e.DirectMisses > 0 {
		if e.DirectMisses == 1 {
			return "patch miss in 1 hunk"
		}
		return fmt.Sprintf("patch miss in %d hunks", e.DirectMisses)
	}

	return "patch apply failed"
}

func (e *applyError) Is(target error) bool {
	return target == ErrPatchConflict
}
