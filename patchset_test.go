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

func TestParsePatchOperations(t *testing.T) {
	t.Parallel()

	patchData := []byte(`diff --git a/a.go b/a.go
deleted file mode 100644
index 1111111..0000000
--- a/a.go
+++ /dev/null
@@ -1 +0,0 @@
-content a
diff --git a/c.go b/c.go
new file mode 100755
index 0000000..3333333
--- /dev/null
+++ b/c.go
@@ -0,0 +1 @@
+content c
diff --git a/e.go b/f.go
similarity index 100%
rename from e.go
rename to f.go
diff --git a/mode.go b/mode.go
old mode 100644
new mode 100755
--- a/mode.go
+++ b/mode.go
`)

	operations, err := ParsePatchOperations(patchData)
	require.NoError(t, err)
	require.Len(t, operations, 4)

	assert.Equal(t, PatchOperationTypeDelete, operations[0].Type)
	assert.Equal(t, "a.go", operations[0].SourcePath)
	assert.Empty(t, operations[0].TargetPath)
	assert.True(t, operations[0].MutatesFileSet())

	assert.Equal(t, PatchOperationTypeCreate, operations[1].Type)
	assert.Empty(t, operations[1].SourcePath)
	assert.Equal(t, "c.go", operations[1].TargetPath)
	assert.Equal(t, "100755", operations[1].NewMode)
	assert.True(t, operations[1].MutatesFileSet())

	assert.Equal(t, PatchOperationTypeRename, operations[2].Type)
	assert.Equal(t, "e.go", operations[2].SourcePath)
	assert.Equal(t, "f.go", operations[2].TargetPath)
	assert.True(t, operations[2].MutatesFileSet())

	assert.Equal(t, PatchOperationTypeModeChange, operations[3].Type)
	assert.Equal(t, "mode.go", operations[3].SourcePath)
	assert.Equal(t, "mode.go", operations[3].TargetPath)
	assert.False(t, operations[3].MutatesFileSet())
}

func TestApplyPatchOperations(t *testing.T) {
	t.Parallel()

	patchData := []byte(`diff --git a/a.go b/a.go
deleted file mode 100644
index 1111111..0000000
--- a/a.go
+++ /dev/null
@@ -1 +0,0 @@
-content a
diff --git a/c.go b/c.go
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/c.go
@@ -0,0 +1 @@
+content c
diff --git a/e.go b/f.go
similarity index 100%
rename from e.go
rename to f.go
`)
	tree := map[string][]byte{
		"a.go": []byte("content a\n"),
		"e.go": []byte("content e\n"),
	}
	original := cloneTestTree(tree)

	operations, err := ParsePatchOperations(patchData)
	require.NoError(t, err)

	applied, err := ApplyPatchOperations(tree, operations)
	require.NoError(t, err)
	assert.Equal(t, map[string][]byte{
		"c.go": []byte("content c\n"),
		"f.go": []byte("content e\n"),
	}, applied)
	assert.Equal(t, original, tree)
}

func TestParsePatchOperations_ClassifiesBinaryPatch(t *testing.T) {
	t.Parallel()

	operations, err := ParsePatchOperations(mustReadFile(t, filepath.Join("testdata", "significant", "binary-delta.diff")))
	require.NoError(t, err)
	require.Len(t, operations, 1)
	assert.Equal(t, PatchOperationTypeBinary, operations[0].Type)
	assert.True(t, operations[0].IsBinary)
}

func TestParsePatchOperations_RejectsGitApplyInvalidHeaderCombinations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		patch   []byte
		wantErr string
	}{
		{
			name: "create and copy",
			patch: []byte(`diff --git a/1 b/2
new file mode 100644
copy from 1
copy to 2
`),
			wantErr: "create and copy cannot be combined",
		},
		{
			name: "create and rename",
			patch: []byte(`diff --git a/1 b/2
new file mode 100644
rename from 1
rename to 2
`),
			wantErr: "create and rename cannot be combined",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParsePatchOperations(test.patch)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErr)
		})
	}
}

