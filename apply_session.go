package git_diff_parser

import "fmt"

type validatedPatch struct {
	hunks []patchHunk
}

type applySession struct {
	applier     *PatchApply
	sourceLines []fileLine
	image       []fileLine
	cursor      int
	conflicts   int
}

func (p *PatchApply) validateAndParsePatch(patchData []byte) (validatedPatch, error) {
	normalizedPatch := normalizePatchForValidation(patchData)
	if err := validateSingleFilePatch(normalizedPatch); err != nil {
		return validatedPatch{}, err
	}

	lines := splitLinesPreserveNewline(string(normalizedPatch))
	hunks, err := parseHunks(skipToHunks(lines))
	if err != nil {
		return validatedPatch{}, err
	}
	if !hunksContainChanges(hunks) {
		return validatedPatch{}, fmt.Errorf("patch contains no effective changes")
	}

	return validatedPatch{hunks: hunks}, nil
}

func (p *PatchApply) newApplySession(pristine []byte) *applySession {
	sourceLines := splitFileLines(pristine)
	return &applySession{
		applier:     p,
		sourceLines: sourceLines,
		image:       make([]fileLine, 0, len(sourceLines)),
	}
}

func (s *applySession) apply(patch validatedPatch) (ApplyResult, error) {
	for _, hunk := range patch.hunks {
		s.applyHunk(hunk)
	}

	s.appendSourceUntil(len(s.sourceLines))
	result := ApplyResult{Content: joinFileLines(s.image)}
	if s.conflicts > 0 {
		if s.applier.options.Mode == ApplyModeMerge {
			result.MergeConflicts = s.conflicts
			return result, &ApplyError{
				MergeConflicts:   s.conflicts,
				ConflictingHunks: s.conflicts,
			}
		}
		result.DirectMisses = s.conflicts
		return result, &ApplyError{
			DirectMisses: s.conflicts,
		}
	}
	return result, nil
}

func (s *applySession) applyHunk(hunk patchHunk) {
	matchIndex, matched := s.findPos(hunk)
	if !matched {
		s.conflicts++
		s.appendConflictingHunk(hunk)
		return
	}

	s.appendSourceUntil(matchIndex)

	for _, hunkLine := range hunk.lines {
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
	if s.applier.options.Mode == ApplyModeMerge {
		s.image = s.applier.appendConflict(s.image, s.sourceLines[conflictStart:conflictEnd], desiredLines(hunk))
	} else {
		s.image = appendSourceLines(s.image, s.sourceLines[conflictStart:conflictEnd]...)
	}
	s.cursor = conflictEnd
}

func (s *applySession) appendSourceUntil(limit int) {
	if limit <= s.cursor {
		return
	}
	s.image = appendSourceLines(s.image, s.sourceLines[s.cursor:limit]...)
	s.cursor = limit
}

func (s *applySession) findPos(hunk patchHunk) (int, bool) {
	preferred := hunk.oldStart - 1
	if hunk.oldCount == 0 {
		preferred = hunk.oldStart
	}
	if preferred < s.cursor {
		preferred = s.cursor
	}

	postimage := desiredLines(hunk)
	if hunk.newCount >= hunk.oldCount && preferred <= len(s.sourceLines) && matchFragment(s.sourceLines, preferred, postimage) {
		return 0, false
	}

	preimage := preimageLines(hunk)
	begin, end := splitAnchoredFragment(preimage)
	return s.findPosWithAnchors(preferred, begin, end)
}

func (s *applySession) findPosWithAnchors(preferred int, begin, end anchoredFragment) (int, bool) {
	for offset := 0; ; offset++ {
		left := preferred - offset
		if left >= s.cursor && left <= len(s.sourceLines) && matchAnchoredFragment(s.sourceLines, left, begin, end) {
			return left, true
		}

		right := preferred + offset
		if offset > 0 && right >= s.cursor && right <= len(s.sourceLines) && matchAnchoredFragment(s.sourceLines, right, begin, end) {
			return right, true
		}

		if left < s.cursor && right > len(s.sourceLines) {
			break
		}
	}

	return 0, false
}
