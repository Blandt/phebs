package t421

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	executionOuterMode              = "run-t422-outer"
	executionInnerMode              = "run-t422-inner"
	executionLivenessEnvironment    = "PHEBS_T422_PARENT_LIVENESS_V1"
	executionSelectionSchema        = "t422-execution-selection-v1"
	executionParentLivenessSchema   = "t422-parent-liveness-binding-v1"
	maxExecutionSelectionBytes      = 16 << 10
	maxExecutionSelectionCharacters = 21_846
	maxExecutionLivenessBytes       = 1 << 10
	executionMaximumWall            = 18 * time.Hour
)

var (
	ErrExecutionLauncher         = errors.New("T42.2 execution launcher unavailable")
	errExecutionAuthorityPending = errors.New("T42.2 execution authority is not implemented")
	executionCeremonyID          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

type executionSelectionV1 struct {
	Schema               string `json:"schema"`
	CeremonyID           string `json:"ceremony_id"`
	RepositoryRoot       string `json:"repository_root"`
	PlanSourceCommit     string `json:"plan_source_commit"`
	IntegratedMainCommit string `json:"integrated_main_commit"`
	SourceCommit         string `json:"source_commit"`
	GoRoot               string `json:"go_root"`
	ModuleCache          string `json:"module_cache"`
	GitBinary            string `json:"git_binary"`
	SurrealBinary        string `json:"surreal_binary"`
	SignerControlRoot    string `json:"signer_control_root"`
}

type executionParentLivenessV1 struct {
	Schema                string `json:"schema"`
	OuterPID              int    `json:"outer_pid"`
	OuterStartToken       string `json:"outer_start_token"`
	OuterStartedUnixNano  int64  `json:"outer_started_unix_nano"`
	OuterDeadlineUnixNano int64  `json:"outer_deadline_unix_nano"`
	ReadFD                int    `json:"read_fd"`
	ReadDevice            int64  `json:"read_st_dev"`
	ReadInode             uint64 `json:"read_st_ino"`
	ReadMode              uint32 `json:"read_st_mode"`
	WriteDevice           int64  `json:"write_st_dev"`
	WriteInode            uint64 `json:"write_st_ino"`
	WriteMode             uint32 `json:"write_st_mode"`
	ExecutePathSHA256     string `json:"t422_execute_canonical_path_sha256"`
	ExecuteDevice         int64  `json:"t422_execute_st_dev"`
	ExecuteInode          uint64 `json:"t422_execute_st_ino"`
	ExecuteMode           uint32 `json:"t422_execute_st_mode"`
	ExecuteSize           int64  `json:"t422_execute_size"`
	ExecuteCTimeUnixNano  int64  `json:"t422_execute_ctime_unix_nano"`
	ExecuteImageSHA256    string `json:"t422_execute_image_sha256"`
}

// RunExecutionCommand is the only t422-execute command entry. Selection is
// caller input, never authority; later execution starts only after inner mode
// independently reconstructs its protected custodies.
func RunExecutionCommand(ctx context.Context, args, environment []string) error {
	entered := time.Now()
	if ctx == nil || entered.UnixNano() <= 0 || len(args) != 4 || args[2] != "--selection-base64url" {
		return ErrExecutionLauncher
	}
	switch args[1] {
	case executionOuterMode:
		return runExecutionOuter(ctx, entered, args[0], args[3], environment)
	case executionInnerMode:
		return runExecutionInner(ctx, entered, args[0], args[3], environment)
	default:
		return ErrExecutionLauncher
	}
}

func executionSelection(encoded string) (executionSelectionV1, error) {
	if len(encoded) == 0 || len(encoded) > maxExecutionSelectionCharacters {
		return executionSelectionV1{}, ErrExecutionLauncher
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) == 0 || len(raw) > maxExecutionSelectionBytes {
		return executionSelectionV1{}, ErrExecutionLauncher
	}
	var selection executionSelectionV1
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&selection) != nil || !executionJSONEOF(decoder) {
		return executionSelectionV1{}, ErrExecutionLauncher
	}
	canonical, err := json.Marshal(selection)
	if err != nil || !bytes.Equal(raw, canonical) || base64.RawURLEncoding.EncodeToString(canonical) != encoded || !validExecutionSelection(selection) {
		return executionSelectionV1{}, ErrExecutionLauncher
	}
	return selection, nil
}

func validExecutionSelection(value executionSelectionV1) bool {
	if value.Schema != executionSelectionSchema || !executionCeremonyID.MatchString(value.CeremonyID) || value.CeremonyID == "." || value.CeremonyID == ".." ||
		!validExecutionCommit(value.PlanSourceCommit) || !validExecutionCommit(value.IntegratedMainCommit) || !validExecutionCommit(value.SourceCommit) {
		return false
	}
	directories := []string{value.RepositoryRoot, value.GoRoot, value.ModuleCache, value.SignerControlRoot}
	files := []string{value.GitBinary, value.SurrealBinary}
	for _, path := range append(append([]string(nil), directories...), files...) {
		if len(path) == 0 || len(path) > 1_023 || !utf8.ValidString(path) || strings.ContainsRune(path, 0) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return false
		}
	}
	for index, path := range directories {
		for _, other := range directories[index+1:] {
			if executionPathContains(path, other) || executionPathContains(other, path) {
				return false
			}
		}
		for _, file := range files {
			if executionPathContains(path, file) {
				return false
			}
		}
	}
	return files[0] != files[1]
}

func validExecutionCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, digit := range []byte(value) {
		if digit < '0' || digit > '9' {
			if digit < 'a' || digit > 'f' {
				return false
			}
		}
	}
	return true
}

func validExecutionHexSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, digit := range []byte(value) {
		if digit < '0' || digit > '9' {
			if digit < 'a' || digit > 'f' {
				return false
			}
		}
	}
	return true
}

func executionPathContains(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func executionJSONEOF(decoder *json.Decoder) bool {
	var extra any
	return errors.Is(decoder.Decode(&extra), io.EOF)
}

func decodeExecutionLiveness(encoded string) (executionParentLivenessV1, error) {
	if len(encoded) == 0 || len(encoded) > base64.RawURLEncoding.EncodedLen(maxExecutionLivenessBytes) {
		return executionParentLivenessV1{}, ErrExecutionLauncher
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) == 0 || len(raw) > maxExecutionLivenessBytes {
		return executionParentLivenessV1{}, ErrExecutionLauncher
	}
	var value executionParentLivenessV1
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || !executionJSONEOF(decoder) {
		return executionParentLivenessV1{}, ErrExecutionLauncher
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(raw, canonical) || base64.RawURLEncoding.EncodeToString(canonical) != encoded {
		return executionParentLivenessV1{}, ErrExecutionLauncher
	}
	return value, nil
}
