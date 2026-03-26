package git_diff_parser

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type applyFixtureFiles struct {
	src   []byte
	patch []byte
	out   []byte
}

const (
	defaultCurrentConflictMarker  = "<<<<<<< Current"
	defaultIncomingConflictMarker = ">>>>>>> Incoming patch"
)

func TestApplyFile_TextFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  string
		wantErr  string
		conflict bool
	}{
		{name: "new file", fixture: "text_fragment_new"},
		{name: "add start", fixture: "text_fragment_add_start"},
		{name: "add middle", fixture: "text_fragment_add_middle"},
		{name: "add end", fixture: "text_fragment_add_end"},
		{name: "add end no eof", fixture: "text_fragment_add_end_noeol"},
		{name: "change start", fixture: "text_fragment_change_start"},
		{name: "change middle", fixture: "text_fragment_change_middle"},
		{name: "change end", fixture: "text_fragment_change_end"},
		{name: "change end eol", fixture: "text_fragment_change_end_eol"},
		{name: "change exact", fixture: "text_fragment_change_exact"},
		{name: "change single no eof", fixture: "text_fragment_change_single_noeol"},
		{name: "delete all", fixture: "text_fragment_delete_all"},
		{name: "short src before", fixture: "text_fragment_error_short_src_before", wantErr: "patch conflict", conflict: true},
		{name: "short src", fixture: "text_fragment_error_short_src", wantErr: "patch conflict", conflict: true},
		{name: "context conflict", fixture: "text_fragment_error_context_conflict", wantErr: "patch conflict", conflict: true},
		{name: "delete conflict", fixture: "text_fragment_error_delete_conflict", wantErr: "patch conflict", conflict: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			files := loadApplyFixture(t, test.fixture)
			applied, err := ApplyFile(files.src, files.patch)

			if test.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.wantErr)
				if test.conflict {
					var applyErr *applyError
					require.ErrorAs(t, err, &applyErr)
					assert.True(t, errors.Is(err, ErrPatchConflict))
					assert.Contains(t, string(applied), defaultCurrentConflictMarker)
					assert.Contains(t, string(applied), defaultIncomingConflictMarker)
				}
				return
			}

			require.NoError(t, err)
			assert.True(t, bytes.Equal(expectedApplyFixtureOutput(t, files), applied))
		})
	}
}

func TestApplyFile_RejectsUnsupportedFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		patch   []byte
		wantErr string
	}{
		{
			name: "rename patch",
			patch: []byte(`diff --git a/sdk.go b/custom/sdk.go
similarity index 100%
rename from sdk.go
rename to custom/sdk.go
`),
			wantErr: "patch contains no hunks",
		},
		{
			name: "mode only patch",
			patch: []byte(`diff --git a/sdk.go b/sdk.go
old mode 100644
new mode 100755
`),
			wantErr: "file mode changes are not supported",
		},
		{
			name: "binary patch",
			patch: []byte(`diff --git a/sdk.go b/sdk.go
GIT binary patch
literal 3
abc
`),
			wantErr: "binary patches are not supported",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := ApplyFile([]byte("package testsdk\n"), test.patch)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErr)
		})
	}
}

func TestApplyFile_RejectsHeaderOnlyAndNoOpPatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		patch   []byte
		wantErr string
	}{
		{
			name: "header only",
			patch: []byte(`diff --git a/sdk.go b/sdk.go
--- a/sdk.go
+++ b/sdk.go
`),
			wantErr: "patch contains no hunks",
		},
		{
			name: "no op hunk",
			patch: []byte(`diff --git a/sdk.go b/sdk.go
--- a/sdk.go
+++ b/sdk.go
@@ -1,1 +1,1 @@
 package testsdk
`),
			wantErr: "patch contains no effective changes",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := ApplyFile([]byte("package testsdk\n"), test.patch)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErr)
		})
	}
}

