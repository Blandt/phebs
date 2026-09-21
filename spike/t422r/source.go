package t422r

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const maximumGitOutput = 1 << 20

var errOutputLimit = errors.New("git output exceeds fixed byte bound")

type treeEntry struct {
	mode string
	kind string
	oid  string
}

// VerifyCleanSource resolves an exact clean Git root and verifies every
// tracked Go semantic input against its HEAD blob. Blob verification closes
// the assume-unchanged and skip-worktree gaps left by porcelain status.
func VerifyCleanSource(ctx context.Context, root string) (string, string, error) {
	if ctx == nil {
		return "", "", errors.New("context is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve repository root: %w", err)
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", "", fmt.Errorf("resolve repository root symlinks: %w", err)
	}
	topRaw, err := gitOutput(ctx, absolute, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	top, err := filepath.EvalSymlinks(strings.TrimSpace(string(topRaw)))
	if err != nil {
		return "", "", fmt.Errorf("resolve Git root symlinks: %w", err)
	}
	if top != absolute {
		return "", "", errors.New("root must name the Git checkout root")
	}
	commitRaw, err := gitOutput(ctx, top, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", "", err
	}
	commit := strings.TrimSpace(string(commitRaw))
	if !validCommit(commit) {
		return "", "", errors.New("HEAD is not a lowercase 40-character hexadecimal identity")
	}
	status, err := gitOutput(ctx, top, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return "", "", err
	}
	if len(status) != 0 {
		return "", "", errors.New("source checkout must be exactly clean")
	}
	tree, err := readCommitTree(ctx, top, commit)
	if err != nil {
		return "", "", err
	}
	if err := verifyTrackedInputs(top, tree); err != nil {
		return "", "", err
	}
	if err := verifyNoUntrackedInputs(top, tree); err != nil {
		return "", "", err
	}
	return top, commit, nil
}

func readCommitTree(ctx context.Context, root, commit string) (map[string]treeEntry, error) {
	raw, err := gitOutput(ctx, root, "ls-tree", "-r", "--full-tree", "-z", commit)
	if err != nil {
		return nil, err
	}
	result := make(map[string]treeEntry)
	for _, record := range bytes.Split(raw, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		header, path, ok := bytes.Cut(record, []byte{'\t'})
		if !ok {
			return nil, errors.New("malformed git ls-tree record")
		}
		fields := bytes.Fields(header)
		if len(fields) != 3 {
			return nil, errors.New("malformed git ls-tree header")
		}
		relative := filepath.ToSlash(string(path))
		if !safeRelative(relative) {
			return nil, fmt.Errorf("unsafe path in Git tree: %q", relative)
		}
		result[relative] = treeEntry{mode: string(fields[0]), kind: string(fields[1]), oid: string(fields[2])}
	}
	return result, nil
}

func verifyTrackedInputs(root string, tracked map[string]treeEntry) error {
	paths := make([]string, 0, len(tracked))
	for relative := range tracked {
		if semanticInput(relative) {
			paths = append(paths, relative)
		}
	}
	sort.Strings(paths)
	for _, relative := range paths {
		if err := verifyTrackedBlob(root, relative, tracked[relative]); err != nil {
			return err
		}
	}
	return nil
}

func verifyCompiledInputs(root string, tracked map[string]treeEntry, compiled []string) error {
	for _, path := range compiled {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve compiled Go input: %w", err)
		}
		relative, err := filepath.Rel(root, absolute)
		if err != nil {
			return fmt.Errorf("relativize compiled Go input: %w", err)
		}
		relative = filepath.ToSlash(relative)
		if !safeRelative(relative) {
			return fmt.Errorf("compiled Go input is outside the pinned tree: %s", path)
		}
		entry, ok := tracked[relative]
		if !ok {
			return fmt.Errorf("compiled Go input is absent from the pinned tree: %s", relative)
		}
		if err := verifyTrackedBlob(root, relative, entry); err != nil {
			return err
		}
	}
	return nil
}

func verifyNoUntrackedInputs(root string, tracked map[string]treeEntry) error {
	const maximumEntries = 100_000
	entries := 0
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > maximumEntries {
			return errors.New("source checkout exceeds filesystem entry bound")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relativize source checkout entry: %w", err)
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			if tree, ok := tracked[relative]; ok && tree.kind == "commit" && tree.mode == "160000" {
				return filepath.SkipDir
			}
			return nil
		}
		if relative == "." || !semanticInput(relative) {
			return nil
		}
		if _, ok := tracked[relative]; !ok {
			return fmt.Errorf("semantic Go input is absent from the pinned tree: %s", relative)
		}
		return nil
	})
}

func verifyTrackedBlob(root, relative string, entry treeEntry) error {
	if entry.kind != "blob" || (entry.mode != "100644" && entry.mode != "100755") {
		return fmt.Errorf("semantic Go input is not a regular tracked blob: %s", relative)
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat tracked Go semantic input %s: %w", relative, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("tracked Go semantic input is not a regular file: %s", relative)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read tracked Go semantic input %s: %w", relative, err)
	}
	if got := gitBlobOID(content); got != entry.oid {
		return fmt.Errorf("worktree content does not match pinned Git blob at %s", relative)
	}
	return nil
}

func semanticInput(relative string) bool {
	path := filepath.FromSlash(relative)
	base := filepath.Base(path)
	return strings.HasSuffix(relative, ".go") || base == "go.mod" || base == "go.sum" ||
		(base == "modules.txt" && filepath.Base(filepath.Dir(path)) == "vendor")
}

func gitBlobOID(content []byte) string {
	hash := sha1.New() //nolint:gosec // Git SHA-1 object identity, not a security digest.
	_, _ = fmt.Fprintf(hash, "blob %d%c", len(content), byte(0))
	_, _ = hash.Write(content)
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func safeRelative(path string) bool {
	if path == "" || filepath.IsAbs(filepath.FromSlash(path)) || strings.ContainsRune(path, '\x00') {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	return clean == path && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *boundedBuffer) Write(content []byte) (int, error) {
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining >= len(content) {
		return buffer.buffer.Write(content)
	}
	if remaining > 0 {
		_, _ = buffer.buffer.Write(content[:remaining])
	}
	buffer.overflow = true
	return max(remaining, 0), errOutputLimit
}

func gitOutput(ctx context.Context, root string, arguments ...string) ([]byte, error) {
	if len(arguments) == 0 {
		return nil, errors.New("git operation is required")
	}
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, arguments...)...)
	command.Env = []string{
		"HOME=", "TMPDIR=" + os.TempDir(), "PATH=" + os.Getenv("PATH"),
		"LANG=C", "LC_ALL=C", "TZ=UTC",
		"GIT_ATTR_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_NO_LAZY_FETCH=1", "GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_0=core.fsmonitor", "GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=core.untrackedcache", "GIT_CONFIG_VALUE_1=false",
		"GIT_CONFIG_KEY_2=core.hookspath", "GIT_CONFIG_VALUE_2=" + os.DevNull,
	}
	stdout := boundedBuffer{limit: maximumGitOutput}
	stderr := boundedBuffer{limit: 8 << 10}
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if stdout.overflow || stderr.overflow {
		return nil, errOutputLimit
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", arguments[0], err)
	}
	return append([]byte(nil), stdout.buffer.Bytes()...), nil
}
