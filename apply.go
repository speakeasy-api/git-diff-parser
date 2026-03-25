package git_diff_parser

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

type patchHunk struct {
	oldStart int
	oldCount int
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

type anchoredFragment struct {
	offset int
	lines  []fileLine
}

var hunkHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

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
	return p.newApplySession(pristine).apply(patch)
}

// ApplyPatch is kept as a compatibility alias.
func ApplyPatch(pristine, patchData []byte) ([]byte, error) {
	return ApplyFile(pristine, patchData)
}

func validateSingleFilePatch(patchData []byte) error {
	parsed, errs := Parse(string(patchData))
	if len(errs) > 0 {
		return fmt.Errorf("unsupported patch syntax: %w", errs[0])
	}

	if len(parsed.FileDiff) != 1 {
		return fmt.Errorf("expected exactly 1 file diff, found %d", len(parsed.FileDiff))
	}

	fileDiff := parsed.FileDiff[0]
	if fileDiff.IsBinary {
		return fmt.Errorf("binary patches are not supported")
	}
	if fileDiff.NewMode != "" {
		return fmt.Errorf("file mode changes are not supported")
	}
	if fileDiff.Type == FileDiffTypeAdded || fileDiff.Type == FileDiffTypeDeleted {
		return fmt.Errorf("patches may only modify existing files")
	}
	if len(fileDiff.Hunks) == 0 {
		return fmt.Errorf("patch contains no hunks")
	}
	if fileDiff.RenameFrom != "" || fileDiff.RenameTo != "" || fileDiff.CopyFrom != "" || fileDiff.CopyTo != "" {
		return fmt.Errorf("unsupported patch syntax: copy and rename headers are not supported")
	}

	return nil
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

func hunksContainChanges(hunks []patchHunk) bool {
	for _, hunk := range hunks {
		for _, line := range hunk.lines {
			if line.kind == '+' || line.kind == '-' {
				return true
			}
		}
	}
	return false
}

func skipToHunks(lines []string) []string {
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimRight(line, "\n"), "@@ ") {
			return lines[i:]
		}
	}
	return nil
}

func parseHunks(lines []string) ([]patchHunk, error) {
	hunks := make([]patchHunk, 0)
	for i := 0; i < len(lines); {
		line := strings.TrimRight(lines[i], "\n")
		if line == "" {
			i++
			continue
		}
		if !strings.HasPrefix(line, "@@ ") {
			return nil, fmt.Errorf("unexpected patch line %q", line)
		}

		oldStart, oldCount, newCount, err := parseHunkHeader(line)
		if err != nil {
			return nil, err
		}

		i++
		hunkLines := make([]patchLine, 0)
		for i < len(lines) && !strings.HasPrefix(strings.TrimRight(lines[i], "\n"), "@@ ") {
			raw := lines[i]
			if strings.HasPrefix(raw, `\ No newline at end of file`) {
				if len(hunkLines) == 0 {
					return nil, fmt.Errorf("unexpected no-newline marker without a preceding patch line")
				}
				hunkLines[len(hunkLines)-1].hasNewline = false
				i++
				continue
			}

			line, skip, err := parsePatchLine(raw)
			if err != nil {
				return nil, err
			}
			if !skip {
				hunkLines = append(hunkLines, line)
			}
			i++
		}
		markEOFMarkers(hunkLines, oldCount, newCount)

		hunks = append(hunks, patchHunk{
			oldStart: oldStart,
			oldCount: oldCount,
			newCount: newCount,
			lines:    hunkLines,
		})
	}

	return hunks, nil
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

func matchAnchoredFragment(source []fileLine, start int, begin, end anchoredFragment) bool {
	return matchFragment(source, start+begin.offset, begin.lines) &&
		matchFragment(source, start+end.offset, end.lines)
}

func splitAnchoredFragment(lines []fileLine) (anchoredFragment, anchoredFragment) {
	if len(lines) == 0 {
		return anchoredFragment{}, anchoredFragment{}
	}

	beginLen := len(lines) / 2
	if beginLen == 0 {
		beginLen = 1
	}

	return anchoredFragment{
			offset: 0,
			lines:  lines[:beginLen],
		}, anchoredFragment{
			offset: beginLen,
			lines:  lines[beginLen:],
		}
}

func matchFragment(source []fileLine, start int, fragment []fileLine) bool {
	if len(fragment) == 0 {
		return true
	}
	if start < 0 || start+len(fragment) > len(source) {
		return false
	}

	for i := range fragment {
		if source[start+i].text != fragment[i].text ||
			source[start+i].hasNewline != fragment[i].hasNewline ||
			source[start+i].eofMarker != fragment[i].eofMarker {
			return false
		}
	}

	return true
}

func (p *PatchApply) appendConflict(out []fileLine, ours, theirs []fileLine) []fileLine {
	labels := p.options.ConflictLabels
	out = append(out, fileLine{text: "<<<<<<< " + labels.Current, hasNewline: true})
	out = appendSourceLines(out, ours...)
	out = ensureTrailingNewline(out)
	out = append(out, fileLine{text: "=======", hasNewline: true})
	out = appendSourceLines(out, theirs...)
	out = ensureTrailingNewline(out)
	out = append(out, fileLine{text: ">>>>>>> " + labels.Incoming, hasNewline: true})
	return out
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

func parseHunkHeader(header string) (int, int, int, error) {
	matches := hunkHeaderPattern.FindStringSubmatch(header)
	if len(matches) == 0 {
		return 0, 0, 0, fmt.Errorf("invalid hunk header %q", header)
	}

	oldStart, err := parseNumber(matches[1])
	if err != nil {
		return 0, 0, 0, err
	}

	oldCount := 1
	if matches[2] != "" {
		oldCount, err = parseNumber(matches[2])
		if err != nil {
			return 0, 0, 0, err
		}
	}

	newCount := 1
	if matches[4] != "" {
		newCount, err = parseNumber(matches[4])
		if err != nil {
			return 0, 0, 0, err
		}
	}

	return oldStart, oldCount, newCount, nil
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

func parsePatchLine(raw string) (patchLine, bool, error) {
	if raw == "" {
		return patchLine{}, true, nil
	}

	switch raw[0] {
	case ' ', '-', '+':
		return patchLine{
			kind:       raw[0],
			text:       trimSingleLineEnding(raw[1:]),
			hasNewline: strings.HasSuffix(raw, "\n"),
		}, false, nil
	default:
		return patchLine{}, false, fmt.Errorf("unexpected hunk line %q", strings.TrimRight(raw, "\n"))
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

func parseNumber(raw string) (int, error) {
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil {
		return 0, err
	}
	return value, nil
}
