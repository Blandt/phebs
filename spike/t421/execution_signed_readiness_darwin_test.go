//go:build darwin

package t421

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bmeddeb/phebs/spike/t4013"
	"golang.org/x/sys/unix"
)

// This selector authorizes one rehearsal, including its ephemeral signature and
// its exact live authorization message. It does not select or resume the later
// formal T42.2o freeze. No selector is forwarded to the production process.
// The ordinary executable performs all preparation, fifteen phases, signing,
// custody checks and outer verification with its unchanged limits.
func TestExecutionSignedLauncherOptionalReadiness(t *testing.T) {
	mode := os.Getenv("PHEBS_T422_SIGNED_LAUNCHER_REHEARSAL")
	if mode == "" {
		t.Skip("requires explicit serial signed-launcher rehearsal selection")
	}
	if !executionSignedReadinessMode(mode) {
		t.Fatal("unknown signed rehearsal mode; no custody acquired")
	}
	requireExternalToolFrozenHost(t)
	if os.Getenv("PHEBS_T422_EXTERNAL_SURREAL") == "" {
		t.Fatal("selected signed rehearsal requires an explicit native SurrealDB image")
	}
	selection := executionSelectionV1{
		Schema:               executionSelectionSchema,
		RepositoryRoot:       os.Getenv("PHEBS_T422_PRODUCTION_REPOSITORY"),
		PlanSourceCommit:     os.Getenv("PHEBS_T422_PLAN_SOURCE_COMMIT"),
		IntegratedMainCommit: os.Getenv("PHEBS_T422_INTEGRATED_MAIN_COMMIT"),
		SourceCommit:         os.Getenv("PHEBS_T422_PRODUCTION_COMMIT"),
		GoRoot:               os.Getenv("PHEBS_T422_PRODUCTION_GOROOT"),
		ModuleCache:          os.Getenv("PHEBS_T422_PRODUCTION_MODULE_CACHE"),
		GitBinary:            os.Getenv("PHEBS_T422_PRODUCTION_GIT"),
		SurrealBinary:        toolCustodyExternalSurreal(t),
	}
	// Refuse input shape before even creating the test's private root. The two
	// temporary strings only stand in for paths/ID that the test will own below;
	// they are never passed to an issuer or to the launcher.
	selection.CeremonyID = "readiness"
	selection.SignerControlRoot = "/private/tmp/t422-readiness-selection-shape"
	if !validExecutionSelection(selection) {
		t.Fatal("explicit canonical repository, source/main commits and native tools required")
	}
	root, err := os.MkdirTemp("/private/tmp", "t422-signed-readiness-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("private rehearsal evidence/ephemeral signer custody: %s", root)
	// Never testing.TempDir: a failed real launcher can leave mounted custody.
	// Keep the namespace claims, private key and returned source-free evidence
	// for attribution. No private key is read or copied by the harness.
	selection.CeremonyID = filepath.Base(root)
	selection.SignerControlRoot = filepath.Join(root, "signer")
	bootstrap := filepath.Join(root, "bootstrap")
	for _, path := range []string{selection.SignerControlRoot, bootstrap} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if !validExecutionSelection(selection) {
		t.Fatal("owned rehearsal selections are not canonical")
	}
	namespace, err := holdExecutionSignerNamespace(t.Context(), selection.SignerControlRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = namespace.Close() }()
	// Bootstrap is outside the measured outer lifetime. Both the supplied
	// executor and its independent reference rebuild use genuine protected
	// source/SDK/module custody. The command is not this package's TestMain.
	buildCtx, stopBuild := context.WithTimeout(t.Context(), 20*time.Minute)
	defer stopBuild()
	git, err := ProtectExecutionGit(buildCtx, bootstrap, selection.GitBinary)
	if err != nil {
		t.Fatal("retained bootstrap Git custody", err)
	}
	inputs, err := ProtectExecutionGoBuildInputs(buildCtx, bootstrap, ExecutionGoBuildRequest{
		Git: git, RepositoryRoot: selection.RepositoryRoot, PlanSourceCommit: selection.PlanSourceCommit,
		IntegratedMainCommit: selection.IntegratedMainCommit, SourceCommit: selection.SourceCommit,
		GoRoot: selection.GoRoot, ModuleCache: selection.ModuleCache,
	})
	if err != nil {
		t.Fatal("retained bootstrap build inputs", err)
	}
	builds := filepath.Join(bootstrap, "supplied-builds")
	for _, path := range []string{builds, filepath.Join(builds, "home"), filepath.Join(builds, "tmp"), filepath.Join(builds, "cache")} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	supplied := productionRehearsalBuildSchema(t, buildCtx, inputs, builds, "t422-execute", PlanV3Schema)
	executor, err := inputs.ProtectReferenceToolV3(buildCtx, bootstrap, "t422-execute", supplied)
	if err != nil {
		t.Fatal("retained bootstrap executor", err)
	}
	identity, executable, err := executor.Check(buildCtx, "t422-execute")
	if err != nil {
		t.Fatal(err)
	}
	stopBuild()
	t.Logf("source=%s integrated-main=%s actual protected executor=%s", selection.SourceCommit, selection.IntegratedMainCommit, identity.SHA256)

	ctx, cancel := context.WithTimeout(t.Context(), executionMaximumWall)
	defer cancel()
	reader, writer := executionLauncherOutputPipe(t, true)
	command := exec.CommandContext(ctx, executable, executionOuterMode, "--selection-base64url", encodeExecutionSelection(t, selection))
	command.Env, command.Stdout, command.Stderr = []string{}, writer, io.Discard
	command.WaitDelay = 5 * time.Second
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	prepareProductionSession(command)
	frames := make(chan executionAuthorizationHandoffFrame, 1)
	packages := make(chan executionReturnedOutput, 1)
	captured := make(chan struct{})
	go func() {
		defer close(captured)
		captureExecutionReturnedOutput(reader, frames, packages)
	}()
	if err := command.Start(); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		<-captured
		t.Fatal(err)
	}
	_ = writer.Close() // The child owns the sole remaining write descriptor.
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	joined := false
	innerSession := 0
	defer func() {
		if !joined {
			_ = command.Process.Signal(syscall.SIGTERM)
		}
		// Inner owns a distinct session. Always account for both known scopes,
		// including a t.Fatal after the outer's native Wait already completed.
		var cleanupErr error
		joined, _, cleanupErr = finishExecutionProcessSession(command.Process.Pid, waited, joined, nil, time.Now().Add(5*time.Second))
		if cleanupErr != nil || !joined {
			t.Error("rehearsal outer/session cleanup unavailable or forced; retain all named custody", cleanupErr)
		}
		if innerSession > 0 {
			if err := t4013.WaitPrivateProcessSession(innerSession, time.Now().Add(5*time.Second)); err != nil {
				// Emergency harness cleanup is never accepted as a clean native
				// launcher result. Retain failure even if the forced sweep works.
				t.Error("captured inner session did not close; emergency cleanup required", err)
				killErr := t4013.KillPrivateProcessSession(innerSession)
				closeErr := t4013.WaitPrivateProcessSession(innerSession, time.Now().Add(6*time.Second))
				if killErr != nil || closeErr != nil {
					t.Error("captured inner session remains unavailable after bounded cleanup", killErr, closeErr)
				}
			}
		}
		_ = reader.Close()
		<-captured
	}()
	var frame executionAuthorizationHandoffFrame
	select {
	case frame = <-frames:
	case <-ctx.Done():
		t.Fatal("live handoff unavailable before effective harness/outer deadline", ctx.Err(), t.Context().Err())
	}
	if frame.err != nil || frame.value.T422ExecuteImageSHA256 != identity.SHA256 || frame.value.ClientArgv[0] != executable {
		t.Fatal("real launcher failed before a matching signed handoff", frame.err)
	}
	// At this quiescent boundary preparation children have joined, the client
	// has not started, and the operational sequence is still unauthorized.
	live, err := t4013.ObserveProcessTreeRecords(ctx, command.Process.Pid)
	if err == nil && len(live) > 1 && live[1].ParentPID == command.Process.Pid {
		if observed, observeErr := unix.Getsid(live[1].PID); observeErr == nil && observed == live[1].PID && observed != command.Process.Pid {
			innerSession = observed
		}
	}
	if err != nil || len(live) != 2 || live[0].PID != command.Process.Pid || live[1].ParentPID != command.Process.Pid {
		t.Fatal("live handoff did not expose exactly the native outer/inner pair", err)
	}
	if innerSession == 0 {
		t.Fatal("inner is not its own distinct native session")
	}
	if err := os.WriteFile(filepath.Join(root, "authorization-handoff.json"), frame.raw, 0o600); err != nil {
		t.Fatal(err)
	}
	operational := filepath.Dir(frame.value.SocketPath)
	operationalInfo, err := os.Lstat(operational)
	if err != nil || !operationalInfo.IsDir() || filepath.Dir(operational) != "/private/tmp" || !strings.HasPrefix(filepath.Base(operational), "phebs-t422-") {
		t.Fatal("unexpected live operational root")
	}
	t.Logf("live rehearsal operational custody: %s; admission expires %d", operational, frame.value.FinalAdmissionDeadlineUnixNano)
	// Anchor verification in the independently selected, actually held signer
	// namespace before authorizing. Returned signer.pub is never this anchor.
	names := executionSignerNames(selection.CeremonyID)
	public, err := executionSignedReadinessPublic(ctx, namespace, names.generatedPublic)
	if err != nil {
		t.Fatal("independent rehearsal public key unavailable", err)
	}
	if mode == "orphan-preparation" {
		joined = executionSignedReadinessOrphan(t, ctx, root, selection, frame, live[1], operationalInfo, command, waited, packages, captured)
		return // Actual refused custody remains; no bootstrap cleanup/pass claim.
	}
	clientArgs := append([]string(nil), frame.value.ClientArgv...)
	var signerBefore, replacementInfo os.FileInfo
	var signerClaimBefore []byte
	switch mode {
	case "reject-authorization":
		clientArgs, err = executionSignedReadinessWrongAuthorization(frame.value)
		if err != nil {
			t.Fatal(err)
		}
	case "replace-signer-namespace":
		// Rename only this test's independently created signer directory, after
		// all signer children joined at the live wait. Preserve both identities.
		signerBefore, err = os.Lstat(selection.SignerControlRoot)
		if err != nil {
			t.Fatal(err)
		}
		signerClaimBefore, err = executionSignedReadinessSignerBytes(ctx, namespace, names.claim)
		if err != nil || os.Rename(selection.SignerControlRoot, selection.SignerControlRoot+"-retained") != nil || os.Mkdir(selection.SignerControlRoot, 0o700) != nil {
			t.Fatal("could not install owned namespace replacement")
		}
		replacementInfo, err = os.Lstat(selection.SignerControlRoot)
		if err != nil || os.SameFile(signerBefore, replacementInfo) {
			t.Fatal("namespace replacement did not create a distinct owned identity")
		}
	case "cancel-wait":
		if err := command.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
	}
	if mode != "cancel-wait" {
		clientCtx, stopClient := context.WithDeadline(ctx, time.Unix(0, frame.value.FinalAdmissionDeadlineUnixNano))
		client := exec.CommandContext(clientCtx, clientArgs[0], clientArgs[1:]...)
		client.Env, client.Stdout, client.Stderr = []string{}, io.Discard, io.Discard
		client.WaitDelay = 5 * time.Second
		clientErr := client.Run()
		stopClient()
		if clientErr != nil {
			t.Fatal("exact live authorization client failed", clientErr)
		}
	}
	var waitErr error
	select {
	case waitErr = <-waited:
		joined = true
	case <-ctx.Done():
		t.Fatal("launcher did not join before effective harness/outer deadline", ctx.Err(), t.Context().Err())
	}
	finishedAt := time.Now()
	if err := t4013.WaitPrivateProcessSession(command.Process.Pid, time.Now().Add(5*time.Second)); err != nil {
		t.Fatal("outer private session did not close", err)
	}
	if err := t4013.WaitPrivateProcessSession(innerSession, time.Now().Add(5*time.Second)); err != nil {
		t.Fatal("inner private session did not close", err)
	}
	select {
	case <-captured:
	case <-ctx.Done():
		t.Fatal("launcher output remained open after Wait")
	}
	var returned executionReturnedOutput
	select {
	case returned = <-packages:
	default:
		t.Fatal("capture did not finish the returned output boundary")
	}
	if mode == "healthy" {
		if waitErr != nil || returned.err != nil {
			if len(returned.raw) > 0 {
				_ = os.WriteFile(filepath.Join(root, "stopped-package.bin"), returned.raw, 0o600)
			}
			t.Fatal("healthy signed launcher did not return success; preserve actual stopped evidence", waitErr, returned.err)
		}
		verified, err := verifyExecutionReturnedPackage(ctx, returned.raw, selection, frame.value.FreezeSHA256)
		if err != nil {
			t.Fatal("independent complete returned-package replay", err)
		}
		files, err := inspectExecutionReturnedPackage(returned.raw, Plan{SealPolicy: frozenSealPolicy()})
		if err != nil || !bytes.Equal(public, files["signer.pub"]) || verified.receipt.Decision.Outcome != "passed" || verified.receipt.Teardown.Outcome != "clean" || len(verified.receipt.PhaseResults) != 15 {
			t.Fatal("independent signer, success or complete phase evidence mismatch")
		}
		if err := os.WriteFile(filepath.Join(root, "returned-package.bin"), returned.raw, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("actual fifteen-phase signed launcher passed; returned package=%s bytes=%d", verified.digest, len(returned.raw))
	} else {
		var exit *exec.ExitError
		if !executionSignedReadinessTimelyRefusal(ctx.Err(), finishedAt, time.Unix(0, frame.value.FinalAdmissionDeadlineUnixNano)) ||
			!errors.As(waitErr, &exit) || waitErr != exit || exit.ProcessState == nil || !exit.ProcessState.Exited() || exit.ExitCode() != 1 || returned.err == nil || len(returned.raw) != 0 {
			t.Fatal("expected timely ordinary exit status 1 without operational receipt; timeout/signal is unavailable evidence", waitErr, returned.err, ctx.Err())
		}
		t.Logf("real signed-wait refusal %s: timely ordinary native status 1, no operational package; not an executed stopped-receipt test", mode)
	}
	if mode == "replace-signer-namespace" {
		retained, retainedErr := os.Lstat(selection.SignerControlRoot + "-retained")
		replacement, replacementErr := os.Lstat(selection.SignerControlRoot)
		if retainedErr != nil || replacementErr != nil || !os.SameFile(signerBefore, retained) || !os.SameFile(replacementInfo, replacement) {
			t.Fatal("refused namespace drift did not retain both exact external identities")
		}
		retainedNamespace, err := holdExecutionSignerNamespace(ctx, selection.SignerControlRoot+"-retained")
		if err != nil {
			t.Fatal("retained original namespace unavailable", err)
		}
		defer func() { _ = retainedNamespace.Close() }()
		retainedPublic, publicErr := executionSignedReadinessPublic(ctx, retainedNamespace, names.generatedPublic)
		retainedClaim, claimErr := executionSignedReadinessSignerBytes(ctx, retainedNamespace, names.claim)
		if publicErr != nil || claimErr != nil || !bytes.Equal(public, retainedPublic) || !bytes.Equal(signerClaimBefore, retainedClaim) {
			t.Fatal("refused namespace drift changed original public key or spent claim", publicErr, claimErr)
		}
	}
	if _, err := os.Lstat(operational); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("operational custody remains; this is retained failure, not clean readiness", err)
	}
	// Only a joined, verified, removed operational run permits bootstrap copy
	// cleanup. Namespace/key claims and source-free evidence remain deliberate.
	gitCustodyTestCleanup(t, git)
	goBuildTestCleanup(t, inputs)
	inputCustodyTestCleanup(t, executor.input, []ExecutionInputCopy{{Name: "t422-execute"}})
	if err := os.RemoveAll(builds); err != nil {
		t.Fatal("joined bootstrap build cache cleanup", err)
	}
	t.Logf("rehearsal complete; retained ephemeral signer claims/evidence, with exact bootstrap copy cleanup registered: %s", root)
}

