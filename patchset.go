package git_diff_parser

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

var (
	errPatchCreate     = errors.New("patch creates are not supported")
	errPatchDelete     = errors.New("patch deletes are not supported")
	errPatchRename     = errors.New("patch renames are not supported")
	errPatchModeChange = errors.New("patch mode changes are not supported")
	errPatchBinary     = errors.New("binary patches are not supported")
)

type patchsetOperation string

const (
	patchsetOperationCreate     patchsetOperation = "create"
	patchsetOperationDelete     patchsetOperation = "delete"
	patchsetOperationRename     patchsetOperation = "rename"
	patchsetOperationCopy       patchsetOperation = "copy"
	patchsetOperationModeChange patchsetOperation = "mode change"
	patchsetOperationBinary     patchsetOperation = "binary"
)

type unsupportedPatchError struct {
	Operation patchsetOperation
	Path      string
	From      string
	To        string
}

func (e *unsupportedPatchError) Error() string {
	switch e.Operation {
	case patchsetOperationCreate:
		if e.Path != "" {
			return fmt.Sprintf("patch creates are not supported for %q", e.Path)
		}
		return "patch creates are not supported"
	case patchsetOperationDelete:
		if e.Path != "" {
			return fmt.Sprintf("patch deletes are not supported for %q", e.Path)
		}
		return "patch deletes are not supported"
	case patchsetOperationRename:
		if e.From != "" || e.To != "" {
			return fmt.Sprintf("patch renames are not supported: %q -> %q", e.From, e.To)
		}
		return "patch renames are not supported"
	case patchsetOperationModeChange:
		if e.Path != "" {
			return fmt.Sprintf("patch mode changes are not supported for %q", e.Path)
		}
		return "patch mode changes are not supported"
	case patchsetOperationBinary:
		if e.Path != "" {
			return fmt.Sprintf("binary patches are not supported for %q", e.Path)
		}
		return "binary patches are not supported"
	default:
		return "unsupported patch"
	}
}

func (e *unsupportedPatchError) Is(target error) bool {
	switch target {
	case errPatchCreate:
		return e.Operation == patchsetOperationCreate
	case errPatchDelete:
		return e.Operation == patchsetOperationDelete
	case errPatchRename:
		return e.Operation == patchsetOperationRename
	case errPatchModeChange:
		return e.Operation == patchsetOperationModeChange
	case errPatchBinary:
		return e.Operation == patchsetOperationBinary
	default:
		return false
	}
}

type patchset struct {
	Files []patchsetFile
}

type patchsetFile struct {
	Diff  fileDiff
	Patch []byte
}

func parsePatchset(patchData []byte) (patchset, []error) {
	parsed, errs := parse(string(patchData))
	if len(errs) > 0 {
		return patchset{}, errs
	}

	chunks := splitPatchsetChunks(patchData)
	if len(chunks) != len(parsed.FileDiff) {
		return patchset{}, []error{
			fmt.Errorf("parsed %d file diffs but split %d patch fragments", len(parsed.FileDiff), len(chunks)),
		}
	}

	files := make([]patchsetFile, len(chunks))
	for i := range chunks {
		files[i] = patchsetFile{
			Diff:  parsed.FileDiff[i],
			Patch: chunks[i],
		}
	}

	return patchset{Files: files}, nil
}

func (p patchset) apply(tree map[string][]byte) (map[string][]byte, error) {
	out := cloneTree(tree)
	for i := range p.Files {
		if err := applyPatchsetFile(out, &p.Files[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func applyPatchset(tree map[string][]byte, patchData []byte) (map[string][]byte, error) {
	patchset, errs := parsePatchset(patchData)
	if len(errs) > 0 {
		return nil, fmt.Errorf("unsupported patch syntax: %w", errs[0])
	}
	return patchset.apply(tree)
}

func cloneTree(tree map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(tree))
	for path, content := range tree {
		out[path] = append([]byte(nil), content...)
	}
	return out
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
