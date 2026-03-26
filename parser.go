package git_diff_parser

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var errUnhandled = errors.New("unhandled git diff syntax")

func newHunk(line string) (hunk, error) {
	namedHunkRegex := regexp.MustCompile(`(?m)^@@ -(?P<start_old>\d+),?(?P<count_old>\d+)? \+(?P<start_new>\d+),?(?P<count_new>\d+)? @@`)
	match := namedHunkRegex.FindStringSubmatch(line)
	if len(match) == 0 {
		return hunk{}, fmt.Errorf("invalid hunk header: %q", line)
	}
	result := make(map[string]string)
	for i, name := range namedHunkRegex.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = match[i]
		}
	}
	startLineNumberOld, err := strconv.Atoi(result["start_old"])
	if err != nil {
		return hunk{}, fmt.Errorf("failed to parse start line number old: %w", err)
	}
	countOld, err := strconv.Atoi(result["count_old"])
	if err != nil {
		countOld = 1
	}
	startLineNumberNew, err := strconv.Atoi(result["start_new"])
	if err != nil {
		return hunk{}, fmt.Errorf("failed to parse start line number new: %w", err)
	}
	countNew, err := strconv.Atoi(result["count_new"])
	if err != nil {
		countNew = 1
	}
	return hunk{
		StartLineNumberOld: startLineNumberOld,
		CountOld:           countOld,
		StartLineNumberNew: startLineNumberNew,
		CountNew:           countNew,
	}, nil
}

type parserMode int

const (
	modeHeader parserMode = iota
	modeHunk
	modeBinary
)

type parser struct {
	diff diff
	err  []error
	mode parserMode
}

func (p *parser) VisitLine(diff string) {
	line := trimSingleLineEnding(diff)
	hasNewline := strings.HasSuffix(diff, "\n")

	if p.tryVisitHeader(line) {
		return
	}
	if p.tryVisitBinary(line) {
		return
	}
	if p.tryVisitHunkHeader(line) {
		return
	}

	fileHEAD := len(p.diff.FileDiff) - 1
	if fileHEAD < 0 {
		p.err = append(p.err, fmt.Errorf("%w: %s", errUnhandled, line))
		return
	}

	hunkHEAD := len(p.diff.FileDiff[fileHEAD].Hunks) - 1
	if hunkHEAD < 0 {
		p.err = append(p.err, fmt.Errorf("%w: %s", errUnhandled, diff))
		return
	}

	hunk := &p.diff.FileDiff[fileHEAD].Hunks[hunkHEAD]

	// swallow extra, unused lines from start
	if strings.HasPrefix(line, "~") && !hunk.ChangeList.isSignificant() {
		hunk.StartLineNumberOld++
		hunk.StartLineNumberNew++
		hunk.CountOld--
		hunk.CountNew--
		hunk.ChangeList = []contentChange{}
	}

	if strings.HasPrefix(line, "+") {
		if len(hunk.ChangeList) > 0 && hunk.ChangeList[len(hunk.ChangeList)-1].Type == contentChangeTypeDelete {
			hunk.ChangeList[len(hunk.ChangeList)-1].Type = contentChangeTypeModify
			hunk.ChangeList[len(hunk.ChangeList)-1].To = trimSingleLineEnding(strings.TrimPrefix(line, "+"))
			hunk.Lines = append(hunk.Lines, hunkLine{
				Kind:       '+',
				Text:       trimSingleLineEnding(strings.TrimPrefix(line, "+")),
				HasNewline: hasNewline,
			})
			return
		}
		hunk.ChangeList = append(hunk.ChangeList, contentChange{
			Type: contentChangeTypeAdd,
			From: "",
			To:   trimSingleLineEnding(strings.TrimPrefix(line, "+")),
		})
		hunk.Lines = append(hunk.Lines, hunkLine{
			Kind:       '+',
			Text:       trimSingleLineEnding(strings.TrimPrefix(line, "+")),
			HasNewline: hasNewline,
		})
		return
	}

	if strings.HasPrefix(line, "-") {
		hunk.ChangeList = append(hunk.ChangeList, contentChange{
			Type: contentChangeTypeDelete,
			From: trimSingleLineEnding(strings.TrimPrefix(line, "-")),
			To:   "",
		})
		hunk.Lines = append(hunk.Lines, hunkLine{
			Kind:       '-',
			Text:       trimSingleLineEnding(strings.TrimPrefix(line, "-")),
			HasNewline: hasNewline,
		})
		return
	}

	if strings.HasPrefix(line, " ") {
		hunk.ChangeList = append(hunk.ChangeList, contentChange{
			Type: contentChangeTypeNOOP,
			From: line,
			To:   line,
		})
		hunk.Lines = append(hunk.Lines, hunkLine{
			Kind:       ' ',
			Text:       trimSingleLineEnding(strings.TrimPrefix(line, " ")),
			HasNewline: hasNewline,
		})
		return
	}

	if line == "~" {
		hunk.ChangeList = append(hunk.ChangeList, contentChange{
			Type: contentChangeTypeNOOP,
			From: "\n",
			To:   "\n",
		})
		return
	}

	if strings.HasPrefix(line, `\ No newline at end of file`) {
		if n := len(hunk.Lines); n > 0 {
			hunk.Lines[n-1].markNoNewline()
		} else {
			p.err = append(p.err, fmt.Errorf("unexpected no-newline marker without a preceding patch line"))
			return
		}
		hunk.ChangeList = append(hunk.ChangeList, contentChange{
			Type: contentChangeTypeNOOP,
			From: line,
			To:   line,
		})
		return
	}

	if line == "" {
		hunk.ChangeList = append(hunk.ChangeList, contentChange{
			Type: contentChangeTypeNOOP,
			From: line,
			To:   line,
		})
		return
	}

	p.err = append(p.err, fmt.Errorf("unexpected hunk line %q", line))
}

