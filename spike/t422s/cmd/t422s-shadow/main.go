// Command t422s-shadow projects exact-clean diff hunks and, after a separate
// reviewed allowlist, obtains non-gating Jev hazard scores.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bmeddeb/phebs/spike/t422s"
	"golang.org/x/sys/unix"
)

const maxAllowlistBytes = 4 << 20

func main() {
	if len(os.Args) < 2 {
		fail("expected project or classify")
	}
	var err error
	switch os.Args[1] {
	case "project":
		err = project(os.Args[2:])
	case "classify":
		err = classify(os.Args[2:])
	default:
		err = fmt.Errorf("unknown operation %q", os.Args[1])
	}
	if err != nil {
		fail("%v", err)
	}
}

func project(arguments []string) error {
	flags := flag.NewFlagSet("project", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "exact-clean repository root")
	base := flags.String("base", "", "exact lowercase 40-character base commit")
	output := flags.String("output", "", "absolute create-only private hunk JSONL path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *base == "" || *output == "" {
		return errors.New("project requires -base and -output, with optional -root")
	}
	ctx := context.Background()
	canonicalRoot, head, hunks, err := t422s.Census(ctx, *root, *base)
	if err != nil {
		return err
	}
	if err := writePrivateCreateOnly(canonicalRoot, *output, func(writer io.Writer) error {
		return t422s.EncodeHunks(writer, *base, head, hunks)
	}); err != nil {
		return fmt.Errorf("write projected hunks: %w", err)
	}
	review := 0
	for _, hunk := range hunks {
		if hunk.ReviewReason != "" {
			review++
		}
	}
	_, _ = fmt.Fprintf(os.Stdout, "projected %d hunks: %d eligible, %d review\n", len(hunks), len(hunks)-review, review)
	return nil
}

func classify(arguments []string) error {
	flags := flag.NewFlagSet("classify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "exact-clean repository root")
	base := flags.String("base", "", "exact lowercase 40-character base commit")
	allowlistPath := flags.String("allowlist", "", "absolute reviewed allowlist JSON path")
	output := flags.String("output", "", "absolute create-only private prediction JSONL path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *base == "" ||
		*allowlistPath == "" || *output == "" {
		return errors.New("classify requires -base, -allowlist, and -output, with optional -root")
	}
	allowlistRaw, err := readRegularBounded(*allowlistPath, maxAllowlistBytes)
	if err != nil {
		return fmt.Errorf("read reviewed allowlist: %w", err)
	}
	allowlist, err := t422s.DecodeAllowlist(bytes.NewReader(allowlistRaw))
	if err != nil {
		return err
	}
	ctx := context.Background()
	canonicalRoot, head, hunks, err := t422s.Census(ctx, *root, *base)
	if err != nil {
		return err
	}
	reviewed, err := t422s.SelectReviewed(hunks, allowlist, *base, head)
	if err != nil {
		return err
	}
	key := os.Getenv("JEV_KEY")
	if key == "" {
		return errors.New("JEV_KEY is not available to this process")
	}
	classified := 0
	if err := writePrivateCreateOnly(canonicalRoot, *output, func(writer io.Writer) error {
		classifyCtx, cancel := context.WithTimeout(
			context.Background(), time.Duration(len(reviewed))*30*time.Second+time.Minute,
		)
		defer cancel()
		predictions, err := t422s.Classify(classifyCtx, key, reviewed)
		if err != nil {
			return err
		}
		if err := t422s.VerifyCleanPair(context.Background(), canonicalRoot, *base, head); err != nil {
			return fmt.Errorf("reverify source after classification: %w", err)
		}
		if err := t422s.EncodePredictions(writer, predictions); err != nil {
			return err
		}
		classified = len(predictions)
		return nil
	}); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "classified %d reviewed hunks with %s contract %s\n",
		classified, t422s.JevModel, t422s.ContractSHA256())
	return nil
}

func openRegular(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("path must be canonical absolute")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("path must name a regular file, not a symlink")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open regular file returned an invalid descriptor")
	}
	opened, statErr := file.Stat()
	current, pathErr := os.Lstat(path)
	if statErr != nil || pathErr != nil || !opened.Mode().IsRegular() || !current.Mode().IsRegular() ||
		!os.SameFile(before, opened) || !os.SameFile(opened, current) {
		return nil, errors.Join(errors.New("regular file identity changed while opening"), statErr, pathErr, file.Close())
	}
	return file, nil
}

func readRegularBounded(path string, maximum int64) ([]byte, error) {
	file, err := openRegular(path)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maximum+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	if len(raw) == 0 || int64(len(raw)) > maximum {
		return nil, errors.New("input is outside its fixed byte bound")
	}
	return raw, nil
}

func writePrivateCreateOnly(root, path string, write func(io.Writer) error) (retErr error) {
	target, err := outputTarget(root, path)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			retErr = errors.Join(retErr, closeErr)
			keep = false
		}
		if !keep {
			if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				retErr = errors.Join(retErr, removeErr)
			}
		}
	}()
	if err := write(file); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	keep = true
	return nil
}

func outputTarget(root, path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("output path must be canonical absolute")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("resolve output parent: %w", err)
	}
	target := filepath.Join(parent, filepath.Base(path))
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	relative, err := filepath.Rel(canonicalRoot, target)
	if err != nil {
		return "", fmt.Errorf("compare output with repository root: %w", err)
	}
	if relative == "." || relative != ".." && !filepath.IsAbs(relative) && !hasDotDotPrefix(relative) {
		return "", errors.New("output must be outside the source repository")
	}
	return target, nil
}

func hasDotDotPrefix(path string) bool {
	return path == ".." || len(path) > 3 && path[:3] == ".."+string(filepath.Separator)
}

func fail(format string, arguments ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "t422s-shadow: "+format+"\n", arguments...)
	os.Exit(1)
}
