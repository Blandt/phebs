package t422s

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCensusProjectsOnlyExactReviewedText(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "--quiet")
	git(t, root, "config", "user.name", "T42.2s Test")
	git(t, root, "config", "user.email", "t422s@example.invalid")
	write(t, root, "internal/store/state.go", "package store\n\nfunc state() int { return 1 }\n", 0o600)
	write(t, root, "spike/t4013/run.sh", "#!/bin/sh\necho before\n", 0o755)
	write(t, root, "generated.go", "// Code generated fixture DO NOT EDIT.\npackage generated\n", 0o600)
	write(t, root, "binary.sh", "#!/bin/sh\necho before\n", 0o755)
	write(t, root, "large.go", "package large\nvar Value = \"before\"\n", 0o600)
	write(t, root, "many.go", "package many\n", 0o600)
	write(t, root, "literal*.sh", "#!/bin/sh\necho literal-before\n", 0o755)
	write(t, root, "literal-other.sh", "#!/bin/sh\necho OTHER_BEFORE\n", 0o755)
	git(t, root, "add", ".")
	git(t, root, "commit", "--quiet", "-m", "base")
	base := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))

	write(t, root, "internal/store/state.go", "package store\n\nfunc state() int { return 2 }\n", 0o600)
	write(t, root, "spike/t4013/run.sh", "#!/bin/sh\necho after\n", 0o755)
	write(t, root, "generated.go", "// Code generated fixture DO NOT EDIT.\npackage generated\nvar X = 1\n", 0o600)
	writeBytes(t, root, "binary.sh", []byte("#!/bin/sh\necho \x00\n"), 0o755)
	write(t, root, "large.go", "package large\nvar Value = \""+strings.Repeat("x", maxHunkBytes)+"\"\n", 0o600)
	write(t, root, "literal*.sh", "#!/bin/sh\necho literal-after\n", 0o755)
	write(t, root, "literal-other.sh", "#!/bin/sh\necho OTHER_AFTER\n", 0o755)
	var many strings.Builder
	many.WriteString("package many\n")
	for index := 0; index <= maxHunkLines; index++ {
		many.WriteString("var X = 1\n")
	}
	write(t, root, "many.go", many.String(), 0o600)
	git(t, root, "add", ".")
	git(t, root, "commit", "--quiet", "-m", "head")

	canonicalRoot, head, records, err := Census(t.Context(), root, base)
	if err != nil {
		t.Fatal(err)
	}
	rootInfo, statErr := os.Stat(root)
	canonicalInfo, canonicalStatErr := os.Stat(canonicalRoot)
	if statErr != nil || canonicalStatErr != nil || !os.SameFile(rootInfo, canonicalInfo) ||
		len(base) != 40 || len(head) != 40 || len(records) != 8 {
		t.Fatalf("census root=%q base=%q head=%q records=%d", canonicalRoot, base, head, len(records))
	}
	reviewReasons := make(map[string]bool)
	eligible := make([]Hunk, 0, 2)
	for _, record := range records {
		if record.ReviewReason != "" {
			reviewReasons[record.ReviewReason] = true
			continue
		}
		eligible = append(eligible, record)
		if record.ModelState.Before == record.ModelState.After ||
			strings.Contains(record.ModelState.Before, "@@") || strings.Contains(record.ModelState.After, "@@") ||
			strings.Contains(record.ModelState.Before, record.Provenance.Path) ||
			strings.Contains(record.ModelState.After, record.Provenance.Path) {
			t.Fatalf("model state retained provenance or no change: %+v", record.ModelState)
		}
	}
	for _, reason := range []string{"binary_source", "generated_source", "hunk_oversize", "hunk_line_oversize"} {
		if !reviewReasons[reason] {
			t.Errorf("missing explicit review reason %q: %v", reason, reviewReasons)
		}
	}
	if len(eligible) != 4 {
		t.Fatalf("eligible hunks = %d, want 4", len(eligible))
	}
	roles := map[string]bool{}
	for _, record := range eligible {
		roles[record.ModelState.ArtifactRole] = true
		if record.Provenance.Path == "literal*.sh" &&
			(strings.Contains(record.ModelState.Before, "OTHER_") || strings.Contains(record.ModelState.After, "OTHER_")) {
			t.Fatal("literal pathspec widened to another source file")
		}
	}
	if !roles[ArtifactRuntime] || !roles[ArtifactCeremony] {
		t.Fatalf("artifact roles = %v", roles)
	}

	allowlist := Allowlist{Schema: AllowlistSchema, BaseCommit: base, HeadCommit: head, Hunks: []AllowlistEntry{
		{HunkID: eligible[0].HunkID, ContentSHA256: eligible[0].ContentSHA256},
		{HunkID: eligible[1].HunkID, ContentSHA256: eligible[1].ContentSHA256},
	}}
	selected, err := SelectReviewed(records, allowlist, base, head)
	if err != nil || len(selected) != 2 {
		t.Fatalf("select reviewed = %d, %v", len(selected), err)
	}
	allowlist.Hunks[0].ContentSHA256 = digest([]byte("changed"))
	if _, err := SelectReviewed(records, allowlist, base, head); err == nil {
		t.Fatal("content-digest mismatch was accepted")
	}
	allowlist.Hunks[0].ContentSHA256 = eligible[0].ContentSHA256
	allowlist.HeadCommit = strings.Repeat("a", 40)
	if _, err := SelectReviewed(records, allowlist, base, head); err == nil {
		t.Fatal("allowlist for a different base/HEAD pair was accepted")
	}

	var first, second bytes.Buffer
	if err := EncodeHunks(&first, base, head, records); err != nil {
		t.Fatal(err)
	}
	git(t, root, "config", "diff.algorithm", "histogram")
	git(t, root, "config", "diff.indentHeuristic", "true")
	git(t, root, "config", "diff.suppressBlankEmpty", "true")
	_, _, repeated, err := Census(t.Context(), root, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := EncodeHunks(&second, base, head, repeated); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("repeated projection is not byte-identical")
	}

	git(t, root, "update-index", "--assume-unchanged", "spike/t4013/run.sh")
	write(t, root, "spike/t4013/run.sh", "#!/bin/sh\necho hidden-dirty\n", 0o755)
	if _, _, _, err := Census(t.Context(), root, base); err == nil || !strings.Contains(err.Error(), "pinned Git blob") {
		t.Fatalf("hidden shell change refusal = %v", err)
	}
}

