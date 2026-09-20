package t422q

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/bmeddeb/phebs/spike/t4013"
)

const (
	ReviewedBundleAllowlistSchema = "t422q-reviewed-bundle-allowlist-v1"
	maxReviewedBundles            = 128
	maxReviewedBundleAllowlist    = 1 << 20
)

type ReviewedBundleAllowlist struct {
	Schema  string           `json:"schema"`
	Bundles []ReviewedBundle `json:"bundles"`
}

type ReviewedBundle struct {
	PackagePath   string `json:"package_path"`
	PackageDigest string `json:"package_digest"`
}

// ProjectCorpus authenticates only the explicitly reviewed packages in raw.
// It neither discovers packages nor trusts files adjacent to them.
func ProjectCorpus(raw []byte) ([]Episode, error) {
	return projectCorpus(raw, os.RemoveAll)
}

func projectCorpus(raw []byte, removeAll func(string) error) (episodes []Episode, retErr error) {
	allowlist, err := decodeReviewedBundleAllowlist(raw)
	if err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp("", "phebs-t422q-corpus-")
	if err != nil {
		return nil, fmt.Errorf("create corpus extraction root: %w", err)
	}
	defer func() {
		if cleanupErr := removeAll(root); cleanupErr != nil {
			episodes = nil
			retErr = errors.Join(retErr, fmt.Errorf("remove corpus extraction root: %w", cleanupErr))
		}
	}()
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("make corpus extraction root private: %w", err)
	}

	for index, bundle := range allowlist.Bundles {
		receiptGroup := fmt.Sprintf("receipt_%03d", index+1)
		extractionRoot := filepath.Join(root, receiptGroup)
		if err := os.Mkdir(extractionRoot, 0o700); err != nil {
			return nil, fmt.Errorf("create %s extraction root: %w", receiptGroup, err)
		}
		if err := t4013.ExtractReturnedBundle(
			bundle.PackagePath,
			extractionRoot,
			"",
			bundle.PackageDigest,
		); err != nil {
			return nil, fmt.Errorf("authenticate %s: %w", receiptGroup, err)
		}
		planRaw, err := os.ReadFile(filepath.Join(extractionRoot, "evidence", "plan.json"))
		if err != nil {
			return nil, fmt.Errorf("read %s plan: %w", receiptGroup, err)
		}
		plan, err := t4013.DecodePlan(planRaw)
		if err != nil {
			return nil, fmt.Errorf("decode %s plan: %w", receiptGroup, err)
		}
		receiptRaw, err := os.ReadFile(filepath.Join(extractionRoot, "evidence", "results.json"))
		if err != nil {
			return nil, fmt.Errorf("read %s receipt: %w", receiptGroup, err)
		}
		receipt, err := t4013.DecodeReceipt(receiptRaw, plan)
		if err != nil {
			return nil, fmt.Errorf("decode %s receipt: %w", receiptGroup, err)
		}
		projected, err := ProjectReceipt(receiptGroup, receipt)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", receiptGroup, err)
		}
		if len(projected) > maxEpisodes-len(episodes) {
			return nil, errors.New("projected corpus exceeds 4096-case bound")
		}
		episodes = append(episodes, projected...)
	}
	return episodes, nil
}

func decodeReviewedBundleAllowlist(raw []byte) (ReviewedBundleAllowlist, error) {
	if len(raw) == 0 || len(raw) > maxReviewedBundleAllowlist {
		return ReviewedBundleAllowlist{}, errors.New("reviewed bundle allowlist is outside its fixed byte bound")
	}
	var allowlist ReviewedBundleAllowlist
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&allowlist); err != nil {
		return ReviewedBundleAllowlist{}, fmt.Errorf("decode reviewed bundle allowlist: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ReviewedBundleAllowlist{}, errors.New("reviewed bundle allowlist has trailing JSON")
	}
	if allowlist.Schema != ReviewedBundleAllowlistSchema {
		return ReviewedBundleAllowlist{}, errors.New("reviewed bundle allowlist schema is not supported")
	}
	if len(allowlist.Bundles) == 0 || len(allowlist.Bundles) > maxReviewedBundles {
		return ReviewedBundleAllowlist{}, errors.New("reviewed bundle allowlist must contain 1 to 128 bundles")
	}
	for index, bundle := range allowlist.Bundles {
		if !filepath.IsAbs(bundle.PackagePath) || filepath.Clean(bundle.PackagePath) != bundle.PackagePath {
			return ReviewedBundleAllowlist{}, fmt.Errorf("reviewed bundle %d package path is not canonical absolute", index+1)
		}
		if !validSHA256(bundle.PackageDigest) {
			return ReviewedBundleAllowlist{}, fmt.Errorf("reviewed bundle %d package digest is not canonical sha256", index+1)
		}
	}

	sort.Slice(allowlist.Bundles, func(left, right int) bool {
		return allowlist.Bundles[left].PackageDigest < allowlist.Bundles[right].PackageDigest
	})
	seenPaths := make(map[string]struct{}, len(allowlist.Bundles))
	for index, bundle := range allowlist.Bundles {
		if index > 0 && bundle.PackageDigest == allowlist.Bundles[index-1].PackageDigest {
			return ReviewedBundleAllowlist{}, errors.New("reviewed bundle allowlist contains a duplicate package digest")
		}
		if _, duplicate := seenPaths[bundle.PackagePath]; duplicate {
			return ReviewedBundleAllowlist{}, errors.New("reviewed bundle allowlist contains a duplicate package path")
		}
		seenPaths[bundle.PackagePath] = struct{}{}
	}
	return allowlist, nil
}
