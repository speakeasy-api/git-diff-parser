package git_diff_parser

import (
	"errors"
	"fmt"
)

var ErrPatchConflict = errors.New("patch conflict")

// ApplyResult captures the patched content and the type of misses encountered
// while attempting to apply it.
type ApplyResult struct {
	Content        []byte
	DirectMisses   int
	MergeConflicts int
}

// ApplyError reports the aggregate apply outcome.
type ApplyError struct {
	DirectMisses   int
	MergeConflicts int
	// ConflictingHunks keeps the legacy count available for callers that still
	// reason about conflict hunks rather than the new miss/conflict split.
	ConflictingHunks int
}

func (e *ApplyError) Error() string {
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

func (e *ApplyError) Is(target error) bool {
	return target == ErrPatchConflict
}

// ConflictError is kept as a compatibility alias for the old public type.
type ConflictError = ApplyError
