package git_diff_parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchAnchoredFragmentRequiresBothEnds(t *testing.T) {
	source := splitFileLines([]byte("one\na\nb\nc\nd\na\nb\nx\nd\n"))
	begin, end := splitAnchoredFragment([]fileLine{
		{text: "a", hasNewline: true},
		{text: "b", hasNewline: true},
		{text: "c", hasNewline: true},
		{text: "d", hasNewline: true},
	})

	require.True(t, matchAnchoredFragment(source, 1, begin, end, false))
	require.False(t, matchAnchoredFragment(source, 5, begin, end, false))
}

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
	})
	require.True(t, matched)
	assert.Equal(t, 1, match)
}
