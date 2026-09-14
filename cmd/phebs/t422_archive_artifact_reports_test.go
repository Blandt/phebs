package main

import (
	"strings"
	"testing"

	"github.com/bmeddeb/phebs/internal/archiveevidence"
	"github.com/bmeddeb/phebs/internal/dispatchadmission"
	"github.com/bmeddeb/phebs/internal/recovery"
)

func TestArchiveArtifactReportFraming(t *testing.T) {
	initial := dispatchadmission.ProductionSemanticSnapshot{ProducerID: 10, Phase: 12, InputSHA256: [32]byte{1}}
	value := archiveevidence.Observation{Stage: archiveevidence.Before, Path: recovery.DatabaseName, Identity: archiveevidence.Identity{Records: 1, FramedBytes: 100, SHA256: "sha256:" + strings.Repeat("2", 64)}}
	raw, err := t422ArchiveArtifactRecord(initial, initial, value)
	if err != nil || len(raw) != 111 || string(raw) != "AE1:A:C:1:0:0000000000000001:0000000000000064:"+strings.Repeat("2", 64)+"\n" {
		t.Fatalf("record=%q error=%v", raw, err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*dispatchadmission.ProductionSemanticSnapshot, *archiveevidence.Observation)
	}{
		{"producer", func(s *dispatchadmission.ProductionSemanticSnapshot, _ *archiveevidence.Observation) {
			s.ProducerID = 11
		}},
		{"phase", func(s *dispatchadmission.ProductionSemanticSnapshot, _ *archiveevidence.Observation) { s.Phase = 11 }},
		{"input", func(s *dispatchadmission.ProductionSemanticSnapshot, _ *archiveevidence.Observation) {
			s.InputSHA256 = [32]byte{2}
		}},
		{"path", func(_ *dispatchadmission.ProductionSemanticSnapshot, v *archiveevidence.Observation) {
			v.Path = "unknown"
		}},
		{"stage", func(_ *dispatchadmission.ProductionSemanticSnapshot, v *archiveevidence.Observation) {
			v.Stage = archiveevidence.After
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, copy := initial, value
			test.mutate(&state, &copy)
			if _, err := t422ArchiveArtifactRecord(state, initial, copy); err == nil {
				t.Fatal("invalid binding accepted")
			}
		})
	}
}
