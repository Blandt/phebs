package t421

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This is deliberately one full authoring case. AuthorV3 invokes the genuine
// corrected constructor; DecodePlan independently reconstructs that contract.
// Run it in the serialized full-builder gate, not the cheap CLI selector gate.
func TestAuthorV3CanonicalRoundTrip(t *testing.T) {
	repository, commit := authorRepositoryFixture(t)
	destination := filepath.Join(t.TempDir(), "plan-v3.json")
	identity, err := AuthorV3(t.Context(), destination, repository, commit)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(destination)
	if err != nil || identity.Bytes != uint64(len(raw)) || identity.SHA256 != SHA256(raw) || len(raw) > MaxPlanV3AuthorBytes || bytes.Count(raw, []byte{'\n'}) != 1 {
		t.Fatal("authored canonical V3 identity or byte headroom differs", err)
	}
	plan, err := DecodePlan(raw)
	if err != nil || plan.Schema != PlanV3Schema || plan.SourceCommit != commit || plan.LogicalStoreWork == nil || plan.SelectorHandoffCleanup == nil {
		t.Fatal("authored plan did not independently replay the corrected V3 contract", err)
	}
	again, err := MarshalCanonical(plan)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("authored V3 plan changed during canonical round trip", err)
	}
	info, err := os.Stat(destination)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatal("authored V3 plan is not a private regular artifact", err)
	}
	if status := authorFixtureGit(t, repository, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatal("external artifact authoring changed the selected source checkout")
	}
}

func TestAuthorV4CanonicalRoundTrip(t *testing.T) {
	repository, commit := authorRepositoryFixture(t)
	destination := filepath.Join(t.TempDir(), "plan-v4.json")
	identity, err := AuthorV4(t.Context(), destination, repository, commit)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(destination)
	if err != nil || identity.Bytes != uint64(len(raw)) || identity.SHA256 != SHA256(raw) || len(raw) > MaxPlanV3AuthorBytes || bytes.Count(raw, []byte{'\n'}) != 1 {
		t.Fatal("authored canonical V4 identity or byte headroom differs", err)
	}
	plan, err := DecodePlan(raw)
	if err != nil || plan.Schema != PlanV4Schema || plan.SourceCommit != commit || plan.LogicalStoreWork == nil || plan.SelectorHandoffCleanup == nil ||
		plan.ToolPolicy.ExecutionFreezeSchema != ExecutionFreezeV4Schema || plan.ReceiptContract.Schema != ReceiptV4Schema {
		t.Fatal("authored plan did not independently replay the pressure-continuity V4 contract", err)
	}
	again, err := MarshalCanonical(plan)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("authored V4 plan changed during canonical round trip", err)
	}
	info, err := os.Stat(destination)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatal("authored V4 plan is not a private regular artifact", err)
	}
	if status := authorFixtureGit(t, repository, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatal("external artifact authoring changed the selected source checkout")
	}
}

func TestAuthorPlanRefusesBeforeConstruction(t *testing.T) {
	for _, test := range []string{"different_commit", "untracked", "unstaged", "staged", "hidden_tracked"} {
		t.Run(test, func(t *testing.T) {
			repository, commit := authorRepositoryFixture(t)
			source := commit
			switch test {
			case "different_commit":
				source = strings.Repeat("a", 40)
				if source == commit {
					source = strings.Repeat("b", 40)
				}
			case "untracked":
				if err := os.WriteFile(filepath.Join(repository, "new"), []byte("untracked\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "unstaged", "staged":
				if err := os.WriteFile(filepath.Join(repository, "fixture"), []byte("changed\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if test == "staged" {
					authorFixtureGit(t, repository, "add", "fixture")
				}
			case "hidden_tracked":
				authorFixtureGit(t, repository, "update-index", "--assume-unchanged", "fixture")
			}
			destination := filepath.Join(t.TempDir(), "must-not-exist.json")
			called := false
			_, err := authorPlan(t.Context(), destination, repository, source, func(string) (Plan, error) {
				called = true
				return Plan{}, errors.New("test sentinel: forbidden constructor call")
			})
			if err == nil || called {
				t.Fatal("invalid exact-clean source reached construction")
			}
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("refused authoring created an artifact", err)
			}
		})
	}
}

func authorRepositoryFixture(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is required for the exact-clean author boundary")
	}
	// Prevent user Git configuration, hooks or an ambient repository override
	// from substituting another checkout for this tiny native repository.
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	for name, value := range map[string]string{
		"GIT_AUTHOR_NAME": "Author fixture", "GIT_AUTHOR_EMAIL": "author@example.invalid", "GIT_AUTHOR_DATE": "2000-01-01T00:00:00Z",
		"GIT_COMMITTER_NAME": "Author fixture", "GIT_COMMITTER_EMAIL": "author@example.invalid", "GIT_COMMITTER_DATE": "2000-01-01T00:00:00Z",
	} {
		t.Setenv(name, value)
	}
	repository := t.TempDir()
	authorFixtureGit(t, repository, "init", "--quiet", "--template=", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(repository, "fixture"), []byte("clean source fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	authorFixtureGit(t, repository, "add", "fixture")
	authorFixtureGit(t, repository, "-c", "user.name=Author fixture", "-c", "user.email=author@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture")
	return repository, authorFixtureGit(t, repository, "rev-parse", "HEAD")
}

func authorFixtureGit(t *testing.T, repository string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = repository
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("native author fixture Git: %v: %s", err, raw)
	}
	return strings.TrimSpace(string(raw))
}
