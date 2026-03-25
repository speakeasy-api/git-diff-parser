package git_diff_parser

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrPatchCreate     = errors.New("patch creates are not supported")
	ErrPatchDelete     = errors.New("patch deletes are not supported")
	ErrPatchRename     = errors.New("patch renames are not supported")
	ErrPatchModeChange = errors.New("patch mode changes are not supported")
	ErrPatchBinary     = errors.New("binary patches are not supported")
)

type PatchsetOperation string

const (
	PatchsetOperationCreate     PatchsetOperation = "create"
	PatchsetOperationDelete     PatchsetOperation = "delete"
	PatchsetOperationRename     PatchsetOperation = "rename"
	PatchsetOperationModeChange PatchsetOperation = "mode change"
	PatchsetOperationBinary     PatchsetOperation = "binary"
)

type UnsupportedPatchError struct {
	Operation PatchsetOperation
	Path      string
	From      string
	To        string
}

func (e *UnsupportedPatchError) Error() string {
	switch e.Operation {
	case PatchsetOperationCreate:
		if e.Path != "" {
			return fmt.Sprintf("patch creates are not supported for %q", e.Path)
		}
		return "patch creates are not supported"
	case PatchsetOperationDelete:
		if e.Path != "" {
			return fmt.Sprintf("patch deletes are not supported for %q", e.Path)
		}
		return "patch deletes are not supported"
	case PatchsetOperationRename:
		if e.From != "" || e.To != "" {
			return fmt.Sprintf("patch renames are not supported: %q -> %q", e.From, e.To)
		}
		return "patch renames are not supported"
	case PatchsetOperationModeChange:
		if e.Path != "" {
			return fmt.Sprintf("patch mode changes are not supported for %q", e.Path)
		}
		return "patch mode changes are not supported"
	case PatchsetOperationBinary:
		if e.Path != "" {
			return fmt.Sprintf("binary patches are not supported for %q", e.Path)
		}
		return "binary patches are not supported"
	default:
		return "unsupported patch"
	}
}

func (e *UnsupportedPatchError) Is(target error) bool {
	switch target {
	case ErrPatchCreate:
		return e.Operation == PatchsetOperationCreate
	case ErrPatchDelete:
		return e.Operation == PatchsetOperationDelete
	case ErrPatchRename:
		return e.Operation == PatchsetOperationRename
	case ErrPatchModeChange:
		return e.Operation == PatchsetOperationModeChange
	case ErrPatchBinary:
		return e.Operation == PatchsetOperationBinary
	default:
		return false
	}
}

type Patchset struct {
	Files []PatchsetFile
}

type PatchsetFile struct {
	Diff  FileDiff
	Patch []byte
}

func ParsePatchset(patchData []byte) (Patchset, []error) {
	parsed, errs := Parse(string(patchData))
	if len(errs) > 0 {
		return Patchset{}, errs
	}

	chunks := splitPatchsetChunks(patchData)
	if len(chunks) != len(parsed.FileDiff) {
		return Patchset{}, []error{
			fmt.Errorf("parsed %d file diffs but split %d patch fragments", len(parsed.FileDiff), len(chunks)),
		}
	}

	files := make([]PatchsetFile, len(chunks))
	for i := range chunks {
		files[i] = PatchsetFile{
			Diff:  parsed.FileDiff[i],
			Patch: chunks[i],
		}
	}

	return Patchset{Files: files}, nil
}

func (p Patchset) Apply(tree map[string][]byte) (map[string][]byte, error) {
	out := cloneTree(tree)
	for _, file := range p.Files {
		if err := validatePatchsetFile(tree, file.Diff); err != nil {
			return nil, err
		}

		current := out[file.Diff.ToFile]
		applied, err := ApplyFile(current, file.Patch)
		if err != nil {
			return nil, err
		}
		out[file.Diff.ToFile] = append([]byte(nil), applied...)
	}
	return out, nil
}

func ApplyPatchset(tree map[string][]byte, patchData []byte) (map[string][]byte, error) {
	patchset, errs := ParsePatchset(patchData)
	if len(errs) > 0 {
		return nil, fmt.Errorf("unsupported patch syntax: %w", errs[0])
	}
	return patchset.Apply(tree)
}

func cloneTree(tree map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(tree))
	for path, content := range tree {
		out[path] = append([]byte(nil), content...)
	}
	return out
}

func validatePatchsetFile(tree map[string][]byte, fileDiff FileDiff) error {
	switch {
	case fileDiff.IsBinary:
		return &UnsupportedPatchError{
			Operation: PatchsetOperationBinary,
			Path:      fileDiff.ToFile,
		}
	case fileDiff.FromFile != fileDiff.ToFile:
		return &UnsupportedPatchError{
			Operation: PatchsetOperationRename,
			From:      fileDiff.FromFile,
			To:        fileDiff.ToFile,
		}
	}

	_, exists := tree[fileDiff.ToFile]

	switch fileDiff.Type {
	case FileDiffTypeAdded:
		return &UnsupportedPatchError{
			Operation: PatchsetOperationCreate,
			Path:      fileDiff.ToFile,
		}
	case FileDiffTypeDeleted:
		return &UnsupportedPatchError{
			Operation: PatchsetOperationDelete,
			Path:      fileDiff.FromFile,
		}
	}

	if fileDiff.NewMode != "" {
		if exists {
			return &UnsupportedPatchError{
				Operation: PatchsetOperationModeChange,
				Path:      fileDiff.ToFile,
			}
		}
		return &UnsupportedPatchError{
			Operation: PatchsetOperationCreate,
			Path:      fileDiff.ToFile,
		}
	}

	if len(fileDiff.Hunks) == 0 {
		if !exists {
			return &UnsupportedPatchError{
				Operation: PatchsetOperationCreate,
				Path:      fileDiff.ToFile,
			}
		}
		return fmt.Errorf("patch for %q contains no hunks", fileDiff.ToFile)
	}

	if !exists {
		return &UnsupportedPatchError{
			Operation: PatchsetOperationCreate,
			Path:      fileDiff.ToFile,
		}
	}

	return nil
}

func splitPatchsetChunks(patchData []byte) [][]byte {
	lines := splitLinesPreserveNewline(string(patchData))
	if len(lines) == 0 {
		return nil
	}

	chunks := make([][]byte, 0)
	var buf bytes.Buffer
	started := false

	flush := func() {
		if !started || buf.Len() == 0 {
			return
		}
		chunks = append(chunks, append([]byte(nil), buf.Bytes()...))
		buf.Reset()
	}

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimRight(line, "\n"), "diff --git ") {
			flush()
			started = true
		}
		if started {
			buf.WriteString(line)
		}
	}

	flush()
	return chunks
}
