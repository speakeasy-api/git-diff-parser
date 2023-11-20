package pkg

import (
	"embed"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

//go:embed testdata
var testdata embed.FS

func TestParse(t *testing.T) {
	type SignificanceTest struct {
		name         string
		relativePath string
		input        string
		want         bool
		msg          string
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err, msg := SignificantChange(tt.input, func(diff *FileDiff, change *ContentChange) (bool, string) {
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

				if diff.Type == FileDiffTypeModified {
					return true, fmt.Sprintf("significant diff %#v", diff)
				}
				if change.Type == ContentChangeTypeNOOP {
					return false, ""
				}

				return true, fmt.Sprintf("significant change %#v in %s", change, diff.ToFile)
			})
			require.NoError(t, err)
			MatchMessageSnapshot(t, tt.relativePath+".msg", msg)
			assert.Equal(t, tt.want, got)
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