func TestCensusRejectsHiddenExecutableAfterShebangRemoval(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "--quiet")
	git(t, root, "config", "user.name", "T42.2s Test")
	git(t, root, "config", "user.email", "t422s@example.invalid")
	write(t, root, "tool", "#!/bin/sh\necho before\n", 0o755)
	git(t, root, "add", "tool")
	git(t, root, "commit", "--quiet", "-m", "base")
	base := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	write(t, root, "tool", "#!/bin/sh\necho after\n", 0o755)
	git(t, root, "add", "tool")
	git(t, root, "commit", "--quiet", "-m", "head")
	git(t, root, "update-index", "--assume-unchanged", "tool")
	write(t, root, "tool", "not a shell script\n", 0o755)
	if _, _, _, err := Census(t.Context(), root, base); err == nil || !strings.Contains(err.Error(), "pinned Git blob") {
		t.Fatalf("hidden executable change refusal = %v", err)
	}
}

func TestCensusRejectsHiddenPriorExecutableAfterModeDowngrade(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "--quiet")
	git(t, root, "config", "user.name", "T42.2s Test")
	git(t, root, "config", "user.email", "t422s@example.invalid")
	write(t, root, "tool", "#!/bin/sh\necho before\n", 0o755)
	git(t, root, "add", "tool")
	git(t, root, "commit", "--quiet", "-m", "base")
	base := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	write(t, root, "tool", "#!/bin/sh\necho after\n", 0o755)
	if err := os.Chmod(filepath.Join(root, "tool"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "tool")
	git(t, root, "commit", "--quiet", "-m", "head")
	head := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	canonicalRoot, _, _, err := Census(t.Context(), root, base)
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "update-index", "--assume-unchanged", "tool")
	write(t, root, "tool", "#!/bin/sh\necho hidden-dirty\n", 0o644)
	if err := VerifyCleanPair(t.Context(), canonicalRoot, base, head); err == nil || !strings.Contains(err.Error(), "pinned Git blob") {
		t.Fatalf("final hidden prior-executable change refusal = %v", err)
	}
	if _, _, _, err := Census(t.Context(), root, base); err == nil || !strings.Contains(err.Error(), "pinned Git blob") {
		t.Fatalf("hidden prior-executable change refusal = %v", err)
	}
}

func TestParseUnifiedHunkStripsMetadataAndKeepsExactSides(t *testing.T) {
	raw := []byte("diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,2 +1,2 @@ named\n package x\n-var X = 1\n+var X = 2\n")
	hunks, err := parseUnifiedHunks(raw)
	if err != nil || len(hunks) != 1 {
		t.Fatalf("parse = %d, %v", len(hunks), err)
	}
	if got, want := string(hunks[0].before), "package x\nvar X = 1\n"; got != want {
		t.Fatalf("before = %q, want %q", got, want)
	}
	if got, want := string(hunks[0].after), "package x\nvar X = 2\n"; got != want {
		t.Fatalf("after = %q, want %q", got, want)
	}
	if hunks[0].lines != 3 {
		t.Fatalf("hunk lines = %d, want 3", hunks[0].lines)
	}
	for path, want := range map[string]string{
		"internal/store/jobs.go":        ArtifactRuntime,
		"spike/t4013/rehearsal.go":      ArtifactCeremony,
		"cmd/phebs/t421_exact_reads.go": ArtifactCeremony,
		"cmd/phebs/t422_control.go":     ArtifactCeremony,
		"cmd/phebs/main.go":             ArtifactCeremony,
	} {
		if got := artifactRoleFor(path); got != want {
			t.Errorf("artifact role for %s = %q, want %q", path, got, want)
		}
	}
}

func write(t *testing.T, root, relative, content string, mode os.FileMode) {
	t.Helper()
	writeBytes(t, root, relative, []byte(content), mode)
}

func writeBytes(t *testing.T, root, relative string, content []byte, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", arguments[0], err, output)
	}
	return string(output)
}
