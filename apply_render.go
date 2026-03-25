package git_diff_parser

import "bytes"

func renderApplyResult(pristine []byte, outcome applyOutcome, options ApplyOptions) ApplyResult {
	result := ApplyResult{
		Content: joinFileLines(outcome.content),
		Reject:  renderRejectContent(outcome.conflicts),
	}

	if len(outcome.conflicts) == 0 {
		return result
	}

	switch options.Mode {
	case ApplyModeMerge:
		result.Content = renderMergeContent(outcome.content, outcome.conflicts, options.ConflictLabels)
		result.MergeConflicts = len(outcome.conflicts)
	default:
		result.Content = append([]byte{}, pristine...)
		result.DirectMisses = len(outcome.conflicts)
	}

	return result
}

func renderMergeContent(base []fileLine, conflicts []applyConflict, labels ConflictLabels) []byte {
	if len(conflicts) == 0 {
		return joinFileLines(base)
	}

	rendered := append([]fileLine(nil), base...)
	for i := len(conflicts) - 1; i >= 0; i-- {
		conflict := conflicts[i]
		if conflict.offset < 0 || conflict.offset > len(rendered) {
			continue
		}

		end := conflict.offset + len(conflict.ours)
		if end > len(rendered) {
			end = len(rendered)
		}

		replacement := renderConflictLines(labels, conflict.ours, conflict.theirs)
		rendered = append(rendered[:conflict.offset], append(replacement, rendered[end:]...)...)
	}

	return joinFileLines(rendered)
}

func renderRejectContent(conflicts []applyConflict) []byte {
	if len(conflicts) == 0 {
		return nil
	}

	var buf bytes.Buffer
	for i, conflict := range conflicts {
		if i > 0 {
			buf.WriteByte('\n')
		}
		if conflict.hunk.header != "" {
			buf.WriteString(conflict.hunk.header)
			buf.WriteByte('\n')
		}
		for _, line := range conflict.hunk.lines {
			buf.WriteByte(line.kind)
			buf.WriteString(line.text)
			if line.hasNewline {
				buf.WriteByte('\n')
			}
		}
	}
	return buf.Bytes()
}

func renderConflictLines(labels ConflictLabels, ours, theirs []fileLine) []fileLine {
	lines := []fileLine{
		{text: "<<<<<<< " + labels.Current, hasNewline: true},
	}
	lines = appendSourceLines(lines, ours...)
	lines = ensureTrailingNewline(lines)
	lines = append(lines, fileLine{text: "=======", hasNewline: true})
	lines = appendSourceLines(lines, theirs...)
	lines = ensureTrailingNewline(lines)
	lines = append(lines, fileLine{text: ">>>>>>> " + labels.Incoming, hasNewline: true})
	return lines
}
