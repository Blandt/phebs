// Package t422s projects exact-clean source diffs into a reviewed, shadow-only
// hazard-routing corpus. It is not part of ceremony execution.
package t422s

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/bmeddeb/phebs/spike/t422r"
)

const (
	HunkSchema      = "phebs-t422s-hunk-v1"
	StateSchema     = "phebs-t422s-hazard-state-v1"
	AllowlistSchema = "phebs-t422s-reviewed-allowlist-v1"

	LanguageGo    = "go"
	LanguageShell = "shell"

	ArtifactRuntime  = "runtime"
	ArtifactCeremony = "ceremony_harness"

	ChangeAdd    = "add"
	ChangeModify = "modify"
	ChangeDelete = "delete"
	ChangeMode   = "mode_change"

	maxChangedFiles  = 4096
	maxHunks         = 4096
	maxHunkBytes     = 16 << 10
	maxHunkLines     = 400
	maxGitListBytes  = 16 << 20
	maxFileDiffBytes = 4 << 20
	maxBlobBytes     = 4 << 20
)

var (
	errGitOutputLimit = errors.New("git output exceeds fixed byte bound")
	_hunkHeader       = regexp.MustCompile(`^@@ -[0-9]+(?:,[0-9]+)? \+[0-9]+(?:,[0-9]+)? @@`)
)

// ModelState is the complete source-derived value sent to Jev. Provenance is
// deliberately excluded; the reviewed allowlist is the content authorization.
type ModelState struct {
	Schema       string `json:"schema"`
	Language     string `json:"language"`
	ArtifactRole string `json:"artifact_role"`
	ChangeKind   string `json:"change_kind"`
	Before       string `json:"before"`
	After        string `json:"after"`
}

// Hunk is a private local review record. Its provenance never crosses the Jev
// boundary.
type Hunk struct {
	Schema        string      `json:"schema"`
	HunkID        string      `json:"hunk_id"`
	ContentSHA256 string      `json:"content_sha256"`
	Provenance    Provenance  `json:"provenance"`
	ModelState    *ModelState `json:"model_state,omitempty"`
	ReviewReason  string      `json:"review_reason,omitempty"`
}

type Provenance struct {
	BaseCommit  string `json:"base_commit"`
	HeadCommit  string `json:"head_commit"`
	Path        string `json:"path"`
	FileOrdinal int    `json:"file_ordinal"`
	HunkOrdinal int    `json:"hunk_ordinal"`
}

type Allowlist struct {
	Schema     string           `json:"schema"`
	BaseCommit string           `json:"base_commit"`
	HeadCommit string           `json:"head_commit"`
	Hunks      []AllowlistEntry `json:"hunks"`
}

type AllowlistEntry struct {
	HunkID        string `json:"hunk_id"`
	ContentSHA256 string `json:"content_sha256"`
}

type treeEntry struct {
	mode string
	kind string
	oid  string
}

type changedFile struct {
	path       string
	language   string
	role       string
	changeKind string
	old        treeEntry
	new        treeEntry
}

// Census returns a deterministic private hunk corpus for one exact base/HEAD
// pair. Review records are returned instead of silently dropping uncertainty.
func Census(ctx context.Context, root, base string) (string, string, []Hunk, error) {
	if ctx == nil {
		return "", "", nil, errors.New("context is required")
	}
	if !validCommit(base) {
		return "", "", nil, errors.New("base must be a lowercase 40-character commit identity")
	}
	canonicalRoot, head, err := verifyCleanSource(ctx, root)
	if err != nil {
		return "", "", nil, err
	}
	resolvedBase, err := gitOutput(ctx, canonicalRoot, maxGitListBytes, "rev-parse", "--verify", base+"^{commit}")
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve base commit: %w", err)
	}
	if strings.TrimSpace(string(resolvedBase)) != base {
		return "", "", nil, errors.New("base did not resolve to its exact supplied identity")
	}
	if err := requireAncestor(ctx, canonicalRoot, base, head); err != nil {
		return "", "", nil, err
	}
	files, err := changedFiles(ctx, canonicalRoot, base, head)
	if err != nil {
		return "", "", nil, err
	}
	if err := verifyDowngradedExecutables(canonicalRoot, files); err != nil {
		return "", "", nil, err
	}

	records := make([]Hunk, 0, len(files))
	for fileOrdinal, file := range files {
		projected, err := projectFile(ctx, canonicalRoot, base, head, fileOrdinal, file)
		if err != nil {
			return "", "", nil, err
		}
		if len(records)+len(projected) > maxHunks {
			return "", "", nil, errors.New("diff exceeds fixed hunk-count bound")
		}
		records = append(records, projected...)
	}
	verifiedRoot, verifiedHead, err := verifyCleanSource(ctx, canonicalRoot)
	if err != nil {
		return "", "", nil, fmt.Errorf("reverify source after census: %w", err)
	}
	if verifiedRoot != canonicalRoot || verifiedHead != head {
		return "", "", nil, errors.New("source identity changed during census")
	}
	if err := verifyDowngradedExecutables(canonicalRoot, files); err != nil {
		return "", "", nil, fmt.Errorf("reverify source after census: %w", err)
	}
	return canonicalRoot, head, records, nil
}

