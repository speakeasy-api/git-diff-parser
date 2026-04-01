package git_diff_parser

import (
	"fmt"
	"strings"
)

type contentChangeType string

const (
	contentChangeTypeAdd    contentChangeType = "add"
	contentChangeTypeDelete contentChangeType = "delete"
	contentChangeTypeModify contentChangeType = "modify"
	contentChangeTypeNOOP   contentChangeType = ""
)

// contentChange is a part of the line that starts with ` `, `-`, `+`.
// Consecutive contentChange build a line.
// A `~` is a special case of contentChange that is used to indicate a new line.
type contentChange struct {
	Type contentChangeType `json:"type"`
	From string            `json:"from"`
	To   string            `json:"to"`
}

type changeList []contentChange

// hunkLine keeps a normalized, apply-friendly view of a hunk line.
type hunkLine struct {
	Kind       byte   `json:"kind"`
	Text       string `json:"text"`
	HasNewline bool   `json:"has_newline"`
	OldEOF     bool   `json:"old_eof,omitempty"`
	NewEOF     bool   `json:"new_eof,omitempty"`
}

// hunk is a line that starts with @@.
// Each hunk shows one area where the files differ.
// Unified format hunks look like this:
// @@ from-file-line-numbers to-file-line-numbers @@
//
//	line-from-either-file
//	line-from-either-file…
//
// If a hunk contains just one line, only its start line number appears. Otherwise its line numbers look like 'start,count'. An empty hunk is considered to start at the line that follows the hunk.
type hunk struct {
	ChangeList         changeList `json:"change_list"`
	Lines              []hunkLine `json:"lines,omitempty"`
	StartLineNumberOld int        `json:"start_line_number_old"`
	CountOld           int        `json:"count_old"`
	StartLineNumberNew int        `json:"start_line_number_new"`
	CountNew           int        `json:"count_new"`
}

func (l *hunkLine) markNoNewline() {
	l.HasNewline = false
}

func (h *hunk) markEOFMarkers() {
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

func (changes *changeList) isSignificant() bool {
	for _, change := range *changes {
		if change.Type != contentChangeTypeNOOP {
			return true
		}
	}
	return false
}

func (h *hunk) GoString() string {
	return fmt.Sprintf(
		"git_diff_parser.Hunk{ChangeList:%#v, StartLineNumberOld:%d, CountOld:%d, StartLineNumberNew:%d, CountNew:%d}",
		h.ChangeList,
		h.StartLineNumberOld,
		h.CountOld,
		h.StartLineNumberNew,
		h.CountNew,
	)
}

type fileDiffType string

const (
	fileDiffTypeAdded    fileDiffType = "add"
	fileDiffTypeDeleted  fileDiffType = "delete"
	fileDiffTypeModified fileDiffType = "modify"
)

type binaryDeltaType string

const (
	binaryDeltaTypeLiteral binaryDeltaType = "literal"
	binaryDeltaTypeDelta   binaryDeltaType = "delta"
)

type binaryPatch struct {
	Type    binaryDeltaType `json:"type"`
	Count   int
	Content string
}

// fileDiff Source of truth: https://github.com/git/git/blob/master/diffcore.h#L106
// Implemented in https://github.com/git/git/blob/master/diff.c#L3496
type fileDiff struct {
	FromFile           string        `json:"from_file"`
	ToFile             string        `json:"to_file"`
	Type               fileDiffType  `json:"type"`
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
	Hunks              []hunk        `json:"hunks"`
	BinaryPatch        []binaryPatch `json:"binary_patch"`
}

func (fd *fileDiff) GoString() string {
	var hunksStr string
	if fd.Hunks == nil {
		hunksStr = "[]git_diff_parser.Hunk(nil)"
	} else {
		hunks := make([]string, len(fd.Hunks))
		for i := range fd.Hunks {
			hunks[i] = fd.Hunks[i].GoString()
		}
		hunksStr = "[]git_diff_parser.Hunk{" + strings.Join(hunks, ", ") + "}"
	}
	return fmt.Sprintf(
		"&git_diff_parser.FileDiff{FromFile:%#v, ToFile:%#v, Type:%#v, IsBinary:%t, NewMode:%#v, Hunks:%s, BinaryPatch:%#v}",
		fd.FromFile,
		fd.ToFile,
		fd.Type,
		fd.IsBinary,
		fd.NewMode,
		hunksStr,
		fd.BinaryPatch,
	)
}

type diff struct {
	FileDiff []fileDiff `json:"file_diff"`
}