func TestParsePatchOperations_RejectsGitApplyInconsistentFilenames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		patch   []byte
		wantErr string
	}{
		{
			name: "inconsistent new filename",
			patch: []byte(`diff --git a/f b/f
new file mode 100644
index 0000000..d00491f
--- /dev/null
+++ b/f-blah
@@ -0,0 +1 @@
+1
`),
			wantErr: "inconsistent new filename",
		},
		{
			name: "inconsistent old filename",
			patch: []byte(`diff --git a/f b/f
deleted file mode 100644
index d00491f..0000000
--- b/f-blah
+++ /dev/null
@@ -1 +0,0 @@
-1
`),
			wantErr: "inconsistent old filename",
		},
		{
			name: "missing new filename",
			patch: []byte(`diff --git a/f b/f
index 0000000..d00491f
--- a/f
@@ -0,0 +1 @@
+1
`),
			wantErr: "lacks new filename information",
		},
		{
			name: "missing old filename",
			patch: []byte(`diff --git a/f b/f
index d00491f..0000000
+++ b/f
@@ -1 +0,0 @@
-1
`),
			wantErr: "lacks old filename information",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParsePatchOperations(test.patch)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErr)
		})
	}
}

func TestParsePatchOperations_AcceptsQuotedFilenamesWithSpaces(t *testing.T) {
	t.Parallel()

	operations, err := ParsePatchOperations([]byte(`diff --git "a/foo bar.txt" "b/foo bar.txt"
--- "a/foo bar.txt"
+++ "b/foo bar.txt"
@@ -1 +1 @@
-old
+new
`))
	require.NoError(t, err)
	require.Len(t, operations, 1)
	assert.Equal(t, "foo bar.txt", operations[0].SourcePath)
	assert.Equal(t, "foo bar.txt", operations[0].TargetPath)
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

func TestPatchsetApply_SameFilenameSequentialDiffs(t *testing.T) {
	t.Parallel()

	patchData := []byte(`diff --git a/same_fn b/same_fn
--- a/same_fn
+++ b/same_fn
@@ -1,13 +1,13 @@
 a
 b
 c
-d
+z
 e
 f
 g
 h
 i
 j
 k
 l
 m
diff --git a/same_fn b/same_fn
--- a/same_fn
+++ b/same_fn
@@ -1,13 +1,13 @@
 a
 b
 c
 z
-e
+y
 f
 g
 h
 i
 j
 k
 l
 m
`)

	tree := map[string][]byte{
		"same_fn": []byte("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\n"),
	}

	applied, err := applyPatchset(tree, patchData)
	require.NoError(t, err)
	assert.Equal(t, map[string][]byte{
		"same_fn": []byte("a\nb\nc\nz\ny\nf\ng\nh\ni\nj\nk\nl\nm\n"),
	}, applied)
}

func TestPatchsetApply_SameFilenameIndependentDiffs(t *testing.T) {
	t.Parallel()

	patchData := []byte(`diff --git a/same_fn b/same_fn
--- a/same_fn
+++ b/same_fn
@@ -1,13 +1,13 @@
 a
 b
 c
-d
+z
 e
 f
 g
 h
 i
 j
 k
 l
 m
diff --git a/same_fn b/same_fn
--- a/same_fn
+++ b/same_fn
@@ -6,8 +6,8 @@ f
 g
 h
-i
+y
 j
 k
 l
 m
`)

	tree := map[string][]byte{
		"same_fn": []byte("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\n"),
	}

	applied, err := applyPatchset(tree, patchData)
	require.NoError(t, err)
	assert.Equal(t, map[string][]byte{
		"same_fn": []byte("a\nb\nc\nz\ne\nf\ng\nh\ny\nj\nk\nl\nm\n"),
	}, applied)
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
