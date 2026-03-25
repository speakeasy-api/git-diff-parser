package git_diff_parser_test

import (
	"path/filepath"
	"testing"

	git_diff_parser "github.com/speakeasy-api/git-diff-parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePatchset(t *testing.T) {
	t.Parallel()

	patchA := buildPatch(t, "alpha.txt", []byte("alpha\none\n"), []byte("alpha\ntwo\n"))
	patchB := buildPatch(t, "beta.txt", []byte("beta\none\n"), []byte("beta\ntwo\n"))
	patchsetData := append(append([]byte{}, patchA...), patchB...)

	patchset, errs := git_diff_parser.ParsePatchset(patchsetData)
	require.Empty(t, errs)
	require.Len(t, patchset.Files, 2)

	assert.Equal(t, "alpha.txt", patchset.Files[0].Diff.ToFile)
	assert.Equal(t, "beta.txt", patchset.Files[1].Diff.ToFile)
	assert.Contains(t, string(patchset.Files[0].Patch), "diff --git a/alpha.txt b/alpha.txt")
	assert.Contains(t, string(patchset.Files[1].Patch), "diff --git a/beta.txt b/beta.txt")
}

func TestPatchsetApply_MultipleFiles(t *testing.T) {
	t.Parallel()

	original := map[string][]byte{
		"alpha.txt": []byte("alpha\none\n"),
		"beta.txt":  []byte("beta\none\n"),
		"keep.txt":  []byte("unchanged\n"),
	}

	patchA := buildPatch(t, "alpha.txt", original["alpha.txt"], []byte("alpha\ntwo\n"))
	patchB := buildPatch(t, "beta.txt", original["beta.txt"], []byte("beta\ntwo\n"))
	patchsetData := append(append([]byte{}, patchA...), patchB...)

	applied, err := git_diff_parser.ApplyPatchset(original, patchsetData)
	require.NoError(t, err)

	assert.Equal(t, []byte("alpha\ntwo\n"), applied["alpha.txt"])
	assert.Equal(t, []byte("beta\ntwo\n"), applied["beta.txt"])
	assert.Equal(t, []byte("unchanged\n"), applied["keep.txt"])
	assert.Equal(t, []byte("alpha\none\n"), original["alpha.txt"])
	assert.Equal(t, []byte("beta\none\n"), original["beta.txt"])
}

func TestPatchsetApply_RejectsUnsupportedOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		patch       []byte
		tree        map[string][]byte
		wantIs      error
		wantMessage string
	}{
		{
			name:        "create",
			patch:       mustReadFile(t, filepath.Join("testdata", "significant", "add.diff")),
			tree:        map[string][]byte{},
			wantIs:      git_diff_parser.ErrPatchCreate,
			wantMessage: "patch creates are not supported",
		},
		{
			name:        "delete",
			patch:       mustReadFile(t, filepath.Join("testdata", "significant", "rm.diff")),
			tree:        map[string][]byte{"a.txt": []byte("a\n")},
			wantIs:      git_diff_parser.ErrPatchDelete,
			wantMessage: "patch deletes are not supported",
		},
		{
			name:        "rename",
			patch:       mustReadFile(t, filepath.Join("testdata", "significant", "mv.diff")),
			tree:        map[string][]byte{"b.txt": []byte("b\n")},
			wantIs:      git_diff_parser.ErrPatchRename,
			wantMessage: "patch renames are not supported",
		},
		{
			name: "mode change",
			patch: []byte(`diff --git a/mode.go b/mode.go
old mode 100644
new mode 100755
--- a/mode.go
+++ b/mode.go
@@ -1 +1 @@
-package mode
+package mode
`),
			tree:        map[string][]byte{"mode.go": []byte("package mode\n")},
			wantIs:      git_diff_parser.ErrPatchModeChange,
			wantMessage: "patch mode changes are not supported",
		},
		{
			name:        "binary",
			patch:       mustReadFile(t, filepath.Join("testdata", "significant", "binary-delta.diff")),
			tree:        map[string][]byte{"favicon-16x16-light.png": []byte("binary")},
			wantIs:      git_diff_parser.ErrPatchBinary,
			wantMessage: "binary patches are not supported",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := git_diff_parser.ApplyPatchset(test.tree, test.patch)
			require.Error(t, err)
			assert.ErrorIs(t, err, test.wantIs)
			assert.Contains(t, err.Error(), test.wantMessage)

			var unsupportedErr *git_diff_parser.UnsupportedPatchError
			require.ErrorAs(t, err, &unsupportedErr)
		})
	}
}
