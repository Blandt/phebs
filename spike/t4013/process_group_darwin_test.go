//go:build darwin

package t4013

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDarwinSessionInventoryUsesNativeRecords(t *testing.T) {
	pids, err := darwinAllProcessPIDs()
	if err != nil || !slices.Contains(pids, os.Getpid()) {
		t.Fatalf("native host PIDs = %v, %v", pids, err)
	}
	status, present, err := darwinProcessStatus(os.Getpid())
	if err != nil || !present || status == darwinProcessZombie {
		t.Fatalf("native process status = %d, present = %t, %v", status, present, err)
	}
	sessionID, err := unix.Getsid(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	session, err := privateServerSessionPIDs(sessionID)
	if err != nil || !slices.Contains(session, os.Getpid()) {
		t.Fatalf("native session PIDs = %v, %v", session, err)
	}
}

func TestPrivateProcessSessionMembershipNamesMembers(t *testing.T) {
	const (
		helper = "T4013_SESSION_MEMBERSHIP_HELPER"
		marker = "t4013-session-membership-passed\n"
	)
	if os.Getenv(helper) != "1" {
		// A Terminal session may contain a protected login process, not ours.
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPrivateProcessSessionMembershipNamesMembers$", "-test.count=1")
		command.Env = append(os.Environ(), helper+"=1")
		output, err := runCustodyCombinedOutput(command)
		if err != nil {
			t.Fatalf("isolated session membership: %v\n%s", err, output)
		}
		if !bytes.Contains(output, []byte(marker)) {
			t.Fatalf("isolated session membership did not complete:\n%s", output)
		}
		return
	}
	for _, invalid := range []int{0, -1} {
		if _, err := PrivateProcessSessionMembership(invalid); err == nil {
			t.Fatalf("invalid session %d was accepted", invalid)
		}
	}
	sessionID, err := unix.Getsid(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != os.Getpid() {
		t.Fatalf("membership fixture did not own its session: session=%d pid=%d", sessionID, os.Getpid())
	}
	members, err := PrivateProcessSessionMembership(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	self := -1
	for index, member := range members {
		if member.PID <= 0 || member.ParentPID < 0 || member.ParentPID == member.PID ||
			member.RSSBytes < 0 || member.ObservedName == "" || member.StartIdentity == "" {
			t.Fatalf("member %d is not an individually coherent named record: %+v", index, member)
		}
		if member.PID == os.Getpid() {
			self = index
		}
	}
	if self < 0 {
		t.Fatalf("own session membership does not name the test process: %+v", members)
	}
	if _, err := os.Stdout.WriteString(marker); err != nil {
		t.Fatal(err)
	}
}
