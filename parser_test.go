package git_diff_parser_test

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	git_diff_parser "github.com/speakeasy-api/git-diff-parser"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata
var testdata embed.FS

func TestParse(t *testing.T) {
	type SignificanceTest struct {
		name         string
		relativePath string
		input        string
		want         bool
	}
	significantDiffs, err := testdata.ReadDir("testdata/significant")
	assert.NoError(t, err)
	insignificantDiffs, err := testdata.ReadDir("testdata/insignificant")
	assert.NoError(t, err)
	tests := []SignificanceTest{}
	for _, testFile := range significantDiffs {
		if !strings.HasSuffix(testFile.Name(), "diff") {
			continue
		}
		content, err := testdata.ReadFile("testdata/significant/" + testFile.Name())
		assert.NoError(t, err)
		tests = append(tests, SignificanceTest{
			name:         testFile.Name(),
			relativePath: filepath.Join("testdata/significant", testFile.Name()),
			input:        string(content),
			want:         true,
		})
	}
	for _, testFile := range insignificantDiffs {
		if !strings.HasSuffix(testFile.Name(), "diff") {
			continue
		}
		content, err := testdata.ReadFile("testdata/insignificant/" + testFile.Name())
		assert.NoError(t, err)
		tests = append(tests, SignificanceTest{
			name:         testFile.Name(),
			relativePath: filepath.Join("testdata/insignificant", testFile.Name()),
			input:        string(content),
			want:         false,
		})
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, msg, err := git_diff_parser.SignificantChange(test.input, func(diff *git_diff_parser.FileDiff, change *git_diff_parser.ContentChange) (bool, string) {
				if diff.ToFile == "gen.yaml" || diff.ToFile == "RELEASES.md" {
					return false, ""
				}
				if strings.Contains(change.From, "0.13.5") && strings.Contains(change.To, "0.13.6") {
					return false, ""
				}
				if strings.Contains(change.From, "1.120.3") && strings.Contains(change.To, "1.120.4") {
					return false, ""
				}
				if strings.Contains(change.From, "2.192.1") && strings.Contains(change.To, "2.192.3") {
					return false, ""
				}

				if diff.Type == git_diff_parser.FileDiffTypeModified {
					return true, fmt.Sprintf("significant diff %#v", diff)
				}
				if change.Type == git_diff_parser.ContentChangeTypeNOOP {
					return false, ""
				}

				return true, fmt.Sprintf("significant change %#v in %s", change, diff.ToFile)
			})
			require.NoError(t, err)
			MatchMessageSnapshot(t, test.relativePath+".msg", msg)
			assert.Equal(t, test.want, got)
		})
	}
}

func MatchMessageSnapshot(t *testing.T, snapshotName string, content string) {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	dirname := filepath.Dir(filename)
	snapshotFile := filepath.Join(dirname, snapshotName)
	if _, err := os.Stat(snapshotFile); err != nil {
		f, err := os.OpenFile(snapshotFile, os.O_APPEND|os.O_CREATE|os.O_RDWR, os.ModePerm)
		require.NoError(t, err)
		defer f.Close()
		_, err = f.WriteString(content)
		require.NoError(t, err)
		return
	}
	f, err := os.ReadFile(snapshotFile)
	require.NoError(t, err)
	require.Equal(t, string(f), content)
}
