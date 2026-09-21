// Command t422r-sweep emits an offline, deterministic JSONL census of HTTP
// error sites reachable from the T40.13 polled endpoints.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/bmeddeb/phebs/spike/t422r"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "t422r-sweep: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	if len(arguments) == 0 || arguments[0] != "census" {
		return errors.New("expected census")
	}
	flags := flag.NewFlagSet("census", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 {
		return errors.New("census accepts only -root")
	}
	ctx := context.Background()
	canonicalRoot, commit, err := cleanSource(ctx, *root)
	if err != nil {
		return err
	}
	records, err := t422r.Census(ctx, canonicalRoot, commit)
	if err != nil {
		return err
	}
	_, verifiedCommit, err := cleanSource(ctx, canonicalRoot)
	if err != nil {
		return fmt.Errorf("reverify source after census: %w", err)
	}
	if verifiedCommit != commit {
		return errors.New("source commit changed during census")
	}
	return t422r.EncodeJSONL(output, commit, records)
}

func cleanSource(ctx context.Context, root string) (string, string, error) {
	return t422r.VerifyCleanSource(ctx, root)
}