// VerifyCleanPair confirms that a previously censused checkout still has the
// same exact clean source identity, including prior executable shell inputs.
func VerifyCleanPair(ctx context.Context, root, base, head string) error {
	if !validCommit(base) || !validCommit(head) {
		return errors.New("base and HEAD must be lowercase 40-character commit identities")
	}
	canonicalRoot, currentHead, err := verifyCleanSource(ctx, root)
	if err != nil {
		return err
	}
	if canonicalRoot != root || currentHead != head {
		return errors.New("source identity changed after census")
	}
	files, err := changedFiles(ctx, canonicalRoot, base, head)
	if err != nil {
		return err
	}
	return verifyDowngradedExecutables(canonicalRoot, files)
}

// EncodeHunks writes the private review corpus as deterministic JSONL.
func EncodeHunks(writer io.Writer, base, head string, records []Hunk) error {
	if !validCommit(base) || !validCommit(head) {
		return errors.New("base and HEAD must be lowercase 40-character commit identities")
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(struct {
		Kind          string `json:"kind"`
		Schema        string `json:"schema"`
		BaseCommit    string `json:"base_commit"`
		HeadCommit    string `json:"head_commit"`
		Hunks         int    `json:"hunks"`
		ReviewHunks   int    `json:"review_hunks"`
		EligibleHunks int    `json:"eligible_hunks"`
	}{
		Kind: "header", Schema: HunkSchema, BaseCommit: base, HeadCommit: head,
		Hunks: len(records), ReviewHunks: countReview(records), EligibleHunks: len(records) - countReview(records),
	}); err != nil {
		return fmt.Errorf("encode hunk header: %w", err)
	}
	for _, record := range records {
		if err := validateHunk(record); err != nil {
			return err
		}
		if err := encoder.Encode(record); err != nil {
			return fmt.Errorf("encode hunk: %w", err)
		}
	}
	return nil
}

// DecodeAllowlist accepts only the closed reviewed-selection schema.
func DecodeAllowlist(reader io.Reader) (Allowlist, error) {
	var allowlist Allowlist
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&allowlist); err != nil {
		return Allowlist{}, fmt.Errorf("decode reviewed allowlist: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Allowlist{}, errors.New("reviewed allowlist has trailing JSON")
	}
	if allowlist.Schema != AllowlistSchema || !validCommit(allowlist.BaseCommit) || !validCommit(allowlist.HeadCommit) ||
		len(allowlist.Hunks) == 0 || len(allowlist.Hunks) > maxHunks {
		return Allowlist{}, errors.New("reviewed allowlist schema or size is invalid")
	}
	seen := make(map[string]struct{}, len(allowlist.Hunks))
	for _, entry := range allowlist.Hunks {
		if !validDigest(entry.HunkID) || !validDigest(entry.ContentSHA256) {
			return Allowlist{}, errors.New("reviewed allowlist contains an invalid digest")
		}
		if _, exists := seen[entry.HunkID]; exists {
			return Allowlist{}, errors.New("reviewed allowlist contains a duplicate hunk")
		}
		seen[entry.HunkID] = struct{}{}
	}
	return allowlist, nil
}

// SelectReviewed returns eligible hunks in census order only when every
// allowlist entry exactly matches the recomputed private corpus.
func SelectReviewed(records []Hunk, allowlist Allowlist, base, head string) ([]Hunk, error) {
	if allowlist.Schema != AllowlistSchema || allowlist.BaseCommit != base || allowlist.HeadCommit != head ||
		len(allowlist.Hunks) == 0 {
		return nil, errors.New("reviewed allowlist is invalid")
	}
	wanted := make(map[string]string, len(allowlist.Hunks))
	for _, entry := range allowlist.Hunks {
		if _, exists := wanted[entry.HunkID]; exists {
			return nil, errors.New("reviewed allowlist contains a duplicate hunk")
		}
		wanted[entry.HunkID] = entry.ContentSHA256
	}
	selected := make([]Hunk, 0, len(wanted))
	for _, record := range records {
		digest, ok := wanted[record.HunkID]
		if !ok {
			continue
		}
		if record.ModelState == nil || record.ReviewReason != "" {
			return nil, fmt.Errorf("reviewed hunk %s is not eligible for Jev", record.HunkID)
		}
		if digest != record.ContentSHA256 {
			return nil, fmt.Errorf("reviewed hunk %s content digest changed", record.HunkID)
		}
		selected = append(selected, record)
		delete(wanted, record.HunkID)
	}
	if len(wanted) != 0 {
		return nil, errors.New("reviewed allowlist contains a hunk absent from the exact census")
	}
	return selected, nil
}

func projectFile(
	ctx context.Context,
	root, base, head string,
	fileOrdinal int,
	file changedFile,
) ([]Hunk, error) {
	provenance := Provenance{
		BaseCommit: base, HeadCommit: head, Path: file.path, FileOrdinal: fileOrdinal,
	}
	if file.old.kind != "" && file.old.kind != "blob" || file.new.kind != "" && file.new.kind != "blob" {
		return []Hunk{reviewHunk(provenance, "non_blob_source")}, nil
	}
	if file.old.mode != "" && file.old.mode != "100644" && file.old.mode != "100755" ||
		file.new.mode != "" && file.new.mode != "100644" && file.new.mode != "100755" {
		return []Hunk{reviewHunk(provenance, "non_regular_source")}, nil
	}
	oldContent, oldErr := blobContent(ctx, root, file.old)
	newContent, newErr := blobContent(ctx, root, file.new)
	if errors.Is(oldErr, errGitOutputLimit) || errors.Is(newErr, errGitOutputLimit) {
		return []Hunk{reviewHunk(provenance, "source_blob_oversize")}, nil
	}
	if oldErr != nil || newErr != nil {
		return nil, errors.Join(oldErr, newErr)
	}
	if binaryContent(oldContent) || binaryContent(newContent) {
		return []Hunk{reviewHunk(provenance, "binary_source")}, nil
	}
	if generatedContent(oldContent) || generatedContent(newContent) {
		return []Hunk{reviewHunk(provenance, "generated_source")}, nil
	}
	if file.changeKind == ChangeMode {
		return []Hunk{reviewHunk(provenance, "mode_only_change")}, nil
	}
	diff, err := gitOutput(ctx, root, maxFileDiffBytes,
		"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "--text",
		"--diff-algorithm=myers", "--no-indent-heuristic", "--inter-hunk-context=0",
		"--output-indicator-new=+", "--output-indicator-old=-", "--output-indicator-context= ", "--unified=3",
		base, head, "--", file.path,
	)
	if errors.Is(err, errGitOutputLimit) {
		return []Hunk{reviewHunk(provenance, "diff_output_oversize")}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("diff source %q: %w", file.path, err)
	}
	hunks, err := parseUnifiedHunks(diff)
	if err != nil || len(hunks) == 0 {
		return []Hunk{reviewHunk(provenance, "unified_diff_uncertain")}, nil
	}
	records := make([]Hunk, 0, len(hunks))
	for index, excerpt := range hunks {
		itemProvenance := provenance
		itemProvenance.HunkOrdinal = index
		if len(excerpt.before)+len(excerpt.after) > maxHunkBytes {
			records = append(records, reviewHunk(itemProvenance, "hunk_oversize"))
			continue
		}
		if excerpt.lines > maxHunkLines {
			records = append(records, reviewHunk(itemProvenance, "hunk_line_oversize"))
			continue
		}
		state := ModelState{
			Schema: StateSchema, Language: file.language, ArtifactRole: file.role,
			ChangeKind: file.changeKind, Before: string(excerpt.before), After: string(excerpt.after),
		}
		record, err := eligibleHunk(itemProvenance, state)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func changedFiles(
	ctx context.Context,
	root, base, head string,
) ([]changedFile, error) {
	raw, err := gitOutput(ctx, root, maxGitListBytes,
		"diff", "--raw", "-z", "--no-renames", "--abbrev=40", base, head,
	)
	if err != nil {
		return nil, fmt.Errorf("enumerate changed paths: %w", err)
	}
	parts := bytes.Split(raw, []byte{0})
	files := make([]changedFile, 0, min(len(parts), maxChangedFiles))
	for index := 0; index < len(parts); {
		if len(parts[index]) == 0 {
			index++
			continue
		}
		if index+1 >= len(parts) {
			return nil, errors.New("changed-path record is truncated")
		}
		header := bytes.Fields(parts[index])
		pathRaw := parts[index+1]
		index += 2
		if len(header) != 5 || len(header[0]) != 7 || header[0][0] != ':' || len(header[1]) != 6 ||
			len(header[2]) != 40 || len(header[3]) != 40 || len(header[4]) != 1 {
			return nil, errors.New("changed-path record is malformed or rename detection was not disabled")
		}
		path := filepath.ToSlash(string(pathRaw))
		if !safeRelative(path) {
			return nil, errors.New("changed source path is unsafe")
		}
		oldEntry := rawTreeEntry(string(header[0][1:]), string(header[2]))
		newEntry := rawTreeEntry(string(header[1]), string(header[3]))
		language, err := sourceLanguage(ctx, root, path, oldEntry, newEntry)
		if err != nil {
			return nil, err
		}
		if language == "" {
			continue
		}
		kind, err := changeKind(string(header[4]), oldEntry, newEntry)
		if err != nil {
			return nil, fmt.Errorf("classify change for %q: %w", path, err)
		}
		files = append(files, changedFile{
			path: path, language: language, role: artifactRoleFor(path), changeKind: kind,
			old: oldEntry, new: newEntry,
		})
		if len(files) > maxChangedFiles {
			return nil, errors.New("diff exceeds fixed changed-source bound")
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, nil
}

type excerpt struct {
	before []byte
	after  []byte
	lines  int
}

func parseUnifiedHunks(raw []byte) ([]excerpt, error) {
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return nil, errors.New("unified diff is not valid UTF-8 text")
	}
	diffLines := bytes.Split(raw, []byte{'\n'})
	var hunks []excerpt
	var before bytes.Buffer
	var after bytes.Buffer
	lineCount := 0
	inHunk := false
	changed := false
	var previousPrefix byte
	finish := func() error {
		if !inHunk {
			return nil
		}
		if !changed || before.Len()+after.Len() == 0 {
			return errors.New("unified hunk has no changed lines")
		}
		hunks = append(hunks, excerpt{
			before: append([]byte(nil), before.Bytes()...),
			after:  append([]byte(nil), after.Bytes()...),
			lines:  lineCount,
		})
		before.Reset()
		after.Reset()
		changed = false
		lineCount = 0
		previousPrefix = 0
		return nil
	}
	for _, rawLine := range diffLines {
		line := rawLine
		if _hunkHeader.Match(line) {
			if err := finish(); err != nil {
				return nil, err
			}
			inHunk = true
			continue
		}
		if !inHunk {
			continue
		}
		if len(line) == 0 {
			// bytes.Split contributes one empty sentinel for the final newline.
			continue
		}
		switch line[0] {
		case ' ':
			lineCount++
			before.Write(line[1:])
			before.WriteByte('\n')
			after.Write(line[1:])
			after.WriteByte('\n')
			previousPrefix = ' '
		case '+':
			lineCount++
			changed = true
			after.Write(line[1:])
			after.WriteByte('\n')
			previousPrefix = '+'
		case '-':
			lineCount++
			changed = true
			before.Write(line[1:])
			before.WriteByte('\n')
			previousPrefix = '-'
		case '\\':
			if !bytes.Equal(line, []byte(`\ No newline at end of file`)) {
				return nil, errors.New("unrecognized unified-diff metadata")
			}
			switch previousPrefix {
			case ' ':
				removeFinalNewline(&before)
				removeFinalNewline(&after)
			case '+':
				removeFinalNewline(&after)
			case '-':
				removeFinalNewline(&before)
			default:
				return nil, errors.New("orphan no-newline marker")
			}
		default:
			return nil, errors.New("unrecognized unified-diff line")
		}
	}
	if err := finish(); err != nil {
		return nil, err
	}
	return hunks, nil
}

func removeFinalNewline(buffer *bytes.Buffer) {
	raw := buffer.Bytes()
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return
	}
	buffer.Truncate(len(raw) - 1)
}

func eligibleHunk(provenance Provenance, state ModelState) (Hunk, error) {
	if err := validateModelState(state); err != nil {
		return Hunk{}, err
	}
	contentRaw, err := json.Marshal(state)
	if err != nil {
		return Hunk{}, err
	}
	contentDigest := digest(contentRaw)
	identityRaw, err := json.Marshal(struct {
		Provenance    Provenance `json:"provenance"`
		ContentSHA256 string     `json:"content_sha256"`
	}{Provenance: provenance, ContentSHA256: contentDigest})
	if err != nil {
		return Hunk{}, err
	}
	return Hunk{
		Schema: HunkSchema, HunkID: digest(identityRaw), ContentSHA256: contentDigest,
		Provenance: provenance, ModelState: &state,
	}, nil
}

func reviewHunk(provenance Provenance, reason string) Hunk {
	identityRaw, _ := json.Marshal(struct {
		Provenance Provenance `json:"provenance"`
		Reason     string     `json:"review_reason"`
	}{Provenance: provenance, Reason: reason})
	return Hunk{
		Schema: HunkSchema, HunkID: digest(identityRaw), ContentSHA256: digest(nil),
		Provenance: provenance, ReviewReason: reason,
	}
}

func validateHunk(record Hunk) error {
	if record.Schema != HunkSchema || !validDigest(record.HunkID) || !validDigest(record.ContentSHA256) ||
		!validCommit(record.Provenance.BaseCommit) || !validCommit(record.Provenance.HeadCommit) ||
		!safeRelative(record.Provenance.Path) || record.Provenance.FileOrdinal < 0 || record.Provenance.HunkOrdinal < 0 {
		return errors.New("private hunk record is invalid")
	}
	if record.ReviewReason != "" {
		if record.ModelState != nil {
			return errors.New("review hunk unexpectedly contains model state")
		}
		return nil
	}
	if record.ModelState == nil {
		return errors.New("eligible hunk omits model state")
	}
	if err := validateModelState(*record.ModelState); err != nil {
		return err
	}
	raw, err := json.Marshal(record.ModelState)
	if err != nil {
		return err
	}
	if digest(raw) != record.ContentSHA256 {
		return errors.New("eligible hunk content digest is invalid")
	}
	return nil
}

func validateModelState(state ModelState) error {
	if state.Schema != StateSchema || !oneOf(state.Language, LanguageGo, LanguageShell) ||
		!oneOf(state.ArtifactRole, ArtifactRuntime, ArtifactCeremony) ||
		!oneOf(state.ChangeKind, ChangeAdd, ChangeModify, ChangeDelete) ||
		state.Before == state.After || len(state.Before)+len(state.After) == 0 ||
		len(state.Before)+len(state.After) > maxHunkBytes || !utf8.ValidString(state.Before) || !utf8.ValidString(state.After) {
		return errors.New("closed Jev model state is invalid")
	}
	return nil
}

func verifyCleanSource(ctx context.Context, root string) (string, string, error) {
	canonicalRoot, head, err := t422r.VerifyCleanSource(ctx, root)
	if err != nil {
		return "", "", err
	}
	tree, err := readTree(ctx, canonicalRoot, head)
	if err != nil {
		return "", "", err
	}
	paths := make([]string, 0)
	for path, entry := range tree {
		if strings.HasSuffix(path, ".sh") || entry.mode == "100755" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := verifyWorktreeBlob(canonicalRoot, path, tree[path]); err != nil {
			return "", "", err
		}
	}
	return canonicalRoot, head, nil
}

func verifyDowngradedExecutables(root string, files []changedFile) error {
	for _, file := range files {
		if file.old.mode == "100755" && file.new.mode == "100644" && !strings.HasSuffix(file.path, ".sh") {
			if err := verifyWorktreeBlob(root, file.path, file.new); err != nil {
				return err
			}
		}
	}
	return nil
}

func verifyWorktreeBlob(root, path string, entry treeEntry) error {
	if entry.kind != "blob" || entry.mode != "100644" && entry.mode != "100755" {
		return nil
	}
	absolute := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("stat tracked shell candidate %s: %w", path, err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return fmt.Errorf("open tracked shell candidate %s: %w", path, err)
	}
	hash := sha1.New() //nolint:gosec // Git SHA-1 object identity, not a security digest.
	_, _ = fmt.Fprintf(hash, "blob %d%c", info.Size(), byte(0))
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return fmt.Errorf("hash tracked shell source %s: %w", path, errors.Join(copyErr, closeErr))
	}
	if hex.EncodeToString(hash.Sum(nil)) != entry.oid {
		return errors.New("worktree shell content does not match pinned Git blob")
	}
	return nil
}

func readTree(ctx context.Context, root, commit string) (map[string]treeEntry, error) {
	raw, err := gitOutput(ctx, root, maxGitListBytes, "ls-tree", "-r", "--full-tree", "-z", commit)
	if err != nil {
		return nil, err
	}
	result := make(map[string]treeEntry)
	for _, record := range bytes.Split(raw, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		header, pathRaw, ok := bytes.Cut(record, []byte{'\t'})
		fields := bytes.Fields(header)
		path := filepath.ToSlash(string(pathRaw))
		if !ok || len(fields) != 3 || !safeRelative(path) {
			return nil, errors.New("git tree record is malformed")
		}
		result[path] = treeEntry{mode: string(fields[0]), kind: string(fields[1]), oid: string(fields[2])}
	}
	return result, nil
}

func sourceLanguage(ctx context.Context, root, path string, oldEntry, newEntry treeEntry) (string, error) {
	switch {
	case strings.HasSuffix(path, ".go"):
		return LanguageGo, nil
	case strings.HasSuffix(path, ".sh"):
		return LanguageShell, nil
	case oldEntry.mode != "100755" && newEntry.mode != "100755":
		return "", nil
	}
	for _, entry := range []treeEntry{newEntry, oldEntry} {
		content, err := blobContent(ctx, root, entry)
		if errors.Is(err, errGitOutputLimit) {
			return LanguageShell, nil
		}
		if err != nil {
			return "", err
		}
		if shellShebang(content) {
			return LanguageShell, nil
		}
	}
	return "", nil
}

func blobContent(ctx context.Context, root string, entry treeEntry) ([]byte, error) {
	if entry.oid == "" {
		return nil, nil
	}
	if entry.kind != "blob" {
		return nil, nil
	}
	return gitOutput(ctx, root, maxBlobBytes, "cat-file", "blob", entry.oid)
}

func rawTreeEntry(mode, oid string) treeEntry {
	if mode == "000000" {
		return treeEntry{}
	}
	kind := "blob"
	if mode == "160000" {
		kind = "commit"
	}
	return treeEntry{mode: mode, kind: kind, oid: oid}
}

func changeKind(status string, oldEntry, newEntry treeEntry) (string, error) {
	switch status {
	case "A":
		return ChangeAdd, nil
	case "D":
		return ChangeDelete, nil
	case "M":
		if oldEntry.oid == newEntry.oid && oldEntry.mode != newEntry.mode {
			return ChangeMode, nil
		}
		return ChangeModify, nil
	case "T":
		return ChangeModify, nil
	default:
		return "", fmt.Errorf("unsupported Git change status %q", status)
	}
}

func artifactRoleFor(path string) string {
	if strings.HasPrefix(path, "spike/") {
		return ArtifactCeremony
	}
	if strings.HasPrefix(path, "cmd/phebs/") {
		base := filepath.Base(path)
		if base == "main.go" || base == "main_test.go" || strings.HasPrefix(base, "t40") ||
			strings.HasPrefix(base, "t41") || strings.HasPrefix(base, "t42") {
			return ArtifactCeremony
		}
	}
	return ArtifactRuntime
}

func generatedContent(content []byte) bool {
	for _, line := range bytes.Split(content, []byte{'\n'}) {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("// Code generated ")) && bytes.HasSuffix(trimmed, []byte(" DO NOT EDIT.")) ||
			bytes.HasPrefix(trimmed, []byte("# Code generated ")) && bytes.HasSuffix(trimmed, []byte(" DO NOT EDIT.")) {
			return true
		}
	}
	return false
}

func binaryContent(content []byte) bool {
	prefix := content
	if len(prefix) > 8000 {
		prefix = prefix[:8000]
	}
	return bytes.IndexByte(prefix, 0) >= 0 || !utf8.Valid(content)
}

func shellShebang(content []byte) bool {
	line := content
	if newline := bytes.IndexByte(line, '\n'); newline >= 0 {
		line = line[:newline]
	}
	line = bytes.TrimSpace(line)
	return bytes.HasPrefix(line, []byte("#!/bin/sh")) || bytes.HasPrefix(line, []byte("#!/bin/bash")) ||
		bytes.HasPrefix(line, []byte("#!/bin/zsh")) || bytes.HasPrefix(line, []byte("#!/usr/bin/env sh")) ||
		bytes.HasPrefix(line, []byte("#!/usr/bin/env bash")) || bytes.HasPrefix(line, []byte("#!/usr/bin/env zsh"))
}

func requireAncestor(ctx context.Context, root, base, head string) error {
	command := exec.CommandContext(ctx, "git", "-C", root, "merge-base", "--is-ancestor", base, head)
	command.Env = gitEnvironment()
	if err := command.Run(); err != nil {
		return errors.New("base must be an ancestor of exact HEAD")
	}
	return nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *boundedBuffer) Write(content []byte) (int, error) {
	remaining := buffer.limit - buffer.Len()
	if remaining >= len(content) {
		return buffer.Buffer.Write(content)
	}
	if remaining > 0 {
		_, _ = buffer.Buffer.Write(content[:remaining])
	}
	buffer.overflow = true
	return max(remaining, 0), errGitOutputLimit
}

func gitOutput(ctx context.Context, root string, limit int, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, arguments...)...)
	command.Env = gitEnvironment()
	stdout := boundedBuffer{limit: limit}
	stderr := boundedBuffer{limit: 8 << 10}
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if stdout.overflow || stderr.overflow {
		return nil, errGitOutputLimit
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", arguments[0], err)
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}

