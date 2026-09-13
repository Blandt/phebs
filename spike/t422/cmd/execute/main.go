package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bmeddeb/phebs/spike/t421"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if t421.RunExecutionCommand(ctx, os.Args, os.Environ()) != nil {
		_, _ = fmt.Fprintln(os.Stderr, "t422-execute: authenticated execution operation unavailable")
		os.Exit(1)
	}
}