func executionSignedReadinessTimelyRefusal(ctxErr error, finished, deadline time.Time) bool {
	return ctxErr == nil && !finished.IsZero() && !deadline.IsZero() && finished.Before(deadline)
}

func executionSignedReadinessMode(mode string) bool {
	switch mode {
	case "healthy", "reject-authorization", "cancel-wait", "replace-signer-namespace", "orphan-preparation":
		return true
	default:
		return false
	}
}

func executionSignedReadinessPublic(ctx context.Context, namespace *executionSignerNamespaceCustody, name string) ([]byte, error) {
	raw, err := executionSignedReadinessSignerBytes(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	public, _, _, err := deriveExecutionSignerPublic(raw)
	return public, err
}

func executionSignedReadinessSignerBytes(ctx context.Context, namespace *executionSignerNamespaceCustody, name string) ([]byte, error) {
	if _, err := namespace.check(ctx); err != nil || filepath.Base(name) != name {
		return nil, ErrExecutionLauncher
	}
	path := filepath.Join(namespace.path, name)
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, ErrExecutionLauncher
	}
	defer func() { _ = file.Close() }()
	held, heldErr := file.Stat()
	current, currentErr := os.Lstat(path)
	if heldErr != nil || currentErr != nil || !held.Mode().IsRegular() || held.Mode().Perm() != 0o600 || !os.SameFile(held, current) || held.Size() < 1 || held.Size() > maxExecutionSignerKeyBytes {
		return nil, ErrExecutionLauncher
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxExecutionSignerKeyBytes+1))
	if err != nil {
		return nil, err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(held, after) || after.Size() != held.Size() || int64(len(raw)) != held.Size() {
		return nil, ErrExecutionLauncher
	}
	if _, err := namespace.check(ctx); err != nil {
		return nil, err
	}
	return raw, nil
}

