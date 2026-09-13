package main

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/bmeddeb/phebs/internal/dispatchadmission"
)

type t422ReuseFailWriter struct{}

func (t422ReuseFailWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func t422ReuseTestControl(phase uint32, writer interface{ Write([]byte) (int, error) }) (*t422ReuseControl, dispatchadmission.ProductionSemanticSnapshot, *int) {
	producer := uint32(2)
	if phase >= 12 {
		producer = 6
	}
	state := dispatchadmission.ProductionSemanticSnapshot{
		Mode: dispatchadmission.ProductionSemanticV3, ProducerID: producer, Phase: phase,
		InputSHA256: [32]byte{1},
	}
	failures := 0
	initial := state
	if producer == 6 {
		initial.Phase = 12
	}
	return &t422ReuseControl{
		initial: initial,
		launch:  &t422SemanticLaunch{request: t422SemanticLaunchRequest{Repository: "example.com/acme/reuse"}},
		writer:  writer,
		fail:    func(error) { failures++ },
	}, state, &failures
}

func TestT422ReuseRecordKeepsFiveLanesDistinct(t *testing.T) {
	initial := dispatchadmission.ProductionSemanticSnapshot{
		Mode: dispatchadmission.ProductionSemanticV3, ProducerID: 2, Phase: 2,
		InputSHA256: [32]byte{1},
	}
	for _, test := range []struct {
		name  string
		lanes [t422ReuseLaneCount]byte
		want  string
		ok    bool
	}{
		{"known zero", [t422ReuseLaneCount]byte{}, "RU1:2:2:00000\n", true},
		{"five current", [t422ReuseLaneCount]byte{'c', 'c', 'c', 'c', 'c'}, "RU1:2:2:ccccc\n", true},
		{"prior index", [t422ReuseLaneCount]byte{'p', 'p'}, "RU1:2:2:pp000\n", true},
		{"split index", [t422ReuseLaneCount]byte{'c', 0}, "", false},
		{"prior observation", [t422ReuseLaneCount]byte{0, 0, 'p'}, "", false},
		{"unknown", [t422ReuseLaneCount]byte{0, 0, 0, 0, 'x'}, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := t422ReuseRecord(initial, initial, test.lanes)
			if (err == nil) != test.ok || test.ok && string(got[:]) != test.want {
				t.Fatal(string(got[:]), err)
			}
		})
	}
}

func TestT422ReuseControlCoherentRepeatConflictAndConcurrency(t *testing.T) {
	var output bytes.Buffer
	control, state, failures := t422ReuseTestControl(2, &output)
	for range 2 {
		if err := control.observeCurrent(state, "example.com/acme/reuse", t422ReuseSource, t422ReuseSearch, t422ReuseReactivated); err != nil {
			t.Fatal(err)
		}
	}
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := control.observeCurrent(state, "example.com/acme/reuse", t422ReuseCatalog, t422ReuseCatalog, t422ReuseCurrent); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if *failures != 0 || control.phases[1].lanes != ([t422ReuseLaneCount]byte{'p', 'p', 0, 'c', 0}) {
		t.Fatal(*failures, control.phases[1])
	}
	if err := control.observeCurrent(state, "example.com/acme/reuse", t422ReuseSource, t422ReuseSearch, t422ReuseCurrent); err == nil || *failures != 1 {
		t.Fatal("conflicting decision did not refuse", *failures)
	}
}

func TestT422ReuseControlTerminalBoundaryCancelAndWriteFailure(t *testing.T) {
	var output bytes.Buffer
	control, state, failures := t422ReuseTestControl(14, &output)
	if started, err := control.beginFinal(state, false); err != nil || started || output.Len() != 0 {
		t.Fatal("first product F completed reuse", started, err, output.String())
	}
	started, err := control.beginFinal(state, true)
	if err != nil || !started {
		t.Fatal(started, err)
	}
	control.abortFinal(14)
	if started, err = control.beginFinal(state, true); err != nil || !started {
		t.Fatal("canceled report could not retry", started, err)
	}
	if err := control.finishFinal(state); err != nil || output.String() != "RU1:6:E:00000\n" || *failures != 0 {
		t.Fatal(output.String(), *failures, err)
	}
	if started, err = control.beginFinal(state, true); err != nil || started {
		t.Fatal("completed terminal repeated", started, err)
	}

	failed, failedState, failedCalls := t422ReuseTestControl(2, t422ReuseFailWriter{})
	if started, err = failed.beginFinal(failedState, false); err != nil || !started {
		t.Fatal(started, err)
	}
	if err := failed.finishFinal(failedState); err == nil || failed.phases[1].complete {
		t.Fatal("failed write completed terminal")
	}
	_ = failed.refuse()
	_ = failed.refuse()
	if *failedCalls != 1 {
		t.Fatal("failed write did not latch once", *failedCalls)
	}
}

func TestT422ReuseControlOrdinaryConstructionIsNil(t *testing.T) {
	if control, err := newT422ReuseControl(nil, nil); err != nil || control != nil {
		t.Fatal(control, err)
	}
}
