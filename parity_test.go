//go:build parity

package git_diff_parser_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	git_diff_parser "github.com/speakeasy-api/git-diff-parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type parityFixture struct {
	GitArgs        []string `json:"gitArgs"`
	ExpectConflict bool     `json:"expectConflict"`
	CheckReject    bool     `json:"checkReject"`
}

type parityCase struct {
	name    string
	src     []byte
	patch   []byte
	out     []byte
	fixture parityFixture
}

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
			applied, err := git_diff_parser.ApplyFile(tc.src, tc.patch)

			if tc.fixture.ExpectConflict {
				require.Error(t, err)
				var conflictErr *git_diff_parser.ConflictError
				require.ErrorAs(t, err, &conflictErr)
				assert.True(t, errors.Is(err, git_diff_parser.ErrPatchConflict))
				assert.Equal(t, tc.src, oracles.applied)
				assert.Contains(t, string(applied), "<<<<<<< Current")
				assert.Contains(t, string(applied), ">>>>>>> Incoming patch")
				if len(tc.out) > 0 {
					for _, line := range bytes.Split(bytes.TrimSpace(tc.out), []byte("\n")) {
						if len(line) == 0 {
							continue
						}
						assert.Contains(t, string(applied), string(line))
					}
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, oracles.applied, applied)
				if len(tc.out) > 0 {
					assert.Equal(t, tc.out, applied)
				}
			}

			if tc.fixture.CheckReject {
				rejectOracles := runGitApplyOracles(t, tc, "--reject")
				require.True(t, rejectOracles.rejected)
				require.NotEqual(t, tc.src, rejectOracles.applied)
				if len(tc.out) > 0 {
					assert.Equal(t, tc.out, rejectOracles.applied)
				}
				require.NotEmpty(t, rejectOracles.rej)
				assert.Contains(t, string(rejectOracles.rej), "line5")
			}
		})
	}
}

type gitApplyOracle struct {
	applied  []byte
	rej      []byte
	rejected bool
	exitErr  error
}

func runGitApplyOracles(t *testing.T, tc parityCase, extraArgs ...string) gitApplyOracle {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), tc.src, 0o600))
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
	require.NoError(t, readErr)
	oracles.applied = applied

	rej, rejErr := os.ReadFile(filepath.Join(dir, "file.txt.rej"))
	if rejErr == nil {
		oracles.rej = rej
	}

	if len(output) > 0 && err == nil {
		// git apply is quiet here; keep the command output surfaced only if it was unexpected.
		assert.Empty(t, string(output))
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
		cases = append(cases, parityCase{
			name:    entry.Name(),
			src:     readParityFile(t, filepath.Join(dir, "src")),
			patch:   readParityFile(t, filepath.Join(dir, "patch")),
			out:     readParityFile(t, filepath.Join(dir, "out")),
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

func requireGitBinary(t *testing.T) {
	t.Helper()

	_, err := exec.LookPath("git")
	require.NoError(t, err)
}