func TestApplyFile_NoNewlineMatrix(t *testing.T) {
	t.Parallel()

	files := []struct {
		name    string
		content []byte
	}{
		{name: "0", content: []byte("a\nb\n")},
		{name: "1", content: []byte("a\nb\nc\n")},
		{name: "2", content: []byte("a\nb")},
		{name: "3", content: []byte("a\nc\nb")},
	}

	for i := range files {
		for j := range files {
			if i == j {
				continue
			}

			from := files[i]
			to := files[j]
			name := from.name + " to " + to.name

			t.Run(name, func(t *testing.T) {
				t.Parallel()

				patch := mustReadFile(t, filepath.Join("testdata", "apply", "t4101", "diff."+from.name+"-"+to.name))
				applied, err := ApplyFile(from.content, patch)
				require.NoError(t, err)
				assert.Equal(t, to.content, applied)
			})
		}
	}
}

func TestApplyFile_BoundaryCases(t *testing.T) {
	t.Parallel()

	original := []byte("b\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\nq\nr\ns\nt\nu\nv\nw\nx\ny\n")
	tests := []struct {
		name string
		want []byte
	}{
		{name: "add head", want: []byte("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\nq\nr\ns\nt\nu\nv\nw\nx\ny\n")},
		{name: "insert second", want: []byte("b\na\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\nq\nr\ns\nt\nu\nv\nw\nx\ny\n")},
		{name: "modify head", want: []byte("a\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\nq\nr\ns\nt\nu\nv\nw\nx\ny\n")},
		{name: "delete head", want: []byte("c\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\nq\nr\ns\nt\nu\nv\nw\nx\ny\n")},
		{name: "add tail", want: []byte("b\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\nq\nr\ns\nt\nu\nv\nw\nx\ny\nz\n")},
		{name: "modify tail", want: []byte("b\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\nq\nr\ns\nt\nu\nv\nw\nx\nz\n")},
		{name: "delete tail", want: []byte("b\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\nq\nr\ns\nt\nu\nv\nw\nx\n")},
	}

	for _, context := range []int{3, 0} {
		context := context
		for _, test := range tests {
			test := test
			t.Run(test.name+" context "+contextLabel(context), func(t *testing.T) {
				t.Parallel()

				patch := buildPatchWithContext(t, "victim", original, test.want, context)
				applied, err := ApplyFile(original, patch)
				require.NoError(t, err)
				assert.Equal(t, test.want, applied)
			})
		}
	}
}

func TestApplyFile_OffsetPatches(t *testing.T) {
	t.Parallel()

	original := []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n")
	target := []byte("1\n2\n3\n4\n5\n6\n7\na\nb\nc\nd\ne\n8\n9\n10\n11\n12\n")
	basePatch := buildPatchWithContext(t, "file", original, target, 3)

	tests := []struct {
		name   string
		header string
	}{
		{name: "unmodified patch", header: "@@ -5,6 +5,11 @@"},
		{name: "minus offset", header: "@@ -2,6 +2,11 @@"},
		{name: "plus offset", header: "@@ -7,6 +7,11 @@"},
		{name: "big offset", header: "@@ -19,6 +19,11 @@"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			patch := rewriteFirstHunkHeader(basePatch, test.header)
			applied, err := ApplyFile(original, patch)
			require.NoError(t, err)
			assert.Equal(t, target, applied)
		})
	}
}

func TestApplyFile_DamagedContextPatchesConflictWithoutFuzz(t *testing.T) {
	t.Parallel()

	original := []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n")
	target := []byte("1\n2\n3\n4\n5\n6\n7\na\nb\nc\nd\ne\n8\n9\n10\n11\n12\n")
	basePatch := buildPatchWithContext(t, "file", original, target, 3)
	damaged := bytes.Replace(basePatch, []byte("\n 5\n"), []byte("\n S\n"), 1)

	tests := []struct {
		name   string
		header string
	}{
		{name: "no offset", header: "@@ -5,6 +5,11 @@"},
		{name: "minus offset", header: "@@ -2,6 +2,11 @@"},
		{name: "plus offset", header: "@@ -7,6 +7,11 @@"},
		{name: "big offset", header: "@@ -19,6 +19,11 @@"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			patch := rewriteFirstHunkHeader(damaged, test.header)
			applied, err := ApplyFile(original, patch)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrPatchConflict)
			assert.Contains(t, string(applied), defaultCurrentConflictMarker)
		})
	}
}

