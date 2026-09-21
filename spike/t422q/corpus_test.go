package t422q

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeReviewedBundleAllowlist(t *testing.T) {
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)
	raw := []byte(fmt.Sprintf(
		`{"schema":%q,"bundles":[{"package_path":"/second.tgz","package_digest":%q},{"package_path":"/first.tgz","package_digest":%q}]}`,
		ReviewedBundleAllowlistSchema,
		digestB,
		digestA,
	))
	decoded, err := decodeReviewedBundleAllowlist(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Bundles) != 2 || decoded.Bundles[0].PackageDigest != digestA || decoded.Bundles[1].PackageDigest != digestB {
		t.Fatalf("bundle order = %#v", decoded.Bundles)
	}

	invalid := []string{
		fmt.Sprintf(`{"schema":%q,"bundles":[{"package_path":"/a.tgz","package_digest":%q}],"extra":true}`, ReviewedBundleAllowlistSchema, digestA),
		fmt.Sprintf(`{"schema":%q,"bundles":[{"package_path":"relative.tgz","package_digest":%q}]}`, ReviewedBundleAllowlistSchema, digestA),
		fmt.Sprintf(`{"schema":%q,"bundles":[{"package_path":"/a.tgz","package_digest":%q},{"package_path":"/b.tgz","package_digest":%q}]}`, ReviewedBundleAllowlistSchema, digestA, digestA),
	}
	for _, input := range invalid {
		if _, err := decodeReviewedBundleAllowlist([]byte(input)); err == nil {
			t.Fatalf("accepted invalid allowlist: %s", input)
		}
	}
}

func TestProjectCorpusSurfacesCleanupFailure(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	missing := filepath.Join(t.TempDir(), "missing.tgz")
	raw := []byte(fmt.Sprintf(
		`{"schema":%q,"bundles":[{"package_path":%q,"package_digest":%q}]}`,
		ReviewedBundleAllowlistSchema,
		missing,
		digest,
	))
	cleanupFailure := errors.New("cleanup failure")
	var removed string
	_, err := projectCorpus(raw, func(path string) error {
		removed = path
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Errorf("stat extraction root: %v", statErr)
		} else if info.Mode().Perm() != 0o700 {
			t.Errorf("extraction root mode = %o, want 700", info.Mode().Perm())
		}
		if removeErr := os.RemoveAll(path); removeErr != nil {
			t.Errorf("remove extraction root: %v", removeErr)
		}
		return cleanupFailure
	})
	if !errors.Is(err, cleanupFailure) {
		t.Fatalf("project error = %v, want cleanup failure", err)
	}
	if removed == "" {
		t.Fatal("cleanup was not called")
	}
	if _, statErr := os.Stat(removed); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("extraction root remains after cleanup: %v", statErr)
	}
}
