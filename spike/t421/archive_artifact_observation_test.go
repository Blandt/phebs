package t421

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bmeddeb/phebs/internal/archiveevidence"
)

func TestArchiveArtifactBindingAndFraming(t *testing.T) {
	input := "sha256:" + strings.Repeat("1", 64)
	valid := "AE1:A:C:1:0:0000000000000001:0000000000000064:" + strings.Repeat("2", 64) + "\n"
	for _, test := range []struct {
		name, line string
		producer   uint32
		want       bool
	}{
		{"valid", valid, 10, true},
		{"server", valid, 2, false},
		{"wrong producer", valid, 11, false},
		{"wrong phase", strings.Replace(valid, ":C:", ":B:", 1), 10, false},
		{"wrong stage", strings.Replace(valid, ":1:0:", ":3:0:", 1), 10, false},
		{"wrong component", strings.Replace(valid, ":1:0:", ":1:6:", 1), 10, false},
		{"truncated", strings.TrimSuffix(valid, "\n"), 10, false},
		{"duplicate field", valid + "0", 10, false},
		{"bad digest", strings.Replace(valid, strings.Repeat("2", 64), strings.Repeat("g", 64), 1), 10, false},
		{"overflow framing", strings.Replace(valid, "0000000000000001", "ffffffffffffffff", 1), 10, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := ExecutionArchiveArtifactObservation{Bound: true}
			seen, err := observeArchiveArtifactEvent([]byte(test.line), test.producer, input, &out)
			if !seen || (err == nil) != test.want {
				t.Fatalf("seen=%v error=%v", seen, err)
			}
			if test.want {
				if _, err := observeArchiveArtifactEvent([]byte(test.line), test.producer, input, &out); err == nil {
					t.Fatal("duplicate accepted")
				}
			}
		})
	}
	var out ExecutionArchiveArtifactObservation
	if _, err := observeArchiveArtifactEvent([]byte(valid), 10, input, &out); err == nil {
		t.Fatal("unbound record accepted")
	}
	header := []byte(fmt.Sprintf("AEB1:10:%s\n", input))
	if _, err := observeArchiveArtifactEvent(header, 10, input, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := observeArchiveArtifactEvent(header, 10, input, &out); err == nil {
		t.Fatal("binding replay accepted")
	}
}

func TestArchiveArtifactJoinedComposition(t *testing.T) {
	var work executionJoinedWork
	value := archiveevidence.Identity{Records: 1, FramedBytes: 100, SHA256: "sha256:" + strings.Repeat("2", 64)}
	for _, producer := range []uint32{10, 11} {
		row := &work.Records[executionJoinedWorkSlot(producer)]
		row.Producer, row.Input, row.Joined, row.SessionEmpty = producer, [32]byte{byte(producer)}, true, true
		row.Attempts.Complete = true
		row.Attempts.ArchiveArtifacts.Bound = true
		for stage := range 3 {
			if producer == 10 && stage != 0 || producer == 11 && stage == 0 {
				continue
			}
			for component := range 6 {
				row.Attempts.ArchiveArtifacts.Inventories[stage][component] = value
			}
		}
		row.Attempts.ArchiveArtifacts.Complete = row.Attempts.ArchiveArtifacts.complete(producer)
	}
	for _, test := range []struct {
		name   string
		mutate func(*executionJoinedWork)
		want   bool
	}{
		{"valid", func(*executionJoinedWork) {}, true},
		{"missing after", func(w *executionJoinedWork) {
			w.Records[6].Attempts.ArchiveArtifacts.Inventories[2][5] = archiveevidence.Identity{}
		}, false},
		{"readback differs", func(w *executionJoinedWork) {
			w.Records[6].Attempts.ArchiveArtifacts.Inventories[2][5].SHA256 = "sha256:" + strings.Repeat("3", 64)
		}, false},
		{"unjoined", func(w *executionJoinedWork) { w.Records[6].Joined = false }, false},
		{"bad footer", func(w *executionJoinedWork) { w.Records[6].Attempts.Complete = false }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := work
			test.mutate(&copy)
			got, err := executionArchiveStateInventories(copy)
			if (err == nil) != test.want {
				t.Fatalf("inventories=%+v error=%v", got, err)
			}
			if test.want && (got[0].Records != 6 || got[0] != got[1] || got[0] != got[2]) {
				t.Fatal(got)
			}
		})
	}
}
