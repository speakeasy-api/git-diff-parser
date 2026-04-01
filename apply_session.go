package git_diff_parser

import "fmt"

type validatedPatch struct {
	rejectHead string
	hunks      []patchHunk
}

type applySession struct {
	applier     *patchApply
	sourceLines []fileLine
	patched     []bool
	image       []fileLine
	cursor      int
	conflicts   []applyConflict
	rejectHead  string
}

type matchedHunk struct {
	sourceStart int
	sourceEnd   int
	hunkStart   int
	hunkEnd     int
}

func (p *patchApply) validateAndParsePatch(patchData []byte) (validatedPatch, error) {
	normalizedPatch := normalizePatchForValidation(patchData)
	parsed, errs := parse(string(normalizedPatch))
	if len(errs) > 0 {
		return validatedPatch{}, fmt.Errorf("unsupported patch syntax: %w", errs[0])
	}
	if len(parsed.FileDiff) != 1 {
		return validatedPatch{}, fmt.Errorf("expected exactly 1 file diff, found %d", len(parsed.FileDiff))
	}

	fileDiff := parsed.FileDiff[0]
	if err := validateApplyFileDiff(&fileDiff); err != nil {
		return validatedPatch{}, err
	}

	hunks := make([]patchHunk, 0, len(fileDiff.Hunks))
	for i := range fileDiff.Hunks {
		hunks = append(hunks, patchHunkFromHunk(&fileDiff.Hunks[i]))
	}

	return validatedPatch{
		rejectHead: formatRejectHeader(&fileDiff),
		hunks:      hunks,
	}, nil
}

func (p *patchApply) newApplySession(pristine []byte) *applySession {
	sourceLines := splitFileLines(pristine)
	return &applySession{
		applier:     p,
		sourceLines: sourceLines,
		patched:     make([]bool, len(sourceLines)),
		image:       make([]fileLine, 0, len(sourceLines)),
	}
}

func (s *applySession) apply(patch validatedPatch) (applyOutcome, error) {
	s.rejectHead = patch.rejectHead

	for _, hunk := range patch.hunks {
		match, matched := s.findPos(hunk)
		if !matched {
			s.appendConflictingHunk(hunk)
			continue
		}

		s.applyHunk(hunk, match)
	}

	s.appendSourceUntil(len(s.sourceLines))
	return applyOutcome{
		content:    append([]fileLine(nil), s.image...),
		conflicts:  append([]applyConflict(nil), s.conflicts...),
		rejectHead: s.rejectHead,
	}, nil
}

func (s *applySession) applyHunk(hunk patchHunk, match matchedHunk) {
	s.appendSourceUntil(match.sourceStart)

	for _, hunkLine := range hunk.lines[match.hunkStart:match.hunkEnd] {
		switch hunkLine.kind {
		case ' ':
			s.image = append(s.image, fileLine{text: hunkLine.text, hasNewline: hunkLine.hasNewline, eofMarker: hunkLine.newEOF})
			s.cursor++
		case '-':
			s.cursor++
		case '+':
			s.image = append(s.image, fileLine{text: hunkLine.text, hasNewline: hunkLine.hasNewline, eofMarker: hunkLine.newEOF})
		}
	}

	if !s.allowOverlap() {
		for i := match.sourceStart; i < match.sourceEnd && i < len(s.patched); i++ {
			s.patched[i] = true
		}
	}
}

func (s *applySession) appendConflictingHunk(hunk patchHunk) {
	conflictStart := hunk.oldStart - 1
	if conflictStart < s.cursor {
		conflictStart = s.cursor
	}
	if conflictStart > len(s.sourceLines) {
		conflictStart = len(s.sourceLines)
	}

	conflictEnd := conflictStart + hunk.oldCount
	if conflictEnd > len(s.sourceLines) {
		conflictEnd = len(s.sourceLines)
	}

	s.appendSourceUntil(conflictStart)
	offset := len(s.image)
	ours := append([]fileLine(nil), s.sourceLines[conflictStart:conflictEnd]...)
	theirs := desiredLines(hunk)
	s.image = appendSourceLines(s.image, ours...)
	s.conflicts = append(s.conflicts, applyConflict{
		offset: offset,
		hunk:   hunk,
		ours:   ours,
		theirs: theirs,
	})
	s.cursor = conflictEnd
}

func (s *applySession) appendSourceUntil(limit int) {
	if limit <= s.cursor {
		return
	}
	s.image = appendSourceLines(s.image, s.sourceLines[s.cursor:limit]...)
	s.cursor = limit
}

func (s *applySession) findPos(hunk patchHunk) (matchedHunk, bool) {
	preferred := hunk.oldStart - 1
	if hunk.oldCount == 0 {
		preferred = hunk.oldStart
	}
	if preferred < s.cursor {
		preferred = s.cursor
	}

	postimage := desiredLines(hunk)
	if hunk.newCount >= hunk.oldCount && preferred <= len(s.sourceLines) && matchFragment(s.sourceLines, preferred, postimage, s.ignoreWhitespace()) {
		return matchedHunk{}, false
	}

	matchBeginning := hunk.oldStart == 0 || hunk.oldStart == 1
	leading, trailing := hunkContext(hunk.lines)
	matchEnd := trailing == 0

	hunkStart := 0
	hunkEnd := len(hunk.lines)

	for {
		preimage := preimageLinesWindow(hunk, hunkStart, hunkEnd)
		if pos, ok := s.findPosForFragment(preferred, preimage, matchBeginning, matchEnd); ok {
			return matchedHunk{
				sourceStart: pos,
				sourceEnd:   pos + len(preimage),
				hunkStart:   hunkStart,
				hunkEnd:     hunkEnd,
			}, true
		}

		if leading <= s.minContext() && trailing <= s.minContext() {
			break
		}
		if matchBeginning || matchEnd {
			matchBeginning = false
			matchEnd = false
			continue
		}
		if leading >= trailing && hunkStart < hunkEnd {
			hunkStart++
			preferred--
			if preferred < s.cursor {
				preferred = s.cursor
			}
			leading--
		}
		if trailing > leading && hunkStart < hunkEnd {
			hunkEnd--
			trailing--
		}
	}

	return matchedHunk{}, false
}

