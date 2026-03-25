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

	require.True(t, matchAnchoredFragment(source, 1, begin, end))
	require.False(t, matchAnchoredFragment(source, 5, begin, end))
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

	pos, matched := session.findPos(hunk)
	assert.Equal(t, 0, pos)
	assert.False(t, matched)
}