func (p *parser) tryVisitHeader(diff string) bool {
	// format: "diff --git a/README.md b/README.md"
	if strings.HasPrefix(diff, "diff ") {
		p.finalizeCurrentHunk()
		p.diff.FileDiff = append(p.diff.FileDiff, p.parseDiffLine(diff))
		p.mode = modeHeader
		return true
	}

	fileHEAD := len(p.diff.FileDiff) - 1
	if len(diff) == 0 && p.mode == modeHeader {
		return true
	}
	if fileHEAD < 0 {
		p.err = append(p.err, fmt.Errorf("%w: %s", errUnhandled, diff))
		return true
	}
	if p.mode != modeHeader {
		return false
	}

	if strings.HasPrefix(diff, "+++ ") || strings.HasPrefix(diff, "--- ") {
		// ignore -- we're still in the FileDiff and we've already captured the file names
		return true
	}
	if strings.HasPrefix(diff, "index ") {
		p.parseIndexHeader(diff, fileHEAD)
		return true
	}
	if strings.HasPrefix(diff, "similarity index ") {
		p.diff.FileDiff[fileHEAD].SimilarityIndex = parsePercentValue(strings.TrimPrefix(diff, "similarity index "))
		return true
	}
	if strings.HasPrefix(diff, "dissimilarity index ") {
		p.diff.FileDiff[fileHEAD].DissimilarityIndex = parsePercentValue(strings.TrimPrefix(diff, "dissimilarity index "))
		return true
	}
	if strings.HasPrefix(diff, "copy from ") {
		p.diff.FileDiff[fileHEAD].CopyFrom = strings.TrimPrefix(diff, "copy from ")
		p.diff.FileDiff[fileHEAD].Type = fileDiffTypeModified
		return true
	}
	if strings.HasPrefix(diff, "copy to ") {
		p.diff.FileDiff[fileHEAD].CopyTo = strings.TrimPrefix(diff, "copy to ")
		p.diff.FileDiff[fileHEAD].Type = fileDiffTypeModified
		return true
	}
	if strings.HasPrefix(diff, "rename from ") {
		p.diff.FileDiff[fileHEAD].RenameFrom = strings.TrimPrefix(diff, "rename from ")
		p.diff.FileDiff[fileHEAD].Type = fileDiffTypeModified
		return true
	}
	if strings.HasPrefix(diff, "rename to ") {
		p.diff.FileDiff[fileHEAD].RenameTo = strings.TrimPrefix(diff, "rename to ")
		p.diff.FileDiff[fileHEAD].Type = fileDiffTypeModified
		return true
	}

	if done := p.visitFileModeHeader(diff, fileHEAD); done {
		return done
	}

	if strings.HasPrefix(diff, "GIT binary patch") {
		p.diff.FileDiff[fileHEAD].Type = fileDiffTypeModified
		p.diff.FileDiff[fileHEAD].IsBinary = true
		p.mode = modeBinary
		return true
	}

	// binary files ... differ
	if strings.HasPrefix(strings.ToLower(diff), "binary files ") {
		return true
	}

	if strings.HasPrefix(diff, "similarity") {
		return true
	}

	// continue to parse if fileHEAD > 0
	return fileHEAD < 0
}

