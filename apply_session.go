package git_diff_parser

import "fmt"

type validatedPatch struct {
	rejectHead string
	hunks      []patchHunk
}

type applySession struct {
	applier     *patchApply
	sourceLines []fileLine
	image       []fileLine
	cursor      int
	conflicts   []applyConflict
	rejectHead  string
}

type matchedHunk struct {
	sourceStart int
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
	if err := validateApplyFileDiff(fileDiff); err != nil {
		return validatedPatch{}, err
	}

	hunks := make([]patchHunk, 0, len(fileDiff.Hunks))
	for _, hunk := range fileDiff.Hunks {
		hunks = append(hunks, patchHunkFromHunk(hunk))
	}
	hunks, err := normalizePatchHunks(hunks, p.options)
	if err != nil {
		return validatedPatch{}, err
	}

	return validatedPatch{
		rejectHead: formatRejectHeader(fileDiff),
		hunks:      hunks,
	}, nil
}

func (p *patchApply) newApplySession(pristine []byte) *applySession {
	sourceLines := splitFileLines(pristine)
	return &applySession{
		applier:     p,
		sourceLines: sourceLines,
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

	preimage := preimageLines(hunk)
	if pos, ok := s.findPosForFragment(preferred, preimage); ok {
		return matchedHunk{
			sourceStart: pos,
			hunkStart:   0,
			hunkEnd:     len(hunk.lines),
		}, true
	}

	return matchedHunk{}, false
}

func (s *applySession) findPosForFragment(preferred int, fragment []fileLine) (int, bool) {
	for offset := 0; ; offset++ {
		left := preferred - offset
		if left >= s.cursor && matchFragment(s.sourceLines, left, fragment, s.ignoreWhitespace()) {
			return left, true
		}

		right := preferred + offset
		if offset > 0 && right >= s.cursor && matchFragment(s.sourceLines, right, fragment, s.ignoreWhitespace()) {
			return right, true
		}

		if left < s.cursor && right > len(s.sourceLines) {
			break
		}
	}

	return 0, false
}

func patchHunkFromHunk(hunk hunk) patchHunk {
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

func formatRejectHeader(fileDiff fileDiff) string {
	path := firstNonEmpty(fileDiff.ToFile, fileDiff.FromFile)
	if path == "" {
		return ""
	}
	return "diff a/" + path + " b/" + path + "\t(rejected hunks)"
}

func formatPatchHunkHeader(hunk hunk) string {
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