func TestApplyFile_EmptyContextPatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		original []byte
		target   []byte
	}{
		{
			name:     "delete blank-lined middle line",
			original: []byte("\n\nA\nB\nC\n\n"),
			target:   []byte("\n\nA\nC\n\n"),
		},
		{
			name:     "insert middle",
			original: []byte("alpha\ncharlie\n"),
			target:   []byte("alpha\nbravo\ncharlie\n"),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			patch := buildPatchWithContext(t, "file", test.original, test.target, 0)
			applied, err := ApplyFile(test.original, patch)
			require.NoError(t, err)
			assert.Equal(t, test.target, applied)
		})
	}
}

func TestApplyFile_EmptyContextNoTrailingNewlinePatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		original []byte
		target   []byte
		patch    []byte
	}{
		{
			name:     "append no newline tail",
			original: []byte("\n\nA\nC\n\n"),
			target:   []byte("\n\nA\nC\n\nQ"),
			patch: []byte(`diff --git a/file b/file
--- a/file
+++ b/file
@@ -6,0 +7 @@
+Q
\ No newline at end of file
`),
		},
		{
			name:     "modify tail no newline",
			original: []byte("alpha\nbravo"),
			target:   []byte("alpha\ncharlie"),
			patch: []byte(`diff --git a/file b/file
--- a/file
+++ b/file
@@ -2 +2 @@
-bravo
\ No newline at end of file
+charlie
\ No newline at end of file
`),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			applied, err := ApplyFile(test.original, test.patch)
			require.NoError(t, err)
			assert.Equal(t, test.target, applied)
		})
	}
}

func TestApplyFile_RelocatesHunkWhenContextStillMatches(t *testing.T) {
	t.Parallel()

	originalPristine := []byte("package testsdk\n\ntype Status struct{}\n")
	patchData := buildPatch(t, "status.go", originalPristine, []byte("package testsdk\n\ntype Status struct{}\n\nfunc (s *Status) String() string {\n\treturn \"custom\"\n}\n"))
	shiftedPristine := []byte("package testsdk\n\n// generated comment moved the hunk down\n\ntype Status struct{}\n")

	applied, err := ApplyFile(shiftedPristine, patchData)
	require.NoError(t, err)
	assert.Equal(t, []byte("package testsdk\n\n// generated comment moved the hunk down\n\ntype Status struct{}\n\nfunc (s *Status) String() string {\n\treturn \"custom\"\n}\n"), applied)
}

func TestApplyFile_RelocatesToNearestMatchingBlock(t *testing.T) {
	t.Parallel()

	original := []byte("header\nanchor\ncommon\nvalue-old\nend\ngap\nanchor\ncommon\nvalue-old\nend\n")
	target := []byte("header\nanchor\ncommon\nvalue-old\nend\ngap\nanchor\ncommon\nvalue-new\nend\n")
	shifted := []byte("header\nanchor\ncommon\nvalue-old\nend\ngap\nextra\nanchor\ncommon\nvalue-old\nend\n")

	patch := buildPatchWithContext(t, "dup.txt", original, target, 1)
	applied, err := ApplyFile(shifted, patch)
	require.NoError(t, err)
	assert.Equal(t, []byte("header\nanchor\ncommon\nvalue-old\nend\ngap\nextra\nanchor\ncommon\nvalue-new\nend\n"), applied)
}

func TestApplyFile_MultipleHunks(t *testing.T) {
	t.Parallel()

	original := []byte("line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\n")
	target := []byte("line 1\nline two\nline 3\nline 4\nline 5\nline six\nline 7\nline 8\n")

	patch := buildPatchWithContext(t, "multi.txt", original, target, 1)
	applied, err := ApplyFile(original, patch)
	require.NoError(t, err)
	assert.Equal(t, target, applied)
}

