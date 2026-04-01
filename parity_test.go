//go:build parity

package git_diff_parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type parityFixture struct {
	GitArgs          []string          `json:"gitArgs"`
	ExpectConflict   bool              `json:"expectConflict"`
	CheckReject      bool              `json:"checkReject"`
	IgnoreWhitespace bool              `json:"ignoreWhitespace"`
	SkipLibrary      bool              `json:"skipLibrary"`
	ExpectGitError   bool              `json:"expectGitError"`
	SrcFiles         map[string]string `json:"srcFiles"`
	OutFiles         map[string]string `json:"outFiles"`
	SrcModes         map[string]string `json:"srcModes"`
	OutModes         map[string]string `json:"outModes"`
}

type parityCase struct {
	name    string
	src     []byte
	patch   []byte
	out     []byte
	rej     []byte
	srcTree parityTree
	outTree parityTree
	fixture parityFixture
}

type parityFile struct {
	content []byte
	mode    fs.FileMode
}

type parityTree map[string]parityFile

func TestApplyFile_ParityCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("parity corpus is an integration test stream")
	}

	requireGitBinary(t)

	cases := loadParityCases(t)
	require.NotEmpty(t, cases)

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			oracles := runGitApplyOracles(t, tc)
			if tc.fixture.SkipLibrary {
				assertParityTree(t, tc.outTree, oracles.tree)
				if tc.fixture.ExpectGitError {
					require.Error(t, oracles.exitErr)
				} else {
					require.NoError(t, oracles.exitErr)
				}
				return
			}

			mergeResult, mergeErr := runLibraryApply(t, tc, false)

			if tc.fixture.ExpectConflict {
				require.Error(t, mergeErr)
				var applyErr *applyError
				require.ErrorAs(t, mergeErr, &applyErr)
				assert.True(t, errors.Is(mergeErr, ErrPatchConflict))
				assert.Equal(t, tc.src, oracles.applied)
				assert.Contains(t, string(mergeResult.Content), "<<<<<<< Current")
				assert.Contains(t, string(mergeResult.Content), ">>>>>>> Incoming patch")
				if len(tc.out) > 0 {
					for _, line := range bytes.Split(bytes.TrimSpace(tc.out), []byte("\n")) {
						if len(line) == 0 {
							continue
						}
						assert.Contains(t, string(mergeResult.Content), string(line))
					}
				}
				assertParityTree(t, tc.srcTree, oracles.tree)
			} else {
				require.NoError(t, mergeErr)
				require.Equal(t, oracles.applied, mergeResult.Content)
				if len(tc.out) > 0 {
					assert.Equal(t, tc.out, mergeResult.Content)
				}
				assertParityTree(t, tc.outTree, oracles.tree)
			}

			if tc.fixture.CheckReject {
				rejectOracles := runGitApplyOracles(t, tc, "--reject")
				require.True(t, rejectOracles.rejected)
				rejectResult, rejectErr := runLibraryApply(t, tc, true)
				require.Error(t, rejectErr)
				var applyErr *applyError
				require.ErrorAs(t, rejectErr, &applyErr)
				require.NotEqual(t, tc.src, rejectOracles.applied)
				assert.Equal(t, tc.src, rejectResult.Content)
				if len(tc.rej) > 0 {
					assert.Equal(t, tc.rej, trimGitRejectHeader(rejectResult.Reject))
				} else {
					assert.Equal(t, rejectOracles.rej, rejectResult.Reject)
				}
				if len(tc.out) > 0 {
					assert.Equal(t, tc.out, rejectOracles.applied)
				}
				require.NotEmpty(t, rejectOracles.rej)
				assert.Contains(t, string(rejectOracles.rej), "rejected hunks")
			}
		})
	}
}

func runLibraryApply(t *testing.T, tc parityCase, rejectMode bool) (applyResult, error) {
	t.Helper()

	options := defaultMergeApplyOptions()
	options.IgnoreWhitespace = tc.fixture.IgnoreWhitespace
	if rejectMode {
		options = defaultApplyOptions()
		options.IgnoreWhitespace = tc.fixture.IgnoreWhitespace
	}
	if minContext, ok := fixtureContextArg(tc.fixture); ok {
		options.MinContext = minContext
		options.MinContextSet = true
	}

	return applyFileWithOptions(tc.src, tc.patch, options)
}

func trimGitRejectHeader(rej []byte) []byte {
	if idx := bytes.IndexByte(rej, '\n'); idx >= 0 {
		return rej[idx+1:]
	}
	return rej
}

func fixtureContextArg(fixture parityFixture) (int, bool) {
	for _, candidate := range fixture.GitArgs {
		if !strings.HasPrefix(candidate, "-C") || len(candidate) <= 2 {
			continue
		}
		value, err := strconv.Atoi(strings.TrimPrefix(candidate, "-C"))
		if err == nil {
			return value, true
		}
	}
	return 0, false
}

type gitApplyOracle struct {
	applied  []byte
	tree     parityTree
	rej      []byte
	rejected bool
	exitErr  error
}

