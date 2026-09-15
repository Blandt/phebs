//go:build darwin

package t4013

import (
	"os"
	"slices"
	"testing"

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
	for _, invalid := range []int{0, -1} {
		if _, err := PrivateProcessSessionMembership(invalid); err == nil {
			t.Fatalf("invalid session %d was accepted", invalid)
		}
	}
	sessionID, err := unix.Getsid(os.Getpid())
	if err != nil {
		t.Fatal(err)
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
}
