package store

// Cold backup/restore coverage for the stop-first filesystem path documented
// in docs/guides/OPERATIONS.md ("The stop-first cold path remains available").
//
// The database's root credential is bound to the database directory's
// lifetime in the mode-0600 $DATA/.surreal-child-pass file: SurrealDB only
// initializes the root user when none exists and never rotates a stored root
// password, so a cold copy of db/ is only openable with its matching password
// file. These tests pin the documented matching set (exact config, db/,
// .surreal-child-pass), the safe failure when the file is missing or wrong,
// and the legacy root/root upgrade path.
//
// "The database is unmodified" is verified semantically, not byte-wise:
// merely starting the engine rewrites SurrealKV housekeeping files (LOCK,
// manifest, sstables) even when sign-in then fails. What must not change is
// the stored credential and the data: after a failed reopen, placing the
// true password file must open the database with the probe row intact.
//
// They need the pinned SurrealDB engine: set PHEBS_SURREAL (or have surreal
// on PATH); otherwise they skip.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	surrealdb "github.com/surrealdb/surrealdb.go"
)

// requireColdBackupEngine skips the test when no SurrealDB engine is
// available. Production resolution is PHEBS_SURREAL first, then PATH.
func requireColdBackupEngine(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("PHEBS_SURREAL")) != "" {
		return
	}
	if _, err := exec.LookPath("surreal"); err != nil {
		t.Skip("cold backup tests need a SurrealDB engine: set PHEBS_SURREAL or put surreal on PATH")
	}
}

// coldBackupContext bounds one supervised-child lifecycle well under the go
// test timeout while leaving room for first-start schema application.
func coldBackupContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(t.Context(), 10*time.Minute)
}

func coldBackupCopyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatal(err)
	}
	// O_EXCL: a cold copy must never merge into an existing tree.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write(data); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func coldBackupCopyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		srcPath, dstPath := filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			coldBackupCopyDir(t, srcPath, dstPath)
			continue
		}
		if !entry.Type().IsRegular() {
			continue
		}
		coldBackupCopyFile(t, srcPath, dstPath)
	}
	// Preserve the source directory's own mode on the copy.
	if info, err := os.Stat(src); err == nil {
		_ = os.Chmod(dst, info.Mode().Perm())
	}
}

type coldBackupProbe struct {
	Note string `json:"note"`
}

func coldBackupWriteProbe(t *testing.T, ctx context.Context, s *Surreal, note string) {
	t.Helper()
	if _, err := surrealdb.Query[any](ctx, s.db,
		"CREATE cold_backup_probe:marker SET note = $note;",
		map[string]any{"note": note}); err != nil {
		t.Fatalf("write probe row: %v", err)
	}
}

func coldBackupReadProbe(t *testing.T, ctx context.Context, s *Surreal) string {
	t.Helper()
	rows, err := surrealdb.Query[[]coldBackupProbe](ctx, s.db,
		"SELECT note FROM cold_backup_probe:marker;", nil)
	if err != nil {
		t.Fatalf("read probe row: %v", err)
	}
	got := firstDomainRows(rows)
	if len(got) != 1 {
		t.Fatalf("probe rows = %d, want exactly 1", len(got))
	}
	return got[0].Note
}

func coldBackupClose(t *testing.T, ctx context.Context, s *Surreal, dataDir string) {
	t.Helper()
	if err := s.Close(ctx); err != nil {
		t.Fatalf("close store: %v", err)
	}
	// The live-backup rendezvous is removed on stop; its absence plus the
	// synchronous child wait in Close is the test's "database has exited"
	// confirmation before the filesystem copy.
	if _, err := os.Lstat(filepath.Join(dataDir, localRuntimeName)); !os.IsNotExist(err) {
		t.Fatalf("runtime descriptor survived close: %v", err)
	}
}

func coldBackupAssertAuthFailure(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("reopen succeeded with a missing/wrong password file; want an authentication failure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "auth") {
		t.Fatalf("reopen error = %v; want an authentication failure", err)
	}
}

