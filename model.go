package git_diff_parser

import (
	"fmt"
	"strings"
)

type ContentChangeType string

const (
	ContentChangeTypeAdd    ContentChangeType = "add"
	ContentChangeTypeDelete ContentChangeType = "delete"
	ContentChangeTypeModify ContentChangeType = "modify"
	ContentChangeTypeNOOP   ContentChangeType = ""
)

// ContentChange is a part of the line that starts with ` `, `-`, `+`.
// Consecutive ContentChange build a line.
// A `~` is a special case of ContentChange that is used to indicate a new line.
type ContentChange struct {
	Type ContentChangeType `json:"type"`
	From string            `json:"from"`
	To   string            `json:"to"`
}

type ChangeList []ContentChange

// HunkLine keeps a normalized, apply-friendly view of a hunk line.
type HunkLine struct {
	Kind       byte   `json:"kind"`
	Text       string `json:"text"`
	HasNewline bool   `json:"has_newline"`
	OldEOF     bool   `json:"old_eof,omitempty"`
	NewEOF     bool   `json:"new_eof,omitempty"`
}

func (l *HunkLine) MarkNoNewline() {
	l.HasNewline = false
}

func (h *Hunk) MarkEOFMarkers() {
	oldSeen := 0
	newSeen := 0

	for i := range h.Lines {
		line := &h.Lines[i]
		if line.Kind == ' ' || line.Kind == '-' {
			oldSeen++
		}
		if line.Kind == ' ' || line.Kind == '+' {
			newSeen++
		}
		if !line.HasNewline || strings.TrimSuffix(line.Text, "\r") != "" {
			continue
		}

		line.OldEOF = (line.Kind == ' ' || line.Kind == '-') && oldSeen == h.CountOld
		line.NewEOF = (line.Kind == ' ' || line.Kind == '+') && newSeen == h.CountNew
	}
}

// Hunk is a line that starts with @@.
// Each hunk shows one area where the files differ.
// Unified format hunks look like this:
// @@ from-file-line-numbers to-file-line-numbers @@
//
//	line-from-either-file
//	line-from-either-file…
//
// If a hunk contains just one line, only its start line number appears. Otherwise its line numbers look like ‘start,count’. An empty hunk is considered to start at the line that follows the hunk.
type Hunk struct {
	ChangeList         ChangeList `json:"change_list"`
	Lines              []HunkLine `json:"lines,omitempty"`
	StartLineNumberOld int        `json:"start_line_number_old"`
	CountOld           int        `json:"count_old"`
	StartLineNumberNew int        `json:"start_line_number_new"`
	CountNew           int        `json:"count_new"`
}

func (changes *ChangeList) IsSignificant() bool {
	for _, change := range *changes {
		if change.Type != ContentChangeTypeNOOP {
			return true
		}
	}
	return false
}

func (h Hunk) GoString() string {
	return fmt.Sprintf(
		"git_diff_parser.Hunk{ChangeList:%#v, StartLineNumberOld:%d, CountOld:%d, StartLineNumberNew:%d, CountNew:%d}",
		h.ChangeList,
		h.StartLineNumberOld,
		h.CountOld,
		h.StartLineNumberNew,
		h.CountNew,
	)
}

type FileDiffType string

const (
	FileDiffTypeAdded    FileDiffType = "add"
	FileDiffTypeDeleted  FileDiffType = "delete"
	FileDiffTypeModified FileDiffType = "modify"
)

type BinaryDeltaType string

const (
	BinaryDeltaTypeLiteral BinaryDeltaType = "literal"
	BinaryDeltaTypeDelta   BinaryDeltaType = "delta"
)

type BinaryPatch struct {
	Type    BinaryDeltaType `json:"type"`
	Count   int
	Content string
}

// FileDiff Source of truth: https://github.com/git/git/blob/master/diffcore.h#L106
// Implemented in https://github.com/git/git/blob/master/diff.c#L3496
type FileDiff struct {
	FromFile           string        `json:"from_file"`
	ToFile             string        `json:"to_file"`
	Type               FileDiffType  `json:"type"`
	IsBinary           bool          `json:"is_binary"`
	OldMode            string        `json:"old_mode,omitempty"`
	NewMode            string        `json:"new_mode,omitempty"`
	IndexOld           string        `json:"index_old,omitempty"`
	IndexNew           string        `json:"index_new,omitempty"`
	IndexMode          string        `json:"index_mode,omitempty"`
	SimilarityIndex    int           `json:"similarity_index,omitempty"`
	DissimilarityIndex int           `json:"dissimilarity_index,omitempty"`
	RenameFrom         string        `json:"rename_from,omitempty"`
	RenameTo           string        `json:"rename_to,omitempty"`
	CopyFrom           string        `json:"copy_from,omitempty"`
	CopyTo             string        `json:"copy_to,omitempty"`
	Hunks              []Hunk        `json:"hunks"`
	BinaryPatch        []BinaryPatch `json:"binary_patch"`
}

func (fd FileDiff) GoString() string {
	return fmt.Sprintf(
		"&git_diff_parser.FileDiff{FromFile:%#v, ToFile:%#v, Type:%#v, IsBinary:%t, NewMode:%#v, Hunks:%#v, BinaryPatch:%#v}",
		fd.FromFile,
		fd.ToFile,
		fd.Type,
		fd.IsBinary,
		fd.NewMode,
		fd.Hunks,
		fd.BinaryPatch,
	)
}

type Diff struct {
	FileDiff []FileDiff `json:"file_diff"`
}
