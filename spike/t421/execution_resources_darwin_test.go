//go:build darwin

package t421

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/bmeddeb/phebs/spike/t4013"
)

func TestExecutionWholeProcessPhaseScope(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "all_phases", true: "sticky_stop"}[failed], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			refuse := false
			meter, err := newExecutionProcessPhaseObservation(ctx, 41, 1, "phebs", map[string]string{"phebs": "phebs", "git": "git"}, cancel, func(context.Context, int) ([]t4013.NativeProcessRecord, error) {
				calls++
				if refuse {
					return nil, errors.New("native denial")
				}
				return epochProcessFixtureRows(), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			pauseEpochProcessTicker(meter)
			r := &executionWholeResources{ctx: ctx, cancel: cancel, process: meter, phase: 1}
			if err := r.finish(); err != nil {
				t.Fatal(err)
			}
			if failed {
				refuse = true
				if r.begin("cold") != nil {
					t.Fatal("failed measurement prevented boundary")
				}
				if r.finish() == nil {
					t.Fatal("native refusal lost")
				}
				before := calls
				if r.begin("teardown") != nil || calls != before {
					t.Fatal("refused sample retried or suppressed teardown")
				}
			} else {
				for _, phase := range frozenPhaseOrder()[1:] {
					if err := r.begin(phase); err != nil {
						t.Fatal(phase, err)
					}
					if err := r.finish(); err != nil {
						t.Fatal(phase, err)
					}
				}
			}
			result, err := r.close()
			if !result.Joined || (err != nil) != failed {
				t.Fatal("closure", result, err)
			}
			if result.Native[0].ObservedRSSHighWaterBytes != 12288 {
				t.Fatal("lost actual preflight prefix")
			}
			if failed {
				if result.Native[1].CompletedCensuses != 0 || result.Native[14].Available || result.Native[14].FailureClass != "measurement_unavailable" {
					t.Fatal("fabricated stopped phase census")
				}
			} else {
				for i, row := range result.Native {
					if !row.Available || row.CompletedCensuses == 0 || validateNativeObservation(row) != nil {
						t.Fatal(i, row)
					}
				}
			}
		})
	}
}

func TestExecutionWholeProcessFinishRefusesPassedPhase(t *testing.T) {
	for _, failure := range []string{"native", "disk"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			refuse := false
			meter, err := newExecutionProcessPhaseObservation(ctx, 41, 1, "phebs", map[string]string{"phebs": "phebs", "git": "git"}, cancel, func(context.Context, int) ([]t4013.NativeProcessRecord, error) {
				if refuse {
					return nil, errors.New("endpoint unavailable")
				}
				return epochProcessFixtureRows(), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			pauseEpochProcessTicker(meter)
			r := &executionWholeResources{ctx: ctx, cancel: cancel, process: meter, phase: 1, diskClosed: true, volume: &executionPressureVolume{}}
			defer func() { _, _ = r.close() }()
			if err := r.finish(); err != nil {
				t.Fatal(err)
			}
			ordinals := newExecutionEventOrdinals()
			admitted, _, err := ordinals.consumeFinalAdmission()
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			recorder, err := newExecutionPhaseEventRecorder(admitted, frozenPhaseOrder(), now, now.Add(time.Hour))
			if err != nil || recorder.begin("preflight") != nil || recorder.finish("preflight", "passed") != nil {
				t.Fatal(err)
			}
			flow := &ExecutionEpochOne{executionPhaseEvents: recorder,
				executionEvidenceEvents: make(map[string]uint64), executionEvidenceTimes: make(map[string]time.Time)}
			flow.executionWholeResources = r
			if err := flow.beginExecutionPhase("cold"); err != nil {
				t.Fatal(err)
			}
			if failure == "native" {
				refuse = true
			} else {
				r.diskClosed = false
			}
			if err := flow.finishExecutionPhase("cold", "passed"); err == nil {
				t.Fatal("failed endpoint accepted")
			}
			rows, err := recorder.snapshot()
			ordinal := flow.executionEvidenceEvents["failure:cold"]
			if err != nil || ordinal <= rows[1].StartEventOrdinal || ordinal >= rows[1].FinishEventOrdinal {
				t.Fatal("endpoint failure was not recorded inside the stopped phase", ordinal, rows, err)
			}
			if err := recorder.begin("warm_noop"); err == nil {
				t.Fatal("stopped phase allowed operational suffix")
			}
			if err := recorder.begin("teardown"); err != nil {
				t.Fatal("stopped phase suppressed teardown", err)
			}
		})
	}
}

func TestExecutionWholeProcessUsesSelectedRootName(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	// The real test image is not named t422-execute. Its selected pathname,
	// rather than the observed native row, defines the expected root identity.
	r, err := startExecutionWholeResources(ctx, os.Args[0])
	if r == nil {
		t.Fatal(err)
	}
	if err != nil {
		_, _ = r.close()
		t.Fatal(err)
	}
	if err := r.finish(); err != nil {
		_, _ = r.close()
		t.Fatal(err)
	}
	observed, err := r.close()
	if err != nil || !observed.Joined || !observed.Native[0].Available || observed.Native[0].CompletedCensuses == 0 {
		t.Fatal("selected native root identity was refused", err)
	}
}