func TestApplyFile_MultipleHunksOneConflict(t *testing.T) {
	t.Parallel()

	original := []byte("line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\n")
	target := []byte("line 1\nline two\nline 3\nline 4\nline 5\nline six\nline 7\nline 8\n")
	current := []byte("line 1\nline 2\nline 3\nline 4\nline 5\nline VI\nline 7\nline 8\n")

	patch := buildPatchWithContext(t, "multi.txt", original, target, 1)
	applied, err := ApplyFile(current, patch)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPatchConflict)
	assert.Contains(t, string(applied), "line two")
	assert.Contains(t, string(applied), defaultCurrentConflictMarker)
	assert.Contains(t, string(applied), "line VI")
	assert.Contains(t, string(applied), "line six")
}

func TestApplyFile_ReturnsConflictMarkers(t *testing.T) {
	t.Parallel()

	base := []byte("package testsdk\n\ntype Status struct{}\n")
	current := []byte("package testsdk\n\ntype Status struct {\n\tValue string\n}\n")
	patchData := buildPatch(t, "status.go", base, []byte("package testsdk\n\ntype Status struct{}\n\nfunc (s *Status) String() string {\n\treturn \"custom\"\n}\n"))

	applied, err := ApplyFile(current, patchData)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPatchConflict)
	assert.True(t, errors.Is(err, ErrPatchConflict))
	assert.Contains(t, string(applied), defaultCurrentConflictMarker)
	assert.Contains(t, string(applied), defaultIncomingConflictMarker)
	assert.Contains(t, string(applied), "func (s *Status) String() string")
}

func TestApplyFileWithOptions_RendersNeutralConflictMarkers(t *testing.T) {
	t.Parallel()

	base := []byte("package testsdk\n\ntype Status struct{}\n")
	current := []byte("package testsdk\n\ntype Status struct {\n\tValue string\n}\n")
	patchData := buildPatch(t, "status.go", base, []byte("package testsdk\n\ntype Status struct{}\n\nfunc (s *Status) String() string {\n\treturn \"custom\"\n}\n"))

	result, err := applyFileWithOptions(current, patchData, applyOptions{
		Mode: applyModeMerge,
	})
	require.Error(t, err)
	var applyErr *applyError
	require.ErrorAs(t, err, &applyErr)
	assert.Equal(t, 0, result.DirectMisses)
	assert.Equal(t, 1, result.MergeConflicts)
	assert.Contains(t, string(result.Content), "<<<<<<<")
	assert.NotContains(t, string(result.Content), "Current (Your changes)")
	assert.NotContains(t, string(result.Content), "Generated by Speakeasy")
	assert.NotEmpty(t, result.Reject)
	assert.True(t, strings.HasPrefix(string(result.Reject), "diff a/status.go b/status.go\t(rejected hunks)\n@@"))
}

func TestApplyFileWithOptions_DirectModeReportsMissesWithoutMarkers(t *testing.T) {
	t.Parallel()

	base := []byte("package testsdk\n\ntype Status struct{}\n")
	current := []byte("package testsdk\n\ntype Status struct {\n\tValue string\n}\n")
	patchData := buildPatch(t, "status.go", base, []byte("package testsdk\n\ntype Status struct{}\n\nfunc (s *Status) String() string {\n\treturn \"custom\"\n}\n"))

	result, err := applyFileWithOptions(current, patchData, applyOptions{
		Mode: applyModeApply,
	})
	require.Error(t, err)
	var applyErr *applyError
	require.ErrorAs(t, err, &applyErr)
	assert.Equal(t, 1, result.DirectMisses)
	assert.Equal(t, 0, result.MergeConflicts)
	assert.Equal(t, current, result.Content)
	assert.NotContains(t, string(result.Content), "<<<<<<<")
	assert.NotContains(t, string(result.Content), ">>>>>>>")
	assert.NotEmpty(t, result.Reject)
	assert.True(t, strings.HasPrefix(string(result.Reject), "diff a/status.go b/status.go\t(rejected hunks)\n@@"))
}

