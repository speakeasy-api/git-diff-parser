package git_diff_parser

import "fmt"

func normalizePatchHunks(hunks []patchHunk, options applyOptions) ([]patchHunk, error) {
	if len(hunks) == 0 {
		return hunks, nil
	}

	normalized := append([]patchHunk(nil), hunks...)
	if options.Reverse {
		normalized = reversePatchHunks(normalized)
	}
	if options.Recount {
		recountPatchHunks(normalized)
	}

	// These are present for API compatibility. Their broader Git parity work is
	// still a follow-on slice.
	_ = options.UnidiffZero
	_ = options.InaccurateEOF

	return normalized, nil
}

func reversePatchHunks(hunks []patchHunk) []patchHunk {
	reversed := make([]patchHunk, len(hunks))
	for i, hunk := range hunks {
		reversed[i] = reversePatchHunk(hunk)
	}
	return reversed
}

func reversePatchHunk(hunk patchHunk) patchHunk {
	reversed := patchHunk{
		header:   hunk.header,
		oldStart: hunk.newStart,
		oldCount: hunk.newCount,
		newStart: hunk.oldStart,
		newCount: hunk.oldCount,
		lines:    make([]patchLine, len(hunk.lines)),
	}

	for i, line := range hunk.lines {
		reversedLine := line
		switch reversedLine.kind {
		case '+':
			reversedLine.kind = '-'
		case '-':
			reversedLine.kind = '+'
		}
		reversedLine.oldEOF, reversedLine.newEOF = line.newEOF, line.oldEOF
		reversed.lines[i] = reversedLine
	}

	reversed.header = formatPatchHunkHeaderFromPatchHunk(reversed)
	return reversed
}

func recountPatchHunks(hunks []patchHunk) {
	for i := range hunks {
		recountPatchHunk(&hunks[i])
	}
}

func recountPatchHunk(hunk *patchHunk) {
	if hunk == nil {
		return
	}

	oldCount := 0
	newCount := 0
	for i := range hunk.lines {
		hunk.lines[i].oldEOF = false
		hunk.lines[i].newEOF = false
		switch hunk.lines[i].kind {
		case ' ', '-':
			oldCount++
		}
		switch hunk.lines[i].kind {
		case ' ', '+':
			newCount++
		}
	}

	hunk.oldCount = oldCount
	hunk.newCount = newCount
	markEOFMarkers(hunk.lines, oldCount, newCount)
	hunk.header = formatPatchHunkHeaderFromPatchHunk(*hunk)
}

func formatPatchHunkHeaderFromPatchHunk(hunk patchHunk) string {
	oldRange := formatPatchHunkRange(hunk.oldStart, hunk.oldCount)
	newRange := formatPatchHunkRange(hunk.newStart, hunk.newCount)
	return fmt.Sprintf("@@ -%s +%s @@", oldRange, newRange)
}