func (s *applySession) findPosForFragment(preferred int, fragment []fileLine, matchBeginning, matchEnd bool) (int, bool) {
	maxStart := s.fragmentEndLimit(fragment) - len(fragment)
	if maxStart < 0 {
		maxStart = s.fragmentEndLimit(fragment)
	}
	if matchBeginning {
		preferred = 0
	} else if matchEnd {
		preferred = maxStart
	}
	if preferred > maxStart {
		preferred = maxStart
	}
	if preferred < s.cursor {
		preferred = s.cursor
	}

	for offset := 0; ; offset++ {
		left := preferred - offset
		if left >= s.cursor && s.matchFragmentAt(left, fragment, matchBeginning, matchEnd) {
			return left, true
		}

		right := preferred + offset
		if offset > 0 && right >= s.cursor && s.matchFragmentAt(right, fragment, matchBeginning, matchEnd) {
			return right, true
		}

		if left < s.cursor && right > maxStart {
			break
		}
	}

	return 0, false
}

func (s *applySession) matchFragmentAt(start int, fragment []fileLine, matchBeginning, matchEnd bool) bool {
	if matchBeginning && start != 0 {
		return false
	}
	if start < 0 {
		return false
	}
	if len(fragment) == 0 {
		if matchEnd {
			return start == s.sourceContentLines()
		}
		return start <= s.sourceContentLines()
	}
	if start+len(fragment) > len(s.sourceLines) {
		return false
	}
	if matchEnd && start+len(fragment) != s.fragmentEndLimit(fragment) {
		return false
	}
	if !s.allowOverlap() {
		for i := start; i < start+len(fragment); i++ {
			if i < len(s.patched) && s.patched[i] {
				return false
			}
		}
	}
	return matchFragment(s.sourceLines, start, fragment, s.ignoreWhitespace())
}

func patchHunkFromHunk(hunk *hunk) patchHunk {
	lines := make([]patchLine, 0, len(hunk.Lines))
	for _, line := range hunk.Lines {
		lines = append(lines, patchLine{
			kind:       line.Kind,
			text:       line.Text,
			hasNewline: line.HasNewline,
			oldEOF:     line.OldEOF,
			newEOF:     line.NewEOF,
		})
	}

	return patchHunk{
		header:   formatPatchHunkHeader(hunk),
		oldStart: hunk.StartLineNumberOld,
		oldCount: hunk.CountOld,
		newStart: hunk.StartLineNumberNew,
		newCount: hunk.CountNew,
		lines:    lines,
	}
}

func formatRejectHeader(fileDiff *fileDiff) string {
	path := firstNonEmpty(fileDiff.ToFile, fileDiff.FromFile)
	if path == "" {
		return ""
	}
	return "diff a/" + path + " b/" + path + "\t(rejected hunks)"
}

func formatPatchHunkHeader(hunk *hunk) string {
	oldRange := formatPatchHunkRange(hunk.StartLineNumberOld, hunk.CountOld)
	newRange := formatPatchHunkRange(hunk.StartLineNumberNew, hunk.CountNew)
	return fmt.Sprintf("@@ -%s +%s @@", oldRange, newRange)
}

func formatPatchHunkRange(start, count int) string {
	if count == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

func (s *applySession) ignoreWhitespace() bool {
	return s.applier != nil && s.applier.options.IgnoreWhitespace
}

func (s *applySession) allowOverlap() bool {
	return s.applier != nil && s.applier.options.AllowOverlap
}

func (s *applySession) minContext() int {
	if s.applier == nil {
		return 0
	}
	return s.applier.options.MinContext
}

func (s *applySession) sourceContentLines() int {
	if n := len(s.sourceLines); n > 0 && s.sourceLines[n-1].eofMarker {
		return n - 1
	}
	return len(s.sourceLines)
}

func (s *applySession) fragmentEndLimit(fragment []fileLine) int {
	if len(fragment) > 0 && fragment[len(fragment)-1].eofMarker {
		return len(s.sourceLines)
	}
	return s.sourceContentLines()
}

func hunkContext(lines []patchLine) (leading, trailing int) {
	firstChange := len(lines)
	lastChange := -1
	for i, line := range lines {
		if line.kind == '+' || line.kind == '-' {
			if firstChange == len(lines) {
				firstChange = i
			}
			lastChange = i
		}
	}

	if lastChange < 0 {
		return len(lines), len(lines)
	}

	leading = 0
	for i := 0; i < firstChange; i++ {
		if lines[i].kind == ' ' {
			leading++
		}
	}

	trailing = 0
	for i := len(lines) - 1; i > lastChange; i-- {
		if lines[i].kind == ' ' {
			trailing++
		}
	}

	return leading, trailing
}