func gitEnvironment() []string {
	return []string{
		"TMPDIR=" + os.TempDir(), "PATH=" + os.Getenv("PATH"),
		"LANG=C", "LC_ALL=C", "TZ=UTC", "GIT_ATTR_NOSYSTEM=1",
		"GIT_LITERAL_PATHSPECS=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_NO_LAZY_FETCH=1",
		"GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=4", "GIT_CONFIG_KEY_0=core.fsmonitor", "GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=core.untrackedcache", "GIT_CONFIG_VALUE_1=false",
		"GIT_CONFIG_KEY_2=core.hookspath", "GIT_CONFIG_VALUE_2=" + os.DevNull,
		"GIT_CONFIG_KEY_3=diff.suppressBlankEmpty", "GIT_CONFIG_VALUE_3=false",
	}
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func validCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, valueRune := range value {
		if valueRune < '0' || valueRune > '9' {
			if valueRune < 'a' || valueRune > 'f' {
				return false
			}
		}
	}
	return true
}

func safeRelative(path string) bool {
	if path == "" || strings.ContainsRune(path, 0) || filepath.IsAbs(filepath.FromSlash(path)) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	return clean == path && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func countReview(records []Hunk) int {
	count := 0
	for _, record := range records {
		if record.ReviewReason != "" {
			count++
		}
	}
	return count
}
