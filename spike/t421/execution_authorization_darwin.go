//go:build darwin

package t421

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type executionAuthorizationSocketIdentity struct {
	device int32
	inode  uint64
	mode   uint16
	uid    uint32
}

type executionAuthorizationWait struct {
	mu                  sync.Mutex
	root                productionRoot
	path                string
	socket              executionAuthorizationSocketIdentity
	deadline            time.Time
	listener            int
	connection          int
	state               uint8
	interrupted         bool
	linked              bool
	listenerCloseOnExec bool
}

const (
	executionAuthorizationReady uint8 = iota + 1
	executionAuthorizationAccepting
	executionAuthorizationSpent
	executionAuthorizationClosed
)

func prepareExecutionAuthorization(ctx context.Context, root productionRoot, deadline time.Time) (_ *executionAuthorizationWait, retErr error) {
	if ctx == nil || ctx.Err() != nil || root.file == nil || root.info == nil || !time.Now().Before(deadline) ||
		pressureRootsUnchanged(root) != nil {
		return nil, errExecutionAuthorization
	}
	path := filepath.Join(root.path, executionAuthorizationSocketName)
	if !validExecutionAuthorizationSocketPath(path) || filepath.Dir(path) != root.path {
		return nil, errExecutionAuthorization
	}
	var existing unix.Stat_t
	if err := unix.Fstatat(int(root.file.Fd()), executionAuthorizationSocketName, &existing, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		return nil, errExecutionAuthorization
	}
	listener, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, errExecutionAuthorization
	}
	wait := &executionAuthorizationWait{
		root: root, path: path, deadline: deadline, listener: listener, connection: -1,
		state: executionAuthorizationReady,
	}
	defer func() {
		if retErr != nil {
			_ = wait.close()
		}
	}()
	syscall.CloseOnExec(listener)
	flags, flagErr := unix.FcntlInt(uintptr(listener), unix.F_GETFD, 0)
	wait.listenerCloseOnExec = flagErr == nil && flags&unix.FD_CLOEXEC != 0
	if !wait.listenerCloseOnExec || pressureRootsUnchanged(root) != nil || ctx.Err() != nil ||
		unix.Bind(listener, &unix.SockaddrUnix{Name: path}) != nil {
		return wait, errExecutionAuthorization
	}
	wait.linked = true
	identity, err := observeExecutionAuthorizationSocket(root)
	if err != nil {
		return wait, errExecutionAuthorization
	}
	wait.socket = identity
	if err := unix.Fchmodat(int(root.file.Fd()), executionAuthorizationSocketName, 0o600, 0); err != nil {
		return wait, errExecutionAuthorization
	}
	identity, err = observeExecutionAuthorizationSocket(root)
	if err != nil || identity.mode != uint16(unix.S_IFSOCK|0o600) {
		return wait, errExecutionAuthorization
	}
	wait.socket = identity
	if pressureRootsUnchanged(root) != nil || unix.Listen(listener, 1) != nil || pressureRootsUnchanged(root) != nil {
		return wait, errExecutionAuthorization
	}
	identity, err = observeExecutionAuthorizationSocket(root)
	if err != nil || identity != wait.socket || ctx.Err() != nil || !time.Now().Before(deadline) {
		return wait, errExecutionAuthorization
	}
	return wait, nil
}