func TestPatchApply_AllowsCustomConflictLabels(t *testing.T) {
	t.Parallel()

	base := []byte("package testsdk\n\ntype Status struct{}\n")
	current := []byte("package testsdk\n\ntype Status struct {\n\tValue string\n}\n")
	patchData := buildPatch(t, "status.go", base, []byte("package testsdk\n\ntype Status struct{}\n\nfunc (s *Status) String() string {\n\treturn \"custom\"\n}\n"))

	applier := newPatchApply(applyOptions{
		Mode: applyModeMerge,
		ConflictLabels: conflictLabels{
			Current:  "Current (Your changes)",
			Incoming: "New (Generated by Speakeasy)",
		},
	})

	applied, err := applier.applyFile(current, patchData)
	require.Error(t, err)
	assert.Contains(t, string(applied), "<<<<<<< Current (Your changes)")
	assert.Contains(t, string(applied), ">>>>>>> New (Generated by Speakeasy)")
}

func TestApplyFileWithOptions_IgnoreWhitespaceAppliesThroughContextDrift(t *testing.T) {
	t.Parallel()

	original := []byte("alpha\n    beta\ncharlie\n")
	target := []byte("alpha\n    BETA\ncharlie\n")
	patchData := buildPatchWithContext(t, "whitespace.txt", original, target, 1)
	current := []byte("alpha\n  beta\ncharlie\n")

	_, err := applyFileWithOptions(current, patchData, applyOptions{
		Mode: applyModeMerge,
	})
	require.Error(t, err)

	applied, err := applyFileWithOptions(current, patchData, applyOptions{
		Mode:             applyModeMerge,
		IgnoreWhitespace: true,
	})
	require.NoError(t, err)
	assert.Equal(t, target, applied.Content)
	assert.Equal(t, 0, applied.DirectMisses)
	assert.Equal(t, 0, applied.MergeConflicts)
}

func TestApplyFileWithOptions_ReverseAppliesPatchBackwards(t *testing.T) {
	t.Parallel()

	current := []byte("z\nb\n")
	patchData := []byte(`diff --git a/file.txt b/file.txt
--- a/file.txt
+++ b/file.txt
@@ -1,2 +1,2 @@
-a
+z
 b
`)

	applied, err := applyFileWithOptions(current, patchData, applyOptions{
		Reverse: true,
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("a\nb\n"), applied.Content)
}

func TestApplyFileWithOptions_UnidiffZeroIsAccepted(t *testing.T) {
	t.Parallel()

	current := []byte("alpha\nbeta\ngamma\n")
	patchData := []byte(`diff --git a/file.txt b/file.txt
--- a/file.txt
+++ b/file.txt
@@ -2 +1,0 @@
-beta
`)

	baseline, err := applyFileWithOptions(current, patchData, applyOptions{})
	require.NoError(t, err)
	assert.Equal(t, []byte("alpha\ngamma\n"), baseline.Content)

	applied, err := applyFileWithOptions(current, patchData, applyOptions{
		UnidiffZero: true,
	})
	require.NoError(t, err)
	assert.Equal(t, baseline.Content, applied.Content)
}

func TestApplyFileWithOptions_RecountRebuildsHunkCounts(t *testing.T) {
	t.Parallel()

	current := []byte("alpha\nbeta\ngamma\n")
	patchData := []byte(`diff --git a/file.txt b/file.txt
--- a/file.txt
+++ b/file.txt
@@ -2,2 +2,2 @@
-beta
`)

	_, err := applyFileWithOptions(current, patchData, applyOptions{
		UnidiffZero: true,
	})
	require.Error(t, err)

	applied, err := applyFileWithOptions(current, patchData, applyOptions{
		UnidiffZero: true,
		Recount:     true,
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("alpha\ngamma\n"), applied.Content)
}

func TestApplyFile_RejectsAlreadyAppliedBeginningAndEndingPatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current []byte
		patch   []byte
	}{
		{
			name:    "ending patch",
			current: []byte("a\nb\nc\n"),
			patch: []byte(`diff --git a/file b/file
--- a/file
+++ b/file
@@ -1,2 +1,3 @@
 a
 b
+c
`),
		},
		{
			name:    "beginning patch",
			current: []byte("a\nb\nc\n"),
			patch: []byte(`diff --git a/file b/file
--- a/file
+++ b/file
@@ -1,2 +1,3 @@
+a
 b
 c
`),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			applied, err := ApplyFile(test.current, test.patch)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrPatchConflict)
			assert.Contains(t, string(applied), defaultCurrentConflictMarker)
		})
	}
}