func runGitApplyOracles(t *testing.T, tc parityCase, extraArgs ...string) gitApplyOracle {
	t.Helper()

	dir := t.TempDir()
	writeParityTree(t, dir, tc.srcTree)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "patch.diff"), tc.patch, 0o600))

	args := []string{"apply", "--whitespace=nowarn"}
	args = append(args, tc.fixture.GitArgs...)
	args = append(args, extraArgs...)
	args = append(args, "patch.diff")

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	oracles := gitApplyOracle{exitErr: err}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			oracles.rejected = exitErr.ExitCode() != 0
		} else {
			require.NoError(t, err, "git apply failed to start: %s", output)
		}
	} else {
		oracles.rejected = false
	}

	applied, readErr := os.ReadFile(filepath.Join(dir, "file.txt"))
	if readErr == nil {
		oracles.applied = applied
	}

	rej, rejErr := os.ReadFile(filepath.Join(dir, "file.txt.rej"))
	if rejErr == nil {
		oracles.rej = rej
	}

	oracles.tree = collectParityTree(t, dir)

	if len(output) > 0 && err == nil {
		// git apply may emit successful warnings like context reduction; tree state is the oracle here.
	}

	return oracles
}

func loadParityCases(t *testing.T) []parityCase {
	t.Helper()

	root := filepath.Join("testdata", "parity")
	entries, err := os.ReadDir(root)
	require.NoError(t, err)

	cases := make([]parityCase, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(root, entry.Name())
		fixture := readParityFixture(t, filepath.Join(dir, "fixture.json"))
		srcTree, src, outTree, out := readParityTrees(t, dir, fixture)
		cases = append(cases, parityCase{
			name:    entry.Name(),
			src:     src,
			patch:   readParityFile(t, filepath.Join(dir, "patch")),
			out:     out,
			rej:     readParityFileMaybe(t, filepath.Join(dir, "rej")),
			srcTree: srcTree,
			outTree: outTree,
			fixture: fixture,
		})
	}

	sort.Slice(cases, func(i, j int) bool {
		return cases[i].name < cases[j].name
	})

	return cases
}

func readParityFixture(t *testing.T, path string) parityFixture {
	t.Helper()

	raw := readParityFile(t, path)
	var fixture parityFixture
	require.NoError(t, json.Unmarshal(raw, &fixture))
	return fixture
}

func readParityFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func readParityFileMaybe(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	return data
}

func readParityTrees(t *testing.T, dir string, fixture parityFixture) (parityTree, []byte, parityTree, []byte) {
	t.Helper()

	srcTree := loadParityTree(t, filepath.Join(dir, "src"), fixture.SrcFiles, fixture.SrcModes)
	outTree := loadParityTree(t, filepath.Join(dir, "out"), fixture.OutFiles, fixture.OutModes)
	return srcTree, treeBytes(srcTree), outTree, treeBytes(outTree)
}

func loadParityTree(t *testing.T, legacyPath string, files map[string]string, modes map[string]string) parityTree {
	t.Helper()

	if len(files) > 0 {
		tree := make(parityTree, len(files))
		for path, content := range files {
			tree[path] = parityFile{
				content: []byte(content),
				mode:    parseParityMode(modes[path]),
			}
		}
		return tree
	}

	legacy := readParityFileMaybe(t, legacyPath)
	if legacy == nil {
		return nil
	}
	return parityTree{
		"file.txt": {content: legacy},
	}
}

func parseParityMode(raw string) fs.FileMode {
	if raw == "" {
		return 0
	}
	if len(raw) >= 3 {
		raw = raw[len(raw)-3:]
	}
	switch raw {
	case "644":
		return 0o644
	case "755":
		return 0o755
	default:
		return 0
	}
}

func treeBytes(tree parityTree) []byte {
	if len(tree) != 1 {
		return nil
	}
	file, ok := tree["file.txt"]
	if !ok {
		return nil
	}
	return file.content
}

func writeParityTree(t *testing.T, root string, tree parityTree) {
	t.Helper()

	for path, file := range tree {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, file.content, 0o600))
		if file.mode != 0 {
			require.NoError(t, os.Chmod(fullPath, file.mode))
		}
	}
}

func collectParityTree(t *testing.T, root string) parityTree {
	t.Helper()

	tree := make(parityTree)
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if path == root || d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if base == "patch.diff" || strings.HasSuffix(base, ".rej") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		info, err := d.Info()
		require.NoError(t, err)
		tree[filepath.ToSlash(rel)] = parityFile{
			content: content,
			mode:    info.Mode().Perm(),
		}
		return nil
	}))
	return tree
}

func assertParityTree(t *testing.T, want, got parityTree) {
	t.Helper()

	if len(want) == 0 {
		assert.Len(t, got, 0)
		return
	}

	require.Len(t, got, len(want))
	for path, expected := range want {
		actual, ok := got[path]
		require.True(t, ok, "missing file %s", path)
		assert.Equal(t, expected.content, actual.content, "content mismatch for %s", path)
		if expected.mode != 0 {
			assert.Equal(t, expected.mode, actual.mode, "mode mismatch for %s", path)
		}
	}
	for path := range got {
		_, ok := want[path]
		assert.True(t, ok, "unexpected file %s", path)
	}
}

func requireGitBinary(t *testing.T) {
	t.Helper()

	_, err := exec.LookPath("git")
	require.NoError(t, err)
}