func (p *parser) visitFileModeHeader(diff string, fileHEAD int) bool {
	if strings.HasPrefix(diff, "new file mode ") {
		p.diff.FileDiff[fileHEAD].Type = fileDiffTypeModified
		p.diff.FileDiff[fileHEAD].NewMode = strings.TrimPrefix(diff, "new file mode ")
		return true
	}
	if strings.HasPrefix(diff, "new mode ") {
		p.diff.FileDiff[fileHEAD].Type = fileDiffTypeModified
		p.diff.FileDiff[fileHEAD].NewMode = strings.TrimPrefix(diff, "new mode ")
		return true
	}

	if strings.HasPrefix(diff, "deleted file mode ") {
		p.diff.FileDiff[fileHEAD].Type = fileDiffTypeDeleted
		p.diff.FileDiff[fileHEAD].OldMode = strings.TrimPrefix(diff, "deleted file mode ")
		return true
	}
	if strings.HasPrefix(diff, "old mode ") {
		p.diff.FileDiff[fileHEAD].Type = fileDiffTypeModified
		p.diff.FileDiff[fileHEAD].OldMode = strings.TrimPrefix(diff, "old mode ")
		return true
	}
	return false
}

func (p *parser) parseIndexHeader(diff string, fileHEAD int) {
	fields := strings.Fields(strings.TrimPrefix(diff, "index "))
	if len(fields) == 0 {
		return
	}

	parts := strings.SplitN(fields[0], "..", 2)
	if len(parts) == 2 {
		p.diff.FileDiff[fileHEAD].IndexOld = parts[0]
		p.diff.FileDiff[fileHEAD].IndexNew = parts[1]
	}
	if len(fields) > 1 {
		p.diff.FileDiff[fileHEAD].IndexMode = fields[1]
	}
}

func (p *parser) tryVisitBinary(diff string) bool {
	if p.mode != modeBinary {
		return false
	}
	fileHEAD := len(p.diff.FileDiff) - 1
	if fileHEAD < 0 {
		return true
	}
	if strings.HasPrefix(diff, "delta ") {
		p.diff.FileDiff[fileHEAD].Type = fileDiffTypeModified
		startByteCount, err := strconv.Atoi(strings.Split(diff, " ")[1])
		if err != nil {
			return true
		}

		p.diff.FileDiff[fileHEAD].BinaryPatch = append(p.diff.FileDiff[fileHEAD].BinaryPatch, binaryPatch{
			Type:    binaryDeltaTypeDelta,
			Count:   startByteCount,
			Content: "",
		})
		return true
	}
	if strings.HasPrefix(diff, "literal ") {
		p.diff.FileDiff[fileHEAD].Type = fileDiffTypeModified
		startByteCount, err := strconv.Atoi(strings.Split(diff, " ")[1])
		if err != nil {
			return true
		}
		p.diff.FileDiff[fileHEAD].BinaryPatch = append(p.diff.FileDiff[fileHEAD].BinaryPatch, binaryPatch{
			Type:    binaryDeltaTypeLiteral,
			Count:   startByteCount,
			Content: "",
		})
		return true
	}

	if len(p.diff.FileDiff[fileHEAD].BinaryPatch) > 0 {
		p.diff.FileDiff[fileHEAD].BinaryPatch[len(p.diff.FileDiff[fileHEAD].BinaryPatch)-1].Content += diff
		return true
	}
	return true
}