func TestApplyFile_RejectsAlreadyAppliedMiddlePatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current []byte
		patch   []byte
	}{
		{
			name:    "middle insertion",
			current: []byte("start\nmiddle\ninserted\nend\n"),
			patch: []byte(`diff --git a/file b/file
--- a/file
+++ b/file
@@ -1,3 +1,4 @@
 start
 middle
+inserted
 end
`),
		},
		{
			name:    "replacement already applied",
			current: []byte("start\nnew value\nend\n"),
			patch: []byte(`diff --git a/file b/file
--- a/file
+++ b/file
@@ -1,3 +1,3 @@
 start
-old value
+new value
 end
`),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			applied, err := ApplyFile(test.current, test.patch)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrPatchConflict)
			assert.Contains(t, string(applied), defaultCurrentConflictMarker)
		})
	}
}

func TestApplyFile_RejectsMultiFileDiff(t *testing.T) {
	t.Parallel()

	patchData := []byte(`diff --git a/sdk.go b/sdk.go
--- a/sdk.go
+++ b/sdk.go
@@ -1,3 +1,4 @@
 package testsdk

+// sdk custom
 type SDK struct{}
diff --git a/models/components/pet.go b/models/components/pet.go
--- a/models/components/pet.go
+++ b/models/components/pet.go
@@ -1,3 +1,4 @@
 package components

+// pet custom
 type Pet struct{}
`)

	_, err := ApplyFile([]byte("package testsdk\n\ntype SDK struct{}\n"), patchData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected exactly 1 file diff")
}

func TestApplyFile_RejectsMalformedPatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		patch   []byte
		wantErr string
	}{
		{
			name: "non patch input",
			patch: []byte(`I am not a patch
I look nothing like a patch
git apply must fail
`),
			wantErr: "unsupported patch syntax",
		},
		{
			name: "invalid hunk header",
			patch: []byte(`diff --git a/file b/file
--- a/file
+++ b/file
@@ -x,1 +1,1 @@
-a
+b
`),
			wantErr: "unsupported patch syntax",
		},
		{
			name: "unexpected hunk line prefix",
			patch: []byte(`diff --git a/file b/file
--- a/file
+++ b/file
@@ -1,1 +1,1 @@
!a
`),
			wantErr: "unexpected hunk line",
		},
		{
			name: "no newline marker without preceding line",
			patch: []byte(`diff --git a/file b/file
--- a/file
+++ b/file
@@ -1,1 +1,1 @@
\ No newline at end of file
`),
			wantErr: "unexpected no-newline marker without a preceding patch line",
		},
		{
			name: "unsupported header garbage",
			patch: []byte(`diff --git a/file b/file
copy from file
copy to file-copy
--- a/file
+++ b/file-copy
@@ -1,1 +1,1 @@
-a
+b
`),
			wantErr: "unsupported patch syntax",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := ApplyFile([]byte("a\n"), test.patch)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErr)
		})
	}
}

func TestApplyFile_RejectsAdditionalUnsupportedPatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		patch   []byte
		wantErr string
	}{
		{
			name: "new file mode with hunk",
			patch: []byte(`diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,2 @@
+package test
+
`),
			wantErr: "file mode changes are not supported",
		},
		{
			name: "deleted file mode with hunk",
			patch: []byte(`diff --git a/old.go b/old.go
deleted file mode 100644
--- a/old.go
+++ /dev/null
@@ -1,1 +0,0 @@
-package test
`),
			wantErr: "patches may only modify existing files",
		},
		{
			name: "binary files differ",
			patch: []byte(`diff --git a/file.bin b/file.bin
Binary files a/file.bin and b/file.bin differ
`),
			wantErr: "patch contains no hunks",
		},
		{
			name: "create and rename",
			patch: []byte(`diff --git a/1 b/2
new file mode 100644
rename from 1
rename to 2
`),
			wantErr: "file mode changes are not supported",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := ApplyFile([]byte("package test\n"), test.patch)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErr)
		})
	}
}

