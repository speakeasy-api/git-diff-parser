package git_diff_parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindPosRejectsAlreadyAppliedPostimage(t *testing.T) {
	session := &applySession{
		sourceLines: splitFileLines([]byte("a\nb\nx\nc\n")),
	}
	hunk := patchHunk{
		oldStart: 1,
		oldCount: 3,
		newCount: 4,
		lines: []patchLine{
			{kind: ' ', text: "a", hasNewline: true},
			{kind: ' ', text: "b", hasNewline: true},
			{kind: '+', text: "x", hasNewline: true},
			{kind: ' ', text: "c", hasNewline: true},
		},
	}

	match, matched := session.findPos(hunk)
	assert.Equal(t, matchedHunk{}, match)
	assert.False(t, matched)
}

func TestMatchFragment_IgnoreWhitespace(t *testing.T) {
	source := splitFileLines([]byte("alpha\n  beta\ncharlie\n"))
	fragment := []fileLine{
		{text: "alpha", hasNewline: true},
		{text: "beta", hasNewline: true},
		{text: "charlie", hasNewline: true},
	}

	require.False(t, matchFragment(source, 0, fragment, false))
	require.True(t, matchFragment(source, 0, fragment, true))
}

func TestFindPosForFragmentMatchesExactBlock(t *testing.T) {
	session := &applySession{
		sourceLines: splitFileLines([]byte("zero\nalpha\nbravo\ncharlie\n")),
	}
	match, matched := session.findPosForFragment(1, []fileLine{
		{text: "alpha", hasNewline: true},
		{text: "bravo", hasNewline: true},
		{text: "charlie", hasNewline: true},
	}, false, false)
	require.True(t, matched)
	assert.Equal(t, 1, match)
}

func TestFindPosWithMinContextReducesLeadingContext(t *testing.T) {
	session := &applySession{
		applier:     &patchApply{options: applyOptions{MinContext: 1, MinContextSet: true}},
		sourceLines: splitFileLines([]byte("a0\nA1\na2\na3\na4\na5\na6\n")),
		patched:     make([]bool, 7),
	}
	hunk := patchHunk{
		oldStart: 2,
		oldCount: 5,
		newStart: 2,
		newCount: 5,
		lines: []patchLine{
			{kind: ' ', text: "a1", hasNewline: true},
			{kind: ' ', text: "a2", hasNewline: true},
			{kind: '-', text: "a3", hasNewline: true},
			{kind: '+', text: "A3", hasNewline: true},
			{kind: ' ', text: "a4", hasNewline: true},
			{kind: ' ', text: "a5", hasNewline: true},
		},
	}

	match, matched := session.findPos(hunk)
	require.True(t, matched)
	assert.Equal(t, 2, match.sourceStart)
	assert.Equal(t, 5, match.sourceEnd)
	assert.Equal(t, 1, match.hunkStart)
	assert.Equal(t, 5, match.hunkEnd)
}

func TestFindPosForFragmentRejectsPatchedRangesWithoutOverlap(t *testing.T) {
	session := &applySession{
		sourceLines: splitFileLines([]byte("zero\nalpha\nbravo\ncharlie\n")),
		patched:     []bool{false, true, true, false},
	}

	_, matched := session.findPosForFragment(1, []fileLine{
		{text: "alpha", hasNewline: true},
		{text: "bravo", hasNewline: true},
	}, false, false)
	assert.False(t, matched)
}

func TestFindPosForFragmentAllowsPatchedRangesWithOverlap(t *testing.T) {
	session := &applySession{
		applier:     &patchApply{options: applyOptions{AllowOverlap: true}},
		sourceLines: splitFileLines([]byte("zero\nalpha\nbravo\ncharlie\n")),
		patched:     []bool{false, true, true, false},
	}

	match, matched := session.findPosForFragment(1, []fileLine{
		{text: "alpha", hasNewline: true},
		{text: "bravo", hasNewline: true},
	}, false, false)
	require.True(t, matched)
	assert.Equal(t, 1, match)
}