func (p *parser) tryVisitHunkHeader(diff string) bool {
	fileHEAD := len(p.diff.FileDiff) - 1
	if fileHEAD < 0 {
		return false
	}
	if strings.HasPrefix(diff, "@@") {
		p.finalizeCurrentHunk()
		hunk, err := newHunk(diff)
		if err != nil {
			p.err = append(p.err, err)
		}
		p.diff.FileDiff[fileHEAD].Hunks = append(p.diff.FileDiff[fileHEAD].Hunks, hunk)
		p.mode = modeHunk
		return true
	}
	return false
}

func (p *parser) finalizeCurrentHunk() {
	if len(p.diff.FileDiff) == 0 {
		return
	}
	fileHEAD := len(p.diff.FileDiff) - 1
	hunks := p.diff.FileDiff[fileHEAD].Hunks
	if len(hunks) == 0 {
		return
	}
	p.diff.FileDiff[fileHEAD].Hunks[len(hunks)-1].markEOFMarkers()
}

func (p *parser) parseDiffLine(line string) fileDiff {
	line = trimSingleLineEnding(line)
	filesStr := line[11:]
	var oldPath, newPath string

	quoteIndex := strings.Index(filesStr, "\"")
	switch quoteIndex {
	case -1:
		segs := strings.Split(filesStr, " ")
		oldPath = segs[0][2:]
		newPath = segs[1][2:]

	case 0:
		const indexDelta = 2
		nextQuoteIndex := strings.Index(filesStr[indexDelta:], "\"") + indexDelta
		oldPath = filesStr[3:nextQuoteIndex]
		newQuoteIndex := strings.Index(filesStr[nextQuoteIndex+1:], "\"") + nextQuoteIndex + 1
		if newQuoteIndex < 0 {
			newPath = filesStr[nextQuoteIndex+4:]
		} else {
			newPath = filesStr[newQuoteIndex+3 : len(filesStr)-1]
		}

	default:
		segs := strings.Split(filesStr, " ")
		oldPath = segs[0][2:]
		newPath = segs[1][3 : len(segs[1])-1]
	}

	return fileDiff{
		FromFile: oldPath,
		ToFile:   newPath,
	}
}

func parsePercentValue(raw string) int {
	raw = strings.TrimSuffix(raw, "%")
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}

// Converts git diff --word-diff=porcelain output to a diff object.
func parse(diff string) (diff, []error) {
	p := parser{}
	lines := splitLinesPreserveNewline(diff)
	for i := 0; i < len(lines); i++ {
		p.VisitLine(lines[i])
	}
	if strings.HasSuffix(diff, "\n") {
		p.VisitLine("")
	}
	p.finalizeCurrentHunk()
	return p.diff, p.err
}

// SignificantChange Allows a structured diff to be passed into the `isSignificant` function to determine significance. That function can return a message, which is optionally passed as the final argument
// Returns the first significant change found, or false if non found.
func significantChange(diff string, isSignificant func(*fileDiff, *contentChange) (bool, string)) (bool, string, error) {
	parsed, err := parse(diff)
	if len(err) > 0 {
		return true, "", fmt.Errorf("failed to parse diff: %w", err[0])
	}
	for _, fileDiff := range parsed.FileDiff {
		if significant, msg := isSignificant(&fileDiff, &contentChange{}); significant {
			return true, msg, nil
		}

		for _, hunk := range fileDiff.Hunks {
			for _, change := range hunk.ChangeList {
				if significant, msg := isSignificant(&fileDiff, &change); significant {
					return true, msg, nil
				}
			}
		}
	}

	return false, "", nil
}