func (wait *executionAuthorizationWait) consume(ctx context.Context, expected executionAuthorizationV1) (_ executionAuthorizationPeer, retErr error) {
	canonical, err := canonicalExecutionAuthorization(expected)
	if wait == nil {
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	if ctx == nil || ctx.Err() != nil || err != nil {
		_ = wait.close()
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	wait.mu.Lock()
	if wait.state != executionAuthorizationReady || !time.Now().Before(wait.deadline) {
		wait.mu.Unlock()
		_ = wait.close()
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	wait.state = executionAuthorizationAccepting
	listener := wait.listener
	wait.mu.Unlock()

	lifetime, cancel := context.WithDeadline(ctx, wait.deadline)
	stop := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-lifetime.Done():
			wait.mu.Lock()
			wait.interrupted = true
			wait.closeCurrentLocked()
			wait.mu.Unlock()
		case <-stop:
		}
	}()
	defer func() {
		wait.mu.Lock()
		wait.closeCurrentLocked()
		wait.mu.Unlock()
		close(stop)
		<-joined
		cancel()
		if closeErr := wait.close(); closeErr != nil {
			retErr = errExecutionAuthorization
		}
	}()

	connection, _, acceptErr := unix.Accept(listener)
	if acceptErr != nil {
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	wait.mu.Lock()
	if wait.state != executionAuthorizationAccepting {
		wait.mu.Unlock()
		_ = unix.Close(connection)
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	wait.state = executionAuthorizationSpent
	wait.connection = connection
	listenerToClose := wait.listener
	wait.listener = -1
	interrupted := wait.interrupted
	wait.mu.Unlock()

	// Darwin has no accept4. Nothing in this path can Start a child before this
	// descriptor is made and proved close-on-exec.
	syscall.CloseOnExec(connection)
	connectionFlags, connectionFlagErr := unix.FcntlInt(uintptr(connection), unix.F_GETFD, 0)
	connectionCloseOnExec := connectionFlagErr == nil && connectionFlags&unix.FD_CLOEXEC != 0
	peerAddress, peerAddressErr := unix.Getpeername(connection)
	listenerCloseErr := unix.Close(listenerToClose)
	if interrupted || !connectionCloseOnExec || listenerCloseErr != nil {
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	if err := wait.unlink(); err != nil {
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}

	peer, err := observeExecutionAuthorizationPeer(connection, peerAddress, peerAddressErr)
	if err != nil {
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	raw, err := readExecutionAuthorization(connection)
	if err != nil {
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	actual, err := decodeExecutionAuthorization(raw)
	if err != nil || actual != expected || !bytes.Equal(raw, canonical) || lifetime.Err() != nil ||
		!time.Now().Before(wait.deadline) {
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	peer.listenerCloseOnExec = wait.listenerCloseOnExec
	peer.connectionCloseOnExec = connectionCloseOnExec
	return peer, nil
}

func (wait *executionAuthorizationWait) closeCurrentLocked() {
	if wait.connection >= 0 {
		connection := wait.connection
		wait.connection = -1
		_ = unix.Close(connection)
		return
	}
	if wait.listener >= 0 {
		listener := wait.listener
		wait.listener = -1
		_ = unix.Close(listener)
	}
}

func (wait *executionAuthorizationWait) close() error {
	if wait == nil {
		return nil
	}
	wait.mu.Lock()
	wait.closeCurrentLocked()
	wait.state = executionAuthorizationClosed
	linked := wait.linked
	wait.mu.Unlock()
	if linked {
		return wait.unlink()
	}
	return nil
}

func (wait *executionAuthorizationWait) unlink() error {
	wait.mu.Lock()
	defer wait.mu.Unlock()
	if !wait.linked {
		return nil
	}
	if pressureRootsUnchanged(wait.root) != nil {
		return errExecutionAuthorization
	}
	current, err := observeExecutionAuthorizationSocket(wait.root)
	if err != nil || current != wait.socket {
		return errExecutionAuthorization
	}
	if err := unix.Unlinkat(int(wait.root.file.Fd()), executionAuthorizationSocketName, 0); err != nil {
		return errExecutionAuthorization
	}
	var absent unix.Stat_t
	if err := unix.Fstatat(int(wait.root.file.Fd()), executionAuthorizationSocketName, &absent, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) ||
		pressureRootsUnchanged(wait.root) != nil || unix.Fsync(int(wait.root.file.Fd())) != nil {
		return errExecutionAuthorization
	}
	wait.linked = false
	return nil
}

func observeExecutionAuthorizationSocket(root productionRoot) (executionAuthorizationSocketIdentity, error) {
	if pressureRootsUnchanged(root) != nil {
		return executionAuthorizationSocketIdentity{}, errExecutionAuthorization
	}
	var stat unix.Stat_t
	if err := unix.Fstatat(int(root.file.Fd()), executionAuthorizationSocketName, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
		stat.Ino == 0 || stat.Uid != uint32(os.Geteuid()) || stat.Mode&unix.S_IFMT != unix.S_IFSOCK {
		return executionAuthorizationSocketIdentity{}, errExecutionAuthorization
	}
	return executionAuthorizationSocketIdentity{device: stat.Dev, inode: stat.Ino, mode: stat.Mode, uid: stat.Uid}, nil
}

func observeExecutionAuthorizationPeer(fd int, address unix.Sockaddr, addressErr error) (executionAuthorizationPeer, error) {
	kind, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || kind != unix.SOCK_STREAM {
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	if _, ok := address.(*unix.SockaddrUnix); addressErr != nil || !ok {
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	pid, err := unix.GetsockoptInt(fd, unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	if err != nil || pid <= 0 {
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	credentials, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil || credentials.Version != 0 || credentials.Uid != uint32(os.Geteuid()) {
		return executionAuthorizationPeer{}, errExecutionAuthorization
	}
	return executionAuthorizationPeer{pid: pid, uid: credentials.Uid}, nil
}

func readExecutionAuthorization(fd int) ([]byte, error) {
	buffer := make([]byte, maxExecutionAuthorizationReadBytes)
	read := 0
	for read < len(buffer) {
		count, err := unix.Read(fd, buffer[read:])
		read += count
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nil, errExecutionAuthorization
		}
		if count == 0 {
			break
		}
	}
	if read < 2 || read == len(buffer) || buffer[read-1] != '\n' ||
		bytes.Count(buffer[:read], []byte{'\n'}) != 1 || read-1 > maxExecutionAuthorizationMessageBytes {
		return nil, errExecutionAuthorization
	}
	return append([]byte(nil), buffer[:read-1]...), nil
}

func sendExecutionAuthorization(ctx context.Context, path string, raw []byte, deadline time.Time) (retErr error) {
	value, err := decodeExecutionAuthorization(raw)
	canonical, canonicalErr := canonicalExecutionAuthorization(value)
	if ctx == nil || ctx.Err() != nil || err != nil || canonicalErr != nil || !bytes.Equal(raw, canonical) ||
		!validExecutionAuthorizationSocketPath(path) || !time.Now().Before(deadline) {
		return errExecutionAuthorization
	}
	dialer := net.Dialer{Deadline: deadline}
	connection, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return errExecutionAuthorization
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		_ = connection.Close()
		return errExecutionAuthorization
	}
	defer func() {
		if err := unixConnection.Close(); err != nil {
			retErr = errExecutionAuthorization
		}
	}()
	if unixConnection.SetDeadline(deadline) != nil {
		return errExecutionAuthorization
	}
	frame := make([]byte, len(raw)+1)
	copy(frame, raw)
	frame[len(raw)] = '\n'
	for len(frame) > 0 {
		written, err := unixConnection.Write(frame)
		if err != nil || written <= 0 {
			return errExecutionAuthorization
		}
		frame = frame[written:]
	}
	if unixConnection.CloseWrite() != nil {
		return errExecutionAuthorization
	}
	// The server sends no response. Waiting for its EOF keeps Darwin peer
	// metadata live through the server's mandatory credential observation.
	var response [1]byte
	if n, err := unixConnection.Read(response[:]); n != 0 || !errors.Is(err, io.EOF) {
		return errExecutionAuthorization
	}
	return nil
}
