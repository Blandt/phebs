package t4013

import (
	"context"
	"errors"
)

// MaxNativeProcessRecords is the current native collector's per-census bound,
// including its root. It is not a cumulative process or executable-image cap.
const MaxNativeProcessRecords = maxProcessDescendants + 1

// NativeProcessRecord is one individually coherent kernel observation. These
// private-controller inputs contain process identities and names and must not
// be copied into source-free evidence. ObservedName is a kernel command name,
// not an executable path, image identity or digest. StartIdentity identifies a
// kernel lifetime, not an executable-image epoch.
type NativeProcessRecord struct {
	PID           int
	ParentPID     int
	RSSBytes      int64
	StartIdentity string
	ObservedName  string
}

// ObserveProcessTreeRecords returns bounded native rows without entering the
// historical sampler's cumulative executable-image bookkeeping. The collector
// reads records sequentially: this is not an atomic process-tree snapshot or
// a complete process history. Platforms without that collector fail closed.
func ObserveProcessTreeRecords(ctx context.Context, rootPID int) ([]NativeProcessRecord, error) {
	probe := nativeProcessSnapshotProbe()
	if ctx == nil || rootPID <= 0 || probe == nil {
		return nil, errors.New("native process-record observation is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pids, processes, err := probe(ctx, rootPID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nativeProcessRecords(rootPID, pids, processes)
}

func nativeProcessRecords(rootPID int, pids []int, processes map[int]processSnapshot) ([]NativeProcessRecord, error) {
	if rootPID <= 0 || len(pids) == 0 || len(pids) > MaxNativeProcessRecords ||
		pids[0] != rootPID || len(processes) != len(pids) {
		return nil, errors.New("native process-record inventory is incomplete")
	}
	result := make([]NativeProcessRecord, 0, len(pids))
	seen := make(map[int]bool, len(pids))
	for _, pid := range pids {
		process, present := processes[pid]
		if pid <= 0 || !present || seen[pid] || !process.coherent ||
			process.parent < 0 || process.parent == pid || process.rssBytes < 0 ||
			len(process.identityToken) == 0 || len(process.identityToken) > 64 ||
			len(process.name) == 0 || len(process.name) > 16 ||
			pid == rootPID && process.rssBytes == 0 || pid != rootPID && !seen[process.parent] {
			return nil, errors.New("native process-record observation is incomplete")
		}
		seen[pid] = true
		result = append(result, NativeProcessRecord{PID: pid, ParentPID: process.parent, RSSBytes: process.rssBytes,
			StartIdentity: process.identityToken, ObservedName: process.name})
	}
	return result, nil
}

// PrivateProcessSessionMembership returns the exact live non-zombie membership
// of a private process session as individually coherent native records, one per
// member observed at census time. A member that exits between the session
// census and its own observation is omitted; the observation of a still-live
// member fails closed. Membership, not tree position, scopes the census, so
// orphaned members are still named. These private-controller records contain
// kernel command names and identities and must not be copied into source-free
// evidence. ObservedName is a kernel command name, not an executable path,
// image identity or digest. Platforms without the native collector fail closed.
func PrivateProcessSessionMembership(sessionID int) ([]NativeProcessRecord, error) {
	pids, err := privateServerSessionPIDs(sessionID)
	if err != nil {
		return nil, err
	}
	records := make([]NativeProcessRecord, 0, len(pids))
	for _, pid := range pids {
		observed, err := nativeMemberObservation(pid)
		if errors.Is(err, errProcessIdentityMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !observed.coherent || observed.parent < 0 || observed.parent == pid || observed.rssBytes < 0 ||
			len(observed.identityToken) == 0 || len(observed.identityToken) > 64 ||
			len(observed.name) == 0 || len(observed.name) > 16 {
			return nil, errors.New("native session-member observation is incomplete")
		}
		records = append(records, NativeProcessRecord{PID: pid, ParentPID: observed.parent,
			RSSBytes: observed.rssBytes, StartIdentity: observed.identityToken, ObservedName: observed.name})
	}
	return records, nil
}

// ProcessTreeObservation is one bounded native snapshot of a process and all
// descendants. It is shared by source-free gates that need live aggregate RSS
// or an exact post-teardown descendant count without launching a helper.
type ProcessTreeObservation struct {
	RSSBytes    int64
	Descendants int
}

// ObserveProcessTree uses the same bounded native inventory as the T40.13
// ceremony sampler. Platforms without a native probe fail closed.
func ObserveProcessTree(
	ctx context.Context,
	rootPID int,
) (ProcessTreeObservation, error) {
	probe := nativeProcessSnapshotProbe()
	if ctx == nil || rootPID <= 0 || probe == nil {
		return ProcessTreeObservation{}, errors.New("native process-tree observation is unavailable")
	}
	pids, processes, err := probe(ctx, rootPID)
	if err != nil {
		return ProcessTreeObservation{}, err
	}
	return summarizeProcessTree(rootPID, pids, processes)
}

func summarizeProcessTree(
	rootPID int,
	pids []int,
	processes map[int]processSnapshot,
) (ProcessTreeObservation, error) {
	root, rootPresent := processes[rootPID]
	if len(pids) == 0 || len(pids) > maxProcessDescendants+1 || !rootPresent || root.rssBytes <= 0 {
		return ProcessTreeObservation{}, errors.New("native process-tree observation is incomplete")
	}
	var total int64
	for _, pid := range pids {
		process, present := processes[pid]
		if !present || process.rssBytes < 0 {
			return ProcessTreeObservation{}, errors.New("native process-tree record is incomplete")
		}
		next, err := checkedAddInt64(total, process.rssBytes)
		if err != nil {
			return ProcessTreeObservation{}, err
		}
		total = next
	}
	return ProcessTreeObservation{RSSBytes: total, Descendants: len(pids) - 1}, nil
}