func executionSignedReadinessWrongAuthorization(frame executionAuthorizationHandoffV1) ([]string, error) {
	value, err := decodeExecutionAuthorization([]byte(frame.AuthorizationJSON))
	if err != nil || len(frame.ClientArgv) != 6 {
		return nil, errExecutionAuthorization
	}
	if value.FreezeSHA256[0] == '0' {
		value.FreezeSHA256 = "1" + value.FreezeSHA256[1:]
	} else {
		value.FreezeSHA256 = "0" + value.FreezeSHA256[1:]
	}
	raw, err := canonicalExecutionAuthorization(value)
	if err != nil {
		return nil, err
	}
	args := append([]string(nil), frame.ClientArgv...)
	args[5] = base64.RawURLEncoding.EncodeToString(raw)
	return args, nil
}

func TestExecutionSignedReadinessSelectors(t *testing.T) {
	for _, test := range []struct {
		mode string
		want bool
	}{
		{"healthy", true}, {"reject-authorization", true}, {"cancel-wait", true}, {"replace-signer-namespace", true},
		{"orphan-preparation", true},
		{"", false}, {"1", false}, {"formal", false}, {"healthy,orphan", false},
	} {
		t.Run(test.mode, func(t *testing.T) {
			if executionSignedReadinessMode(test.mode) != test.want {
				t.Fatal("selector authority expanded")
			}
		})
	}
}

