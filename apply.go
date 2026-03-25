package git_diff_parser

import (
	"bytes"
	"errors"
	"strings"
)

type patchHunk struct {
	header   string
	oldStart int
	oldCount int
	newStart int
	newCount int
	lines    []patchLine
}

type patchLine struct {
	kind       byte
	text       string
	hasNewline bool
	oldEOF     bool
	newEOF     bool
}

type fileLine struct {
	text       string
	hasNewline bool
	eofMarker  bool
}

func ApplyFile(pristine, patchData []byte) ([]byte, error) {
	result, err := ApplyFileWithOptions(pristine, patchData, DefaultApplyOptions())
	return result.Content, err
}

func ApplyFileWithOptions(pristine, patchData []byte, options ApplyOptions) (ApplyResult, error) {
	return NewPatchApply(options).applyFileWithResult(pristine, patchData)
}

func (p *PatchApply) ApplyFile(pristine, patchData []byte) ([]byte, error) {
	result, err := p.applyFileWithResult(pristine, patchData)
	return result.Content, err
}

func (p *PatchApply) applyFileWithResult(pristine, patchData []byte) (ApplyResult, error) {
	patch, err := p.validateAndParsePatch(patchData)
	if err != nil {
		return ApplyResult{}, err
	}
	return p.applyValidatedPatch(pristine, patch)
}

// ApplyPatch is kept as a compatibility alias.
func ApplyPatch(pristine, patchData []byte) ([]byte, error) {
	return ApplyFile(pristine, patchData)
}

func (p *PatchApply) applyValidatedPatch(pristine []byte, patch validatedPatch) (ApplyResult, error) {
	outcome, err := p.newApplySession(pristine).apply(patch)
	if err != nil {
		return ApplyResult{}, err
	}

	result := renderApplyResult(pristine, outcome, p.options)
	if len(outcome.conflicts) == 0 {
		return result, nil
	}

	if p.options.Mode == ApplyModeMerge {
		return result, &ApplyError{
			MergeConflicts:   len(outcome.conflicts),
			ConflictingHunks: len(outcome.conflicts),
		}
	}

	return result, &ApplyError{DirectMisses: len(outcome.conflicts)}
}

func validateApplyFileDiff(fileDiff FileDiff) error {
	switch {
	case fileDiff.IsBinary:
		return errors.New("binary patches are not supported")
	case fileDiff.NewMode != "":
		return errors.New("file mode changes are not supported")
	case fileDiff.Type == FileDiffTypeAdded || fileDiff.Type == FileDiffTypeDeleted:
		return errors.New("patches may only modify existing files")
	case len(fileDiff.Hunks) == 0:
		return errors.New("patch contains no hunks")
	case fileDiff.RenameFrom != "" || fileDiff.RenameTo != "" || fileDiff.CopyFrom != "" || fileDiff.CopyTo != "":
		return errors.New("unsupported patch syntax: copy and rename headers are not supported")
	case !fileDiffHasChanges(fileDiff):
		return errors.New("patch contains no effective changes")
	default:
		return nil
	}
}

func fileDiffHasChanges(fileDiff FileDiff) bool {
	for _, hunk := range fileDiff.Hunks {
		for _, change := range hunk.ChangeList {
			if change.Type != ContentChangeTypeNOOP {
				return true
			}
		}
	}
	return false
}

func desiredLines(hunk patchHunk) []fileLine {
	lines := make([]fileLine, 0, len(hunk.lines))
	for _, line := range hunk.lines {
		if line.kind == ' ' || line.kind == '+' {
			lines = append(lines, fileLine{text: line.text, hasNewline: line.hasNewline, eofMarker: line.newEOF})
		}
	}
	return lines
}

func preimageLines(hunk patchHunk) []fileLine {
	lines := make([]fileLine, 0, len(hunk.lines))
	for _, line := range hunk.lines {
		if line.kind == ' ' || line.kind == '-' {
			lines = append(lines, fileLine{text: line.text, hasNewline: line.hasNewline, eofMarker: line.oldEOF})
		}
	}
	return lines
}

func matchFragment(source []fileLine, start int, fragment []fileLine, ignoreWhitespace bool) bool {
	if len(fragment) == 0 {
		return true
	}
	if start < 0 || start+len(fragment) > len(source) {
		return false
	}

	for i := range fragment {
		if !lineMatches(source[start+i], fragment[i], ignoreWhitespace) {
			return false
		}
	}

	return true
}

func lineMatches(left, right fileLine, ignoreWhitespace bool) bool {
	if left.hasNewline != right.hasNewline || left.eofMarker != right.eofMarker {
		return false
	}
	if left.text == right.text {
		return true
	}
	if !ignoreWhitespace {
		return false
	}
	return normalizeWhitespace(left.text) == normalizeWhitespace(right.text)
}

func normalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func appendSourceLines(dst []fileLine, src ...fileLine) []fileLine {
	return append(dst, src...)
}

func ensureTrailingNewline(lines []fileLine) []fileLine {
	if len(lines) == 0 {
		return lines
	}
	lines[len(lines)-1].hasNewline = true
	return lines
}

func markEOFMarkers(lines []patchLine, oldCount, newCount int) {
	oldSeen := 0
	newSeen := 0

	for i := range lines {
		line := lines[i]
		if line.kind == ' ' || line.kind == '-' {
			oldSeen++
		}
		if line.kind == ' ' || line.kind == '+' {
			newSeen++
		}
		if !isEOFMarkerCandidate(line) {
			continue
		}

		lines[i].oldEOF = (line.kind == ' ' || line.kind == '-') && oldSeen == oldCount
		lines[i].newEOF = (line.kind == ' ' || line.kind == '+') && newSeen == newCount
	}
}

func splitFileLines(content []byte) []fileLine {
	rawLines := splitLinesPreserveNewline(string(content))
	lines := make([]fileLine, 0, len(rawLines))
	for _, raw := range rawLines {
		lines = append(lines, fileLine{
			text:       trimSingleLineEnding(raw),
			hasNewline: strings.HasSuffix(raw, "\n"),
		})
	}
	if len(content) > 0 && content[len(content)-1] == '\n' {
		lines = append(lines, fileLine{text: "", hasNewline: true, eofMarker: true})
	}
	return lines
}

func joinFileLines(lines []fileLine) []byte {
	var buf bytes.Buffer
	for _, line := range lines {
		if line.eofMarker {
			continue
		}
		buf.WriteString(line.text)
		if line.hasNewline {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes()
}

func trimSingleLineEnding(s string) string {
	s = strings.TrimSuffix(s, "\n")
	return s
}

func isEOFMarkerCandidate(line patchLine) bool {
	if !line.hasNewline {
		return false
	}
	return strings.TrimSuffix(line.text, "\r") == ""
}

func splitLinesPreserveNewline(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func normalizePatchForValidation(patchData []byte) []byte {
	trimmed := bytes.TrimSpace(patchData)
	if bytes.HasPrefix(trimmed, []byte("diff --git ")) {
		return patchData
	}
	return []byte("diff --git a/__patch__ b/__patch__\n" + string(patchData))
}
