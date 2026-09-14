package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCommandSignalContext(t *testing.T) {
	const variable = "PHEBS_TEST_COMMAND_SIGNAL_CONTEXT"
	if mode := os.Getenv(variable); mode != "" {
		if mode == "version" {
			fmt.Println("draining")
			// Fill the real stdout pipe during runPhebs, so the parent can
			// prove that ordinary output remains terminable by SIGTERM.
			version = strings.Repeat("v", 1<<20)
			_, err := runPhebs([]string{"version"})
			t.Fatalf("version output unexpectedly completed: %v", err)
		}
		parent, cancelParent := context.WithCancel(context.Background())
		defer cancelParent()
		ctx, cancel, stopSignals := commandSignalContext(parent)
		defer stopSignals()
		defer cancel()
		switch mode {
		case "work_cancel":
			cancel()
		case "command_return":
			func() {
				work, cancelWork := context.WithCancel(ctx)
				defer cancelWork()
				ctx = work
			}()
		case "parent_cancel":
			cancelParent()
		case "cleanup_finished":
			stopSignals()
		default:
			t.Fatal("unknown helper mode")
		}
		<-ctx.Done()
		fmt.Println("draining")
		var token [1]byte
		if _, err := io.ReadFull(os.Stdin, token[:]); err != nil {
			t.Fatal(err)
		}
		fmt.Println("drained")
		return
	}
	for _, mode := range []string{"work_cancel", "command_return", "parent_cancel", "cleanup_finished", "version"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCommandSignalContext$")
			command.Env = append(os.Environ(), variable+"="+mode)
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			command.Stderr = os.Stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if command.ProcessState == nil {
					_ = command.Process.Kill()
					_ = command.Wait()
				}
			}()
			reader := bufio.NewReader(output)
			if line, err := reader.ReadString('\n'); err != nil || line != "draining\n" {
				_ = command.Process.Kill()
				_ = command.Wait()
				t.Fatalf("helper did not enter cleanup: %q, %v", line, err)
			}
			if mode == "version" {
				if first, err := reader.ReadByte(); err != nil || first != 'v' {
					t.Fatalf("version did not enter output: %q %v", first, err)
				}
			}
			if err := command.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			if mode == "cleanup_finished" || mode == "version" {
				if err := command.Wait(); err == nil || ctx.Err() != nil || command.ProcessState == nil ||
					command.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
					t.Fatal("completed cleanup retained signal interception")
				}
				return
			}
			// Allow the real signal to arrive while the helper is blocked in
			// cleanup, before permitting the simulated database join to finish.
			time.Sleep(50 * time.Millisecond)
			_, writeErr := input.Write([]byte{'x'})
			line, readErr := reader.ReadString('\n')
			if err := command.Wait(); err != nil || writeErr != nil || readErr != nil || line != "drained\n" {
				t.Fatalf("SIGTERM interrupted cleanup: %v, %v, %v, %q", err, writeErr, readErr, line)
			}
		})
	}
}