func TestExecutionSignedReadinessRefusalDeadline(t *testing.T) {
	now := time.Unix(1, 0)
	for _, test := range []struct {
		name               string
		ctxErr             error
		finished, deadline time.Time
		want               bool
	}{
		{"timely", nil, now, now.Add(time.Second), true},
		{"deadline_equal", nil, now, now, false},
		{"deadline_elapsed", nil, now.Add(time.Second), now, false},
		{"outer_expired", context.DeadlineExceeded, now, now.Add(time.Second), false},
		{"harness_canceled", context.Canceled, now, now.Add(time.Second), false},
		{"unavailable_finish", nil, time.Time{}, now, false},
		{"unavailable_deadline", nil, now, time.Time{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if executionSignedReadinessTimelyRefusal(test.ctxErr, test.finished, test.deadline) != test.want {
				t.Fatal("deadline/context uncertainty became refusal evidence")
			}
		})
	}
}

func TestExecutionSignedReadinessWrongAuthorization(t *testing.T) {
	value := executionAuthorizationV1{Schema: executionAuthorizationSchema, FreezeSHA256: strings.Repeat("a", 64), SessionBindingSHA256: strings.Repeat("b", 64)}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	frame := executionAuthorizationHandoffV1{AuthorizationJSON: string(raw), ClientArgv: []string{"/private/tmp/executor", executionAuthorizationMode, "--socket", "/private/tmp/auth.sock", "--payload-base64url", base64.RawURLEncoding.EncodeToString(raw)}}
	args, err := executionSignedReadinessWrongAuthorization(frame)
	if err != nil {
		t.Fatal(err)
	}
	changedRaw, err := base64.RawURLEncoding.DecodeString(args[5])
	if err != nil {
		t.Fatal(err)
	}
	changed, err := decodeExecutionAuthorization(changedRaw)
	if err != nil || changed.FreezeSHA256 == value.FreezeSHA256 || changed.SessionBindingSHA256 != value.SessionBindingSHA256 || args[3] != frame.ClientArgv[3] || frame.ClientArgv[5] != base64.RawURLEncoding.EncodeToString(raw) {
		t.Fatal("negative authorization must change only the copied freeze binding")
	}
}
