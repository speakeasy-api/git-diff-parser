package git_diff_parser

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var ErrPatchConflict = errors.New("patch conflict")

type ConflictLabels struct {
	Current  string
	Incoming string
}

type ApplyOptions struct {
	ConflictLabels ConflictLabels
}

// PatchApply holds apply-time configuration and mirrors Git's stateful apply design.
type PatchApply struct {
	options ApplyOptions
}

func DefaultApplyOptions() ApplyOptions {
	return ApplyOptions{
		ConflictLabels: ConflictLabels{
			Current:  "Current",
			Incoming: "Incoming patch",
		},
	}
}

func NewPatchApply(options ApplyOptions) *PatchApply {
	return &PatchApply{options: normalizeApplyOptions(options)}
}

func normalizeApplyOptions(options ApplyOptions) ApplyOptions {
	defaults := DefaultApplyOptions()
	if options.ConflictLabels.Current == "" {
		options.ConflictLabels.Current = defaults.ConflictLabels.Current
	}
	if options.ConflictLabels.Incoming == "" {
		options.ConflictLabels.Incoming = defaults.ConflictLabels.Incoming
	}
	return options
}

type ConflictError struct {
	ConflictingHunks int
}

func (e *ConflictError) Error() string {
	if e.ConflictingHunks == 1 {
		return "patch conflict in 1 hunk"
	}
	return fmt.Sprintf("patch conflict in %d hunks", e.ConflictingHunks)
}

func (e *ConflictError) Is(target error) bool {
	return target == ErrPatchConflict
}

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

var hunkHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func ApplyFile(pristine, patchData []byte) ([]byte, error) {
	return NewPatchApply(DefaultApplyOptions()).ApplyFile(pristine, patchData)
}

func (p *PatchApply) ApplyFile(pristine, patchData []byte) ([]byte, error) {
	normalizedPatch := normalizePatchForValidation(patchData)
	if err := validateSingleFilePatch(normalizedPatch); err != nil {
		return nil, err
	}

	lines := splitLinesPreserveNewline(string(normalizedPatch))
	hunks, err := parseHunks(skipToHunks(lines))
	if err != nil {
		return nil, err
	}
	if !hunksContainChanges(hunks) {
		return nil, fmt.Errorf("patch contains no effective changes")
	}

	sourceLines := splitFileLines(pristine)
	cursor := 0
	outLines := make([]fileLine, 0, len(sourceLines))
	conflicts := 0

	for _, hunk := range hunks {
		matchIndex, matched := locateHunk(sourceLines, cursor, hunk)
		if !matched {
			conflicts++

			conflictStart := hunk.oldStart - 1
			if conflictStart < cursor {
				conflictStart = cursor
			}
			if conflictStart > len(sourceLines) {
				conflictStart = len(sourceLines)
			}

			conflictEnd := conflictStart + hunk.oldCount
			if conflictEnd > len(sourceLines) {
				conflictEnd = len(sourceLines)
			}

			outLines = appendSourceLines(outLines, sourceLines[cursor:conflictStart]...)
			outLines = p.appendConflict(outLines, sourceLines[conflictStart:conflictEnd], desiredLines(hunk))
			cursor = conflictEnd
			continue
		}

		outLines = appendSourceLines(outLines, sourceLines[cursor:matchIndex]...)
		cursor = matchIndex

		for _, hunkLine := range hunk.lines {
			switch hunkLine.kind {
			case ' ':
				outLines = append(outLines, fileLine{text: hunkLine.text, hasNewline: hunkLine.hasNewline, eofMarker: hunkLine.newEOF})
				cursor++
			case '-':
				cursor++
			case '+':
				outLines = append(outLines, fileLine{text: hunkLine.text, hasNewline: hunkLine.hasNewline, eofMarker: hunkLine.newEOF})
			}
		}
	}

	outLines = appendSourceLines(outLines, sourceLines[cursor:]...)
	result := joinFileLines(outLines)
	if conflicts > 0 {
		return result, &ConflictError{ConflictingHunks: conflicts}
	}
	return result, nil
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

func locateHunk(sourceLines []fileLine, cursor int, hunk patchHunk) (int, bool) {
	preferred := hunk.oldStart - 1
	if hunk.oldCount == 0 {
		preferred = hunk.oldStart
	}
	if preferred < cursor {
		preferred = cursor
	}

	if hunk.newCount >= hunk.oldCount && preferred <= len(sourceLines) && postimageMatchesAt(sourceLines, preferred, desiredLines(hunk)) {
		return 0, false
	}

	for offset := 0; ; offset++ {
		candidate := preferred - offset
		if candidate >= cursor && candidate <= len(sourceLines) && hunkMatchesAt(sourceLines, candidate, hunk) {
			return candidate, true
		}

		candidate = preferred + offset
		if offset > 0 && candidate >= cursor && candidate <= len(sourceLines) && hunkMatchesAt(sourceLines, candidate, hunk) {
			return candidate, true
		}

		if preferred-offset < cursor && preferred+offset > len(sourceLines) {
			break
		}
	}

	return 0, false
}

func hunkMatchesAt(sourceLines []fileLine, start int, hunk patchHunk) bool {
	if hunk.newCount >= hunk.oldCount && postimageMatchesAt(sourceLines, start, desiredLines(hunk)) {
		return false
	}

	cursor := start
	for _, hunkLine := range hunk.lines {
		switch hunkLine.kind {
		case ' ', '-':
			if cursor >= len(sourceLines) {
				return false
			}
			if sourceLines[cursor].text != hunkLine.text ||
				sourceLines[cursor].hasNewline != hunkLine.hasNewline ||
				sourceLines[cursor].eofMarker != hunkLine.oldEOF {
				return false
			}
			cursor++
		case '+':
			continue
		default:
			return false
		}
	}

	return true
}

func postimageMatchesAt(sourceLines []fileLine, start int, desired []fileLine) bool {
	if len(desired) == 0 {
		return false
	}
	if start < 0 || start+len(desired) > len(sourceLines) {
		return false
	}

	for i := range desired {
		if sourceLines[start+i].text != desired[i].text ||
			sourceLines[start+i].hasNewline != desired[i].hasNewline ||
			sourceLines[start+i].eofMarker != desired[i].eofMarker {
			return false
		}
	}

	return true
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
