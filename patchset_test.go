package git_diff_parser

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePatchset(t *testing.T) {
	t.Parallel()

	patchA := buildPatch(t, "alpha.txt", []byte("alpha\none\n"), []byte("alpha\ntwo\n"))
	patchB := buildPatch(t, "beta.txt", []byte("beta\none\n"), []byte("beta\ntwo\n"))
	patchsetData := append(append([]byte{}, patchA...), patchB...)

	patchset, errs := parsePatchset(patchsetData)
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

	applied, err := applyPatchset(original, patchsetData)
	require.NoError(t, err)

	assert.Equal(t, []byte("alpha\ntwo\n"), applied["alpha.txt"])
	assert.Equal(t, []byte("beta\ntwo\n"), applied["beta.txt"])
	assert.Equal(t, []byte("unchanged\n"), applied["keep.txt"])
	assert.Equal(t, []byte("alpha\none\n"), original["alpha.txt"])
	assert.Equal(t, []byte("beta\none\n"), original["beta.txt"])
}

func TestPatchsetApply_TextTreeOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		patch    []byte
		tree     map[string][]byte
		wantTree map[string][]byte
	}{
		{
			name:     "create",
			patch:    mustReadFile(t, filepath.Join("testdata", "significant", "add.diff")),
			tree:     map[string][]byte{},
			wantTree: map[string][]byte{"a.txt": []byte("a\n")},
		},
		{
			name:     "delete",
			patch:    mustReadFile(t, filepath.Join("testdata", "significant", "rm.diff")),
			tree:     map[string][]byte{"a.txt": []byte("a\n")},
			wantTree: map[string][]byte{},
		},
		{
			name: "rename",
			patch: []byte(`diff --git a/src.txt b/dst.txt
similarity index 100%
rename from src.txt
rename to dst.txt
index 1234567..89abcde 100644
--- a/src.txt
+++ b/dst.txt
@@ -1,2 +1,2 @@
-alpha
+bravo
 charlie
`),
			tree:     map[string][]byte{"src.txt": []byte("alpha\ncharlie\n")},
			wantTree: map[string][]byte{"dst.txt": []byte("bravo\ncharlie\n")},
		},
		{
			name: "copy",
			patch: []byte(`diff --git a/src.txt b/dst.txt
similarity index 100%
copy from src.txt
copy to dst.txt
index 1234567..89abcde 100644
--- a/src.txt
+++ b/dst.txt
@@ -1,2 +1,3 @@
 alpha
+bravo
 charlie
`),
			tree: map[string][]byte{"src.txt": []byte("alpha\ncharlie\n")},
			wantTree: map[string][]byte{
				"src.txt": []byte("alpha\ncharlie\n"),
				"dst.txt": []byte("alpha\nbravo\ncharlie\n"),
			},
		},
		{
			name: "mode change",
			patch: []byte(`diff --git a/mode.go b/mode.go
index 1234567..89abcde 100755
old mode 100644
new mode 100755
--- a/mode.go
+++ b/mode.go
`),
			tree:     map[string][]byte{"mode.go": []byte("package mode\n")},
			wantTree: map[string][]byte{"mode.go": []byte("package mode\n")},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			original := cloneTestTree(test.tree)
			applied, err := applyPatchset(test.tree, test.patch)
			require.NoError(t, err)
			assert.Equal(t, test.wantTree, applied)
			assert.Equal(t, original, test.tree)
		})
	}
}

func TestPatchsetApply_AtomicOnFailure(t *testing.T) {
	t.Parallel()

	renamePatch := []byte(`diff --git a/src.txt b/dst.txt
similarity index 100%
rename from src.txt
rename to dst.txt
--- a/src.txt
+++ b/dst.txt
@@ -1,2 +1,2 @@
-alpha
+bravo
 charlie
`)
	deletePatch := mustReadFile(t, filepath.Join("testdata", "significant", "rm.diff"))
	patchsetData := append(append([]byte{}, renamePatch...), deletePatch...)

	tree := map[string][]byte{
		"src.txt":  []byte("alpha\ncharlie\n"),
		"keep.txt": []byte("keep\n"),
	}
	original := cloneTestTree(tree)

	applied, err := applyPatchset(tree, patchsetData)
	require.Error(t, err)
	assert.Nil(t, applied)
	assert.Equal(t, original, tree)
	assert.Contains(t, err.Error(), "missing file")
}

func TestPatchsetApply_RejectsBinaryPatches(t *testing.T) {
	t.Parallel()

	_, err := applyPatchset(
		map[string][]byte{"favicon-16x16-light.png": []byte("binary")},
		mustReadFile(t, filepath.Join("testdata", "significant", "binary-delta.diff")),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary patches are not supported")

	var unsupportedErr *unsupportedPatchError
	require.ErrorAs(t, err, &unsupportedErr)
	assert.ErrorIs(t, err, errPatchBinary)
}

func cloneTestTree(tree map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(tree))
	for path, content := range tree {
		out[path] = append([]byte(nil), content...)
	}
	return out
}
