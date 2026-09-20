// Command t422q-shadow projects authenticated returned bundles and runs the
// resulting source-free episodes through the non-gating Jev shadow evaluator.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bmeddeb/phebs/spike/t422q"
)

const maxAllowlistBytes = 1 << 20

func main() {
	if len(os.Args) < 2 {
		fail("expected project, classify, or evaluate")
	}
	var err error
	switch os.Args[1] {
	case "project":
		err = project(os.Args[2:])
	case "classify":
		err = classify(os.Args[2:])
	case "evaluate":
		err = evaluate(os.Args[2:])
	default:
		fail("unknown operation %q", os.Args[1])
	}
	if err != nil {
		fail("%v", err)
	}
}

func evaluate(arguments []string) error {
	flags := flag.NewFlagSet("evaluate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	episodesPath := flags.String("episodes", "", "absolute projected episode JSONL path")
	predictionsPath := flags.String("predictions", "", "absolute Jev prediction JSONL path")
	labelsPath := flags.String("labels", "", "absolute adjudicated human-label JSONL path")
	outputPath := flags.String("output", "", "absolute create-only calibration report path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *episodesPath == "" ||
		*predictionsPath == "" || *labelsPath == "" || *outputPath == "" {
		return errors.New("evaluate requires -episodes, -predictions, -labels, and -output")
	}
	episodesFile, err := openRegular(*episodesPath)
	if err != nil {
		return fmt.Errorf("open episodes: %w", err)
	}
	episodes, err := t422q.DecodeEpisodes(episodesFile)
	if closeErr := episodesFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("decode episodes: %w", err)
	}
	predictionsFile, err := openRegular(*predictionsPath)
	if err != nil {
		return fmt.Errorf("open predictions: %w", err)
	}
	predictions, err := t422q.DecodePredictions(predictionsFile)
	if closeErr := predictionsFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("decode predictions: %w", err)
	}
	labelsFile, err := openRegular(*labelsPath)
	if err != nil {
		return fmt.Errorf("open labels: %w", err)
	}
	labels, err := t422q.DecodeHumanLabels(labelsFile)
	if closeErr := labelsFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("decode human labels: %w", err)
	}
	report, err := t422q.EvaluateCalibration(episodes, predictions, labels)
	if err != nil {
		return err
	}
	if err := writePrivateCreateOnly(*outputPath, func(writer io.Writer) error {
		encoder := json.NewEncoder(writer)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(report)
	}); err != nil {
		return fmt.Errorf("write calibration report: %w", err)
	}
	_, _ = fmt.Fprintf(
		os.Stdout,
		"shadow_go=%t test_receipt_groups=%d false_benign_receipt_groups=%d\n",
		report.ShadowGO,
		report.TestReceiptGroups,
		report.FalseBenignReceiptGroups,
	)
	return nil
}

func project(arguments []string) error {
	flags := flag.NewFlagSet("project", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	allowlistPath := flags.String("allowlist", "", "absolute reviewed bundle allowlist path, or - for stdin")
	outputPath := flags.String("output", "", "absolute create-only episode JSONL path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *allowlistPath == "" || *outputPath == "" {
		return errors.New("project requires -allowlist and -output")
	}
	var raw []byte
	var err error
	if *allowlistPath == "-" {
		raw, err = readBounded(os.Stdin, maxAllowlistBytes)
	} else {
		raw, err = readRegularBounded(*allowlistPath, maxAllowlistBytes)
	}
	if err != nil {
		return fmt.Errorf("read reviewed allowlist: %w", err)
	}
	episodes, err := t422q.ProjectCorpus(raw)
	if err != nil {
		return err
	}
	if err := writePrivateCreateOnly(*outputPath, func(writer io.Writer) error {
		return t422q.EncodeEpisodes(writer, episodes)
	}); err != nil {
		return fmt.Errorf("write projected episodes: %w", err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "projected %d source-free episodes\n", len(episodes))
	return nil
}

func classify(arguments []string) error {
	flags := flag.NewFlagSet("classify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	episodesPath := flags.String("episodes", "", "absolute projected episode JSONL path")
	outputPath := flags.String("output", "", "absolute create-only prediction JSONL path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *episodesPath == "" || *outputPath == "" {
		return errors.New("classify requires -episodes and -output")
	}
	input, err := openRegular(*episodesPath)
	if err != nil {
		return fmt.Errorf("open episodes: %w", err)
	}
	episodes, decodeErr := t422q.DecodeEpisodes(input)
	closeErr := input.Close()
	if decodeErr != nil {
		return decodeErr
	}
	if closeErr != nil {
		return fmt.Errorf("close episodes: %w", closeErr)
	}
	key := os.Getenv("JEV_KEY")
	if key == "" {
		return errors.New("JEV_KEY is not available to this process")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	predictions, err := t422q.ClassifyEpisodes(ctx, key, episodes)
	if err != nil {
		return err
	}
	if err := writePrivateCreateOnly(*outputPath, func(writer io.Writer) error {
		return t422q.EncodePredictions(writer, predictions)
	}); err != nil {
		return fmt.Errorf("write predictions: %w", err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "classified %d source-free episodes with %s\n", len(predictions), t422q.JevModel)
	return nil
}

func readRegularBounded(path string, maximum int64) ([]byte, error) {
	file, err := openRegular(path)
	if err != nil {
		return nil, err
	}
	raw, readErr := readBounded(file, maximum)
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return raw, nil
}

func readBounded(reader io.Reader, maximum int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || int64(len(raw)) > maximum {
		return nil, errors.New("input is outside its fixed byte bound")
	}
	return raw, nil
}

func openRegular(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("path must be canonical absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("path must name a regular file, not a symlink")
	}
	return os.Open(path)
}

func writePrivateCreateOnly(path string, write func(io.Writer) error) (retErr error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("output path must be canonical absolute")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
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

func fail(format string, arguments ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "t422q-shadow: "+format+"\n", arguments...)
	os.Exit(1)
}