func TestApplyFile_ShrinkFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		original []byte
		target   []byte
		current  []byte
	}{
		{
			name:     "preimage larger than source",
			original: []byte("1\n2\n3\n4\n5\n6\n7\n8\n999999\nA\nB\nC\nD\nE\nF\nG\nH\nI\nJ\n\n"),
			target:   []byte("11\n2\n3\n4\n5\n6\n7\n8\n9\nA\nB\nC\nD\nE\nF\nG\nHH\nI\nJ\n\n"),
			current:  []byte("2\n3\n4\n5\n6\n7\n8\n999999\nA\nB\nC\nD\nE\nF\nG\nH\nI\nJ\n"),
		},
		{
			name:     "near eof overrun",
			original: []byte("a\nb\nc\nd\ne\n"),
			target:   []byte("a\nb\nc\nd\nz\n"),
			current:  []byte("a\nb\nc\nd\n"),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			patch := buildPatch(t, "F", test.original, test.target)
			applied, err := ApplyFile(test.current, patch)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrPatchConflict)
			assert.Contains(t, string(applied), defaultCurrentConflictMarker)
		})
	}
}

func TestApplyFile_CRLFPreservation(t *testing.T) {
	t.Parallel()

	pristine := []byte("alpha\r\nbeta\r\n")
	target := []byte("alpha\r\nbravo\r\n")
	patch := buildPatch(t, "crlf.txt", pristine, target)

	applied, err := ApplyFile(pristine, patch)
	require.NoError(t, err)
	assert.Equal(t, target, applied)
}

func loadApplyFixture(t *testing.T, name string) applyFixtureFiles {
	t.Helper()

	load := func(ext string) []byte {
		t.Helper()

		path := filepath.Join("testdata", "apply", name+"."+ext)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		require.NoError(t, err)
		return data
	}

	return applyFixtureFiles{
		src:   load("src"),
		patch: load("patch"),
		out:   load("out"),
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func expectedApplyFixtureOutput(t *testing.T, files applyFixtureFiles) []byte {
	t.Helper()

	if files.out == nil {
		return nil
	}

	parsed, errs := parse(string(files.patch))
	require.Empty(t, errs)
	require.Len(t, parsed.FileDiff, 1)
	require.Len(t, parsed.FileDiff[0].Hunks, 1)

	hunk := parsed.FileDiff[0].Hunks[0]
	start := hunk.StartLineNumberOld - 1
	if start < 0 {
		start = 0
	}

	sourceLines := splitBytesLines(files.src)
	end := start + hunk.CountOld
	if end > len(sourceLines) {
		end = len(sourceLines)
	}

	expected := append([]byte{}, files.out...)
	for _, line := range sourceLines[end:] {
		expected = append(expected, line...)
	}
	return expected
}

func splitBytesLines(content []byte) [][]byte {
	if len(content) == 0 {
		return nil
	}

	lines := bytes.SplitAfter(content, []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func contextLabel(context int) string {
	if context == 0 {
		return "0"
	}
	return "3"
}

func rewriteFirstHunkHeader(patch []byte, header string) []byte {
	lines := bytes.Split(patch, []byte("\n"))
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte("@@ ")) {
			lines[i] = []byte(header)
			return bytes.Join(lines, []byte("\n"))
		}
	}
	return patch
}

func buildPatch(t *testing.T, path string, pristine, materialized []byte) []byte {
	t.Helper()
	return buildPatchWithContext(t, path, pristine, materialized, 3)
}

func buildPatchWithContext(t *testing.T, path string, pristine, materialized []byte, context int) []byte {
	t.Helper()

	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(pristine)),
		B:        difflib.SplitLines(string(materialized)),
		FromFile: "a/" + path,
		ToFile:   "b/" + path,
		Context:  context,
	})
	require.NoError(t, err)
	require.NotEmpty(t, diff)

	return append([]byte("diff --git a/"+path+" b/"+path+"\n"), []byte(diff)...)
}

func TestApplyFile_PreservesExactBytes(t *testing.T) {
	t.Parallel()

	files := loadApplyFixture(t, "text_fragment_change_single_noeol")
	applied, err := ApplyFile(files.src, files.patch)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(files.out, applied))
}