// ownTestStore registers the store's cleanup immediately after acquisition,
// before any write, assertion, or explicit close can fatal past it. The
// cleanup is safe after an explicit early close or a reopen: the SDK's Close
// is a no-op on a closed connection and the supervised child's stop refuses
// a second stop, so the redundant close only discards an error. Cleanup runs
// before the test's TempDir removal (LIFO, with the data directory created
// first), so the child never outlives its database directory. OpenLocal
// returns a nil store on every error path, so the nil guard only covers
// unexpected-success registrations in negative cases.
func ownTestStore(t *testing.T, s *Surreal) {
	t.Helper()
	if s == nil {
		return
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
}

// coldBackupSeed writes identifiable data into a fresh random-password
// database, closes it, and returns the data directory holding the exact
// documented precious set: config bytes, db/, and .surreal-child-pass.
func coldBackupSeed(t *testing.T, ctx context.Context) (dataDir string, note string) {
	t.Helper()
	dataDir = t.TempDir()
	configBytes := []byte("# cold backup fixture config for " + t.Name() + "\nserver:\n  data_dir: " + dataDir + "\n")
	if err := os.WriteFile(filepath.Join(dataDir, "phebs.yaml"), configBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenLocal(ctx, dataDir)
	// Registered before the probe writes and assertions below: any of them
	// can fatal, and the seed must never orphan its supervised child.
	ownTestStore(t, s)
	if err != nil {
		t.Fatalf("open seed database: %v", err)
	}
	note = "cold-backup-probe/" + t.Name()
	coldBackupWriteProbe(t, ctx, s, note)
	if got := coldBackupReadProbe(t, ctx, s); got != note {
		t.Fatalf("probe readback = %q, want %q", got, note)
	}
	// The seed must carry a random (non-legacy) password: the missing-file
	// case below is only meaningful when root/root would not authenticate.
	pass, err := readChildPassFile(dataDir)
	if err != nil {
		t.Fatalf("read seed password file: %v", err)
	}
	if pass == legacyRootPass {
		t.Fatal("seed database resolved the legacy root password; want a fresh random one")
	}
	coldBackupClose(t, ctx, s, dataDir)
	return dataDir, note
}

// coldBackupCopyState copies the documented matching set — exact config,
// db/, and the password file when wantPass is true — into a fresh directory.
func coldBackupCopyState(t *testing.T, src, dst string, wantPass bool) {
	t.Helper()
	coldBackupCopyFile(t, filepath.Join(src, "phebs.yaml"), filepath.Join(dst, "phebs.yaml"))
	coldBackupCopyDir(t, filepath.Join(src, "db"), filepath.Join(dst, "db"))
	if wantPass {
		coldBackupCopyFile(t,
			filepath.Join(src, localChildPassName),
			filepath.Join(dst, localChildPassName))
		info, err := os.Stat(filepath.Join(dst, localChildPassName))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("restored password file mode = %v, %v; want 0600", info, err)
		}
	}
}

// coldBackupAssertRecoverable reopens dst with the true password file and
// requires the probe row intact. This is the "failed reopen modified
// nothing" oracle: the stored root credential was not rotated and no data
// was lost or corrupted.
func coldBackupAssertRecoverable(t *testing.T, ctx context.Context, dst, note string) {
	t.Helper()
	recovered, err := OpenLocal(ctx, dst)
	ownTestStore(t, recovered)
	if err != nil {
		t.Fatalf("reopen with the true password file after failed attempt: %v", err)
	}
	if got := coldBackupReadProbe(t, ctx, recovered); got != note {
		t.Fatalf("probe after recovery = %q, want %q: the failed open modified the database", got, note)
	}
}

// TestColdBackupRestoreRoundTrip is the documented procedure end to end: a
// random-password database with identifiable data is cold-copied as the full
// matching set (config, db/, .surreal-child-pass) into a fresh directory and
// reopened, and the data reads back.
func TestColdBackupRestoreRoundTrip(t *testing.T) {
	requireColdBackupEngine(t)
	ctx, cancel := coldBackupContext(t)
	defer cancel()

	src, note := coldBackupSeed(t, ctx)
	dst := t.TempDir()
	coldBackupCopyState(t, src, dst, true)

	reopened, err := OpenLocal(ctx, dst)
	ownTestStore(t, reopened)
	if err != nil {
		t.Fatalf("reopen restored database: %v", err)
	}
	if got := coldBackupReadProbe(t, ctx, reopened); got != note {
		t.Fatalf("restored probe = %q, want %q", got, note)
	}
	restoredConfig, err := os.ReadFile(filepath.Join(dst, "phebs.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sourceConfig, err := os.ReadFile(filepath.Join(src, "phebs.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredConfig) != string(sourceConfig) {
		t.Fatal("restored config differs from the preserved exact config")
	}
}

// TestColdRestoreWithoutPasswordFileFailsClosed copies db/ without its
// password file — the documented defect — and requires the reopen to fail
// authentication safely: an auth error, no invented working credential, and
// the database recoverable with the true password file and data intact.
func TestColdRestoreWithoutPasswordFileFailsClosed(t *testing.T) {
	requireColdBackupEngine(t)
	ctx, cancel := coldBackupContext(t)
	defer cancel()

	src, note := coldBackupSeed(t, ctx)
	dst := t.TempDir()
	coldBackupCopyState(t, src, dst, false)

	unexpected, err := OpenLocal(ctx, dst)
	// Own the store even on the unexpected-success path: the assertion below
	// fatals before any explicit close could run.
	ownTestStore(t, unexpected)
	coldBackupAssertAuthFailure(t, err)

	// No invented replacement credential may authenticate: retrying with
	// only whatever the failed attempt left behind must fail again.
	passPath := filepath.Join(dst, localChildPassName)
	if _, statErr := os.Lstat(passPath); statErr == nil {
		retry, err := OpenLocal(ctx, dst)
		ownTestStore(t, retry)
		if err == nil {
			t.Fatal("reopen with the minted password file succeeded: the failed open invented a working credential")
		}
		coldBackupAssertAuthFailure(t, err)
		// The operator's recovery is to remove the minted file and place
		// the true one; the failed attempt must not have made that
		// impossible.
		if err := os.Remove(passPath); err != nil {
			t.Fatal(err)
		}
	}
	coldBackupCopyFile(t, filepath.Join(src, localChildPassName), passPath)
	coldBackupAssertRecoverable(t, ctx, dst, note)
}

// TestColdRestoreWithWrongPasswordFileFailsClosed copies db/ with another
// database's (well-formed but wrong) password file and requires the same
// safe failure: an auth error, the operator's file left byte-identical so
// the true file can still be placed, and the database recoverable intact.
func TestColdRestoreWithWrongPasswordFileFailsClosed(t *testing.T) {
	requireColdBackupEngine(t)
	ctx, cancel := coldBackupContext(t)
	defer cancel()

	src, note := coldBackupSeed(t, ctx)
	dst := t.TempDir()
	coldBackupCopyState(t, src, dst, true)

	wrong := strings.Repeat("e", 64)
	if !validChildPass(wrong) {
		t.Fatal("fixture wrong password is not well-formed")
	}
	passPath := filepath.Join(dst, localChildPassName)
	if err := os.WriteFile(passPath,
		[]byte(`{"schema":"`+localChildPassSchema+`","pass":"`+wrong+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	passBefore, err := os.ReadFile(passPath)
	if err != nil {
		t.Fatal(err)
	}

	unexpected, err := OpenLocal(ctx, dst)
	// Own the store even on the unexpected-success path: the assertion below
	// fatals before any explicit close could run.
	ownTestStore(t, unexpected)
	coldBackupAssertAuthFailure(t, err)

	passAfter, err := os.ReadFile(passPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(passAfter) != string(passBefore) {
		t.Fatal("failed reopen modified the wrong password file instead of leaving it for the operator")
	}

	// The operator places the true file over the wrong one; the database
	// must open with data intact.
	trueBytes, err := os.ReadFile(filepath.Join(src, localChildPassName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passPath, trueBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	coldBackupAssertRecoverable(t, ctx, dst, note)
}

// TestColdRestoreLegacyRootDatabaseOpensWithRoot retains the upgrade path: a
// database initialized with the historical root/root credential, carrying no
// password file, still opens with root.
func TestColdRestoreLegacyRootDatabaseOpensWithRoot(t *testing.T) {
	requireColdBackupEngine(t)
	ctx, cancel := coldBackupContext(t)
	defer cancel()

	dataDir := t.TempDir()
	// Pre-seed the legacy password so the fresh database initializes its
	// stored root user with root/root, then remove the file: what remains is
	// exactly a pre-binding legacy database directory. publishChildPass is
	// the durable temp+rename+dir-sync publication API that replaced the
	// removed unsafe direct writer; the lock is held and the fresh directory
	// holds no credential yet, satisfying its caller contract.
	release, err := acquireChildPassLock(ctx, dataDir)
	if err != nil {
		t.Fatalf("acquire credential lock to seed legacy password: %v", err)
	}
	if err := publishChildPass(dataDir, legacyRootPass); err != nil {
		release()
		t.Fatalf("seed legacy password file: %v", err)
	}
	release()
	s, err := OpenLocal(ctx, dataDir)
	// Registered before the probe write and the explicit close below: any
	// fatal between here and coldBackupClose must still stop the child.
	ownTestStore(t, s)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	note := "cold-backup-probe/" + t.Name()
	coldBackupWriteProbe(t, ctx, s, note)
	coldBackupClose(t, ctx, s, dataDir)
	if err := os.Remove(filepath.Join(dataDir, localChildPassName)); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenLocal(ctx, dataDir)
	ownTestStore(t, reopened)
	if err != nil {
		t.Fatalf("reopen legacy database without password file: %v", err)
	}
	if got := coldBackupReadProbe(t, ctx, reopened); got != note {
		t.Fatalf("legacy probe = %q, want %q", got, note)
	}
}
