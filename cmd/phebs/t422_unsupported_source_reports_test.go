package main

import (
	"io"
	"math"
	"os"
	"testing"

	"github.com/bmeddeb/phebs/internal/dispatchadmission"
	"github.com/bmeddeb/phebs/internal/readaccounting"
)

func TestT422UnsupportedSourceFraming(t *testing.T) {
	initial := dispatchadmission.ProductionSemanticSnapshot{Mode: dispatchadmission.ProductionSemanticV3, ProducerID: 2, Phase: 2, InputSHA256: [32]byte{1}}
	for _, test := range []struct {
		name  string
		phase uint32
		value uint64
		want  string
		ok    bool
	}{
		{name: "zero", phase: 2, want: "UF1:2:2:00000000\n", ok: true},
		{name: "later phase", phase: 4, want: "UF1:2:4:00000000\n", ok: true},
		{name: "nonzero", phase: 2, value: 5, want: "UF1:2:2:00000005\n", ok: true},
		{name: "maximum", phase: 2, value: math.MaxUint32, want: "UF1:2:2:ffffffff\n", ok: true},
		{name: "overflow", phase: 2, value: math.MaxUint32 + 1},
		{name: "unowned phase", phase: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := initial
			current.Phase = test.phase
			raw, err := t422UnsupportedSourceRecord(current, initial, readaccounting.UnsupportedSourceObservation{Unsupported: test.value})
			if (err == nil) != test.ok || string(raw) != test.want {
				t.Fatal(string(raw), err)
			}
		})
	}
}

func TestT422UnsupportedSourceControlRetainsNonzeroBeforeFailure(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Close() }()
	initial := dispatchadmission.ProductionSemanticSnapshot{Mode: dispatchadmission.ProductionSemanticV3, ProducerID: 2, Phase: 2, InputSHA256: [32]byte{1}}
	failures := 0
	control := &t422UnsupportedSourceControl{initial: initial, writer: write, fail: func(error) { failures++ }}
	if _, err := control.write(initial, readaccounting.UnsupportedSourceObservation{Unsupported: 1}); err == nil || failures != 1 {
		t.Fatal(err, failures)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(read)
	if err != nil || string(raw) != "UF1:2:2:00000001\n" {
		t.Fatal(string(raw), err)
	}
}
