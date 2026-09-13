//go:build darwin

package t421

import (
	"context"
	"os"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/bmeddeb/phebs/spike/t4013"
	"golang.org/x/sys/unix"
)

// observeExecutionAuthorizationSessionBinding derives every process, image and
// listener field from live held custody. Caller strings contribute only the
// already-signed freeze digest and ceremony identifier.
func observeExecutionAuthorizationSessionBinding(
	ctx context.Context,
	ceremonyID, freezeSHA256 string,
	parent *executionParentLiveness,
	wait *executionAuthorizationWait,
	outerDeadline, finalAdmissionDeadline time.Time,
) (executionAuthorizationSessionBinding, error) {
	if ctx == nil || ctx.Err() != nil || parent == nil || parent.alive == nil || parent.alive.Err() != nil ||
		parent.image == nil || wait == nil || !time.Now().Before(finalAdmissionDeadline) {
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}
	outerDeadlineUnixNano, finalDeadlineUnixNano := outerDeadline.UnixNano(), finalAdmissionDeadline.UnixNano()
	if outerDeadlineUnixNano <= 0 || finalDeadlineUnixNano <= 0 || finalDeadlineUnixNano > outerDeadlineUnixNano {
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}

	rows, err := t4013.ObserveProcessTreeRecords(ctx, parent.outer.PID)
	if err != nil || len(rows) != 2 || rows[0].PID != parent.outer.PID || rows[0].StartIdentity != parent.outer.StartIdentity ||
		rows[1].PID != parent.inner.PID || rows[1].ParentPID != parent.outer.PID || rows[1].StartIdentity != parent.inner.StartIdentity ||
		rows[1].PID != os.Getpid() || rows[1].ParentPID != os.Getppid() ||
		!validExecutionAuthorizationStartToken(rows[0].StartIdentity) || !validExecutionAuthorizationStartToken(rows[1].StartIdentity) {
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}
	innerSession, sessionErr := unix.Getsid(rows[1].PID)
	innerGroup, groupErr := syscall.Getpgid(rows[1].PID)
	outerSession, outerSessionErr := unix.Getsid(rows[0].PID)
	outerGroup, outerGroupErr := syscall.Getpgid(rows[0].PID)
	if sessionErr != nil || groupErr != nil || outerSessionErr != nil || outerGroupErr != nil ||
		innerSession != rows[1].PID || innerGroup != rows[1].PID || outerSession <= 0 || outerGroup <= 0 ||
		outerSession == innerSession || outerGroup == innerGroup {
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}

	parent.image.mu.Lock()
	if parent.image.checkLocked(ctx) != nil {
		parent.image.mu.Unlock()
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}
	imagePath, imagePathSHA256 := parent.image.path, parent.image.pathSHA256
	imageDevice, imageInode, imageMode := parent.image.device, parent.image.inode, parent.image.mode
	imageSize, imageCTime, imageSHA256 := parent.image.size, parent.image.ctimeUnixNano, parent.image.digest
	parent.image.mu.Unlock()
	if imagePathSHA256 != executionAuthorizationSHA256([]byte(imagePath)) {
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}

	wait.mu.Lock()
	if wait.state != executionAuthorizationReady || wait.listener < 0 || !wait.linked || !wait.listenerCloseOnExec ||
		wait.path == "" || !validExecutionAuthorizationSocketPath(wait.path) || pressureRootsUnchanged(wait.root) != nil {
		wait.mu.Unlock()
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}
	socket, socketErr := observeExecutionAuthorizationSocket(wait.root)
	rootInfo, rootErr := wait.root.file.Stat()
	if socketErr != nil || socket != wait.socket || rootErr != nil || rootInfo == nil {
		wait.mu.Unlock()
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}
	rootStat, rootOK := rootInfo.Sys().(*syscall.Stat_t)
	if !rootOK || rootStat == nil || int64(rootStat.Dev) < 0 ||
		rootStat.Ino == 0 || socket.device < 0 || socket.inode == 0 || socket.mode != uint16(unix.S_IFSOCK|0o600) {
		wait.mu.Unlock()
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}
	socketPath := wait.path
	wait.mu.Unlock()

	value := executionAuthorizationSessionBindingPreimageV1{
		Schema: executionAuthorizationSessionBindingSchema, CeremonyID: ceremonyID, FreezeSHA256: freezeSHA256,
		OuterPID: int64(rows[0].PID), OuterStartToken: rows[0].StartIdentity,
		InnerPID: int64(rows[1].PID), InnerParentPID: int64(rows[1].ParentPID), InnerStartToken: rows[1].StartIdentity,
		InnerSessionID: int64(innerSession), InnerProcessGroupID: int64(innerGroup),
		T422ExecuteCanonicalPathSHA256: imagePathSHA256, T422ExecuteDevice: imageDevice, T422ExecuteInode: imageInode,
		T422ExecuteMode: imageMode, T422ExecuteSize: imageSize, T422ExecuteCTimeUnixNano: imageCTime,
		T422ExecuteImageSHA256: imageSHA256,
		ListenerParentDevice:   int64(rootStat.Dev), ListenerParentInode: uint64(rootStat.Ino),
		ListenerDevice: int64(socket.device), ListenerInode: socket.inode, ListenerMode: uint32(socket.mode),
		SocketPathSHA256:      executionAuthorizationSHA256([]byte(socketPath)),
		OuterDeadlineUnixNano: outerDeadlineUnixNano, FinalAdmissionDeadlineUnixNano: finalDeadlineUnixNano,
	}
	binding, err := buildExecutionAuthorizationSessionBinding(value)
	if err != nil || parent.image.Check(ctx) != nil || parent.alive.Err() != nil || ctx.Err() != nil {
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}
	wait.mu.Lock()
	current, currentErr := observeExecutionAuthorizationSocket(wait.root)
	unchanged := wait.state == executionAuthorizationReady && wait.listener >= 0 && wait.linked && wait.listenerCloseOnExec &&
		currentErr == nil && current == wait.socket
	wait.mu.Unlock()
	if !unchanged {
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}
	return binding, nil
}

func validExecutionAuthorizationStartToken(value string) bool {
	return value != "" && len(value) <= 64 && utf8.ValidString(value) && !containsExecutionAuthorizationNUL(value)
}

func containsExecutionAuthorizationNUL(value string) bool {
	for index := range value {
		if value[index] == 0 {
			return true
		}
	}
	return false
}
