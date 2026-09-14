package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/bmeddeb/phebs/internal/archiveevidence"
	"github.com/bmeddeb/phebs/internal/dispatchadmission"
	"github.com/bmeddeb/phebs/internal/recovery"
)

var t422ArchiveArtifactPaths = [...]string{recovery.DatabaseName, recovery.FocusedIndexName, recovery.ResolverCatalogName, recovery.CallerPublicationName, recovery.ObservationPublicationName, recovery.RelationshipPublicationName}

func t422ArchiveArtifactRecord(state, initial dispatchadmission.ProductionSemanticSnapshot, value archiveevidence.Observation) ([]byte, error) {
	prefix, err := t422SourceRecord(state, initial)
	if err != nil || state.Mode != "" || state.Phase != 12 || !archiveevidence.ValidIdentity(value.Identity) ||
		state.ProducerID != 10 && state.ProducerID != 11 ||
		state.ProducerID == 10 && value.Stage != archiveevidence.Before ||
		state.ProducerID == 11 && value.Stage != archiveevidence.Archived && value.Stage != archiveevidence.After {
		return nil, errT422AttemptReport
	}
	component := -1
	for index, path := range t422ArchiveArtifactPaths {
		if path == value.Path {
			component = index
		}
	}
	if component < 0 {
		return nil, errT422AttemptReport
	}
	return []byte(fmt.Sprintf("AE1:%c:C:%d:%d:%016x:%016x:%s\n", prefix[4], value.Stage, component, value.Identity.Records, value.Identity.FramedBytes, value.Identity.SHA256[7:])), nil
}

func bindT422ArchiveArtifactReports(ctx context.Context, cancel context.CancelFunc) (context.Context, error) {
	initial, err := dispatchadmission.ProductionWorkState()
	if err != nil || cancel == nil || initial.Mode != "" || initial.Phase != 12 || initial.ProducerID != 10 && initial.ProducerID != 11 {
		return nil, errT422AttemptReport
	}
	writer, ok := log.Writer().(*os.File)
	if !ok || writer != os.Stderr {
		return nil, errT422AttemptReport
	}
	info, err := writer.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return nil, errT422AttemptReport
	}
	binding, err := t422SourceBinding(initial)
	if err != nil {
		return nil, err
	}
	binding[0], binding[1] = 'A', 'E'
	if n, err := writer.Write(binding); err != nil || n != len(binding) {
		cancel()
		return nil, errT422AttemptReport
	}
	var seen [3][6]bool
	return archiveevidence.WithObserver(ctx, func(value archiveevidence.Observation) error {
		current, err := dispatchadmission.ProductionWorkState()
		if err == nil {
			var raw []byte
			raw, err = t422ArchiveArtifactRecord(current, initial, value)
			if err == nil {
				stage, component := int(raw[8]-'1'), int(raw[10]-'0')
				if seen[stage][component] {
					err = errT422AttemptReport
				} else {
					seen[stage][component] = true
					var n int
					n, err = writer.Write(raw)
					if err == nil && n != len(raw) {
						err = io.ErrShortWrite
					}
				}
			}
		}
		if err != nil {
			cancel()
			return errT422AttemptReport
		}
		return nil
	})
}
