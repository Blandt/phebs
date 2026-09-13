//go:build darwin

package t421

import (
	"context"
	"testing"
	"time"
)

// Only the flow's prework bookkeeping is modeled. Image Hold/Check/Close use
// the actual fixed native files; no engine, mounted teardown, issuer or signer
// command is exercised here.
func modeledSystemProfileFlow() *ExecutionEpochOne {
	return &ExecutionEpochOne{
		plan:    Plan{Schema: PlanV3Schema},
		epochs:  &ExecutionEpochConfigCustody{author: &ExecutionAuthorCustody{}},
		release: func() {},
	}
}

func holdSystemProfileSigner(t *testing.T) *ExecutionSystemToolCustody {
	t.Helper()
	requireExternalToolFrozenHost(t)
	tool, err := HoldExecutionSystemTool(t.Context(), "ssh-keygen")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tool.Close(); err != nil {
			t.Error(err)
		}
	})
	return tool
}

func TestExecutionProfileSignerActualBorrowedLifetime(t *testing.T) {
	signer := holdSystemProfileSigner(t)
	flow := modeledSystemProfileFlow()
	if err := flow.prepareProfileSigner(t.Context(), signer); err != nil {
		t.Fatal(err)
	}
	if flow.profileSigner != signer || flow.profileTools != ([2]*ExecutionToolCustody{}) {
		t.Fatal("borrowed fixed images entered mounted inputs")
	}
	observed := flow.profileSignerImage
	identity, path, err := signer.Check(t.Context(), "ssh-keygen")
	if err != nil || observed != (executionProfileSystemImage{Identity: identity, Path: path}) {
		t.Fatal("observation was not actual held-image evidence", err)
	}
	if flow.prepareProfileSigner(t.Context(), signer) == nil {
		t.Fatal("repeat observation admitted")
	}
	if err := flow.Close(); err != nil {
		t.Fatal(err)
	}
	// Real flow.Close, but unused controller/store bookkeeping: not native
	// operational teardown. It must not own the borrowed signer handle.
	if _, _, err := signer.Check(t.Context(), "ssh-keygen"); err != nil {
		t.Fatal("flow close released outer custody", err)
	}
	if err := signer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := signer.Check(t.Context(), "ssh-keygen"); err == nil {
		t.Fatal("closed outer custody accepted")
	}
	if flow.profileSignerImage != observed {
		t.Fatal("outer release rewrote detached observations")
	}
	observed.Identity.Role = "caller mutation"
	if flow.profileSignerImage.Identity.Role != "ssh-keygen" {
		t.Fatal("observation aliases caller value")
	}
}

func TestExecutionProfileSignerPreworkRefusals(t *testing.T) {
	signer := holdSystemProfileSigner(t)
	for _, mode := range []string{"nil_context", "canceled", "v1", "v2", "closed", "started", "used", "authored", "author_closed", "author_active", "epoch_closed", "epoch_active", "released", "repeat"} {
		t.Run(mode, func(t *testing.T) {
			flow := modeledSystemProfileFlow()
			ctx := t.Context()
			switch mode {
			case "nil_context":
				ctx = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "v1":
				flow.plan.Schema = PlanSchema
			case "v2":
				flow.plan.Schema = PlanV2Schema
			case "closed":
				flow.closed = true
			case "started":
				flow.authorStarted = time.Now()
			case "used":
				flow.used = true
			case "authored":
				flow.authored = true
			case "author_closed":
				flow.epochs.author.closed = true
			case "author_active":
				flow.epochs.author.active = true
			case "epoch_closed":
				flow.epochs.closed = true
			case "epoch_active":
				flow.epochs.active = true
			case "released":
				flow.epochs.released = 1
			case "repeat":
				flow.profileSystemUsed = true
			}
			if flow.prepareProfileSigner(ctx, signer) == nil || flow.profileSigner != nil || flow.profileSignerImage != (executionProfileSystemImage{}) {
				t.Fatal("invalid prework returned observations")
			}
			if flow.profileSystemUsed != (mode == "repeat") {
				t.Fatal("prework refusal consumed observation attempt")
			}
		})
	}
}

func TestExecutionProfileSignerOwnedCheckFailureSticks(t *testing.T) {
	for _, mode := range []string{"closed", "drift", "role", "close_error"} {
		t.Run(mode, func(t *testing.T) {
			signer := holdSystemProfileSigner(t)
			switch mode {
			case "closed":
				if err := signer.Close(); err != nil {
					t.Fatal(err)
				}
			case "drift":
				signer.volume[0] ^= 1 // Change only owned fixture metadata, never the system file.
			case "role":
				signer.identity.Role = "sh"
			case "close_error":
				if err := signer.file.Close(); err != nil {
					t.Fatal(err)
				}
				if signer.Close() == nil {
					t.Fatal("actual descriptor-close error was hidden")
				}
			}
			flow := modeledSystemProfileFlow()
			if flow.prepareProfileSigner(t.Context(), signer) == nil || !flow.profileSystemUsed ||
				flow.profileSigner != nil || flow.profileSignerImage != (executionProfileSystemImage{}) {
				t.Fatal("failed actual check retained a partial signer or renewed attempt")
			}
			if flow.prepareProfileSigner(t.Context(), signer) == nil {
				t.Fatal("failed observation retried")
			}
		})
	}
}
