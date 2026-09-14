package t421

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/bmeddeb/phebs/internal/archiveevidence"
	"github.com/bmeddeb/phebs/internal/recovery"
)

var executionArchiveArtifactPaths = [...]string{recovery.DatabaseName, recovery.FocusedIndexName, recovery.ResolverCatalogName, recovery.CallerPublicationName, recovery.ObservationPublicationName, recovery.RelationshipPublicationName}

type ExecutionArchiveArtifactObservation struct {
	Inventories     [3][6]archiveevidence.Identity
	Bound, Complete bool
}

func reservedArchiveArtifactEvent(line []byte) bool {
	return bytes.Contains(line, []byte("AEB")) || reservedBlobEvent(line, "AE")
}

func observeArchiveArtifactEvent(line []byte, producer uint32, input string, out *ExecutionArchiveArtifactObservation) (bool, error) {
	if !reservedArchiveArtifactEvent(line) {
		return false, nil
	}
	if producer != 10 && producer != 11 {
		return true, errExecutionAttempts
	}
	if bytes.Contains(line, []byte("AEB")) {
		if out.Bound || string(line) != fmt.Sprintf("AEB1:%d:%s\n", producer, input) {
			return true, errExecutionAttempts
		}
		out.Bound = true
		return true, nil
	}
	if !out.Bound || len(line) != 111 || !bytes.Equal(line[:4], []byte("AE1:")) || line[4] != executionWorkProducerByte(producer) ||
		!bytes.Equal(line[5:8], []byte(":C:")) || line[9] != ':' || line[11] != ':' || line[28] != ':' || line[45] != ':' || line[110] != '\n' {
		return true, errExecutionAttempts
	}
	stage, component := int(line[8]-'1'), int(line[10]-'0')
	if stage < 0 || stage >= 3 || component < 0 || component >= 6 || producer == 10 && stage != 0 || producer == 11 && stage == 0 || out.Inventories[stage][component].SHA256 != "" {
		return true, errExecutionAttempts
	}
	records, recordsOK := workspaceHex64(line[12:28])
	framed, framedOK := workspaceHex64(line[29:45])
	value := archiveevidence.Identity{Records: records, FramedBytes: framed, SHA256: "sha256:" + string(line[46:110])}
	if !recordsOK || !framedOK || !archiveevidence.ValidIdentity(value) {
		return true, errExecutionAttempts
	}
	out.Inventories[stage][component] = value
	return true, nil
}

func (value ExecutionArchiveArtifactObservation) complete(producer uint32) bool {
	if !value.Bound || producer != 10 && producer != 11 {
		return false
	}
	for stage, inventories := range value.Inventories {
		for _, identity := range inventories {
			if producer == 10 && stage == 0 || producer == 11 && stage != 0 {
				if !archiveevidence.ValidIdentity(identity) {
					return false
				}
			} else if identity != (archiveevidence.Identity{}) {
				return false
			}
		}
	}
	return true
}

// Complete native component observations are retained from two independently
// joined offline processes. A third native readback is required for each member;
// equality is checked here only after those observation owners close their sets.
func executionArchiveStateInventories(work executionJoinedWork) ([3]SetIdentity, error) {
	var result [3]SetIdentity
	if work.Err != nil {
		return result, errExecutionAttempts
	}
	backup, restore := work.Records[5], work.Records[6]
	if backup.Producer != 10 || restore.Producer != 11 || backup.Input == ([32]byte{}) || restore.Input == ([32]byte{}) ||
		!backup.Joined || !restore.Joined || !backup.SessionEmpty || !restore.SessionEmpty ||
		!backup.Attempts.Complete || !restore.Attempts.Complete ||
		!backup.Attempts.ArchiveArtifacts.Complete || !restore.Attempts.ArchiveArtifacts.Complete ||
		!backup.Attempts.ArchiveArtifacts.complete(10) || !restore.Attempts.ArchiveArtifacts.complete(11) {
		return result, errExecutionAttempts
	}
	order := []int{0, 1, 2, 3, 4, 5}
	slices.SortFunc(order, func(left, right int) int {
		return bytes.Compare([]byte(executionArchiveArtifactPaths[left]), []byte(executionArchiveArtifactPaths[right]))
	})
	for stage := range result {
		builder := newIdentityBuilder(archiveevidence.Schema)
		for _, component := range order {
			identity := restore.Attempts.ArchiveArtifacts.Inventories[stage][component]
			if stage == 0 {
				identity = backup.Attempts.ArchiveArtifacts.Inventories[0][component]
			}
			if !archiveevidence.ValidIdentity(identity) {
				return result, errExecutionAttempts
			}
			record := struct {
				Path      string                   `json:"path"`
				Inventory archiveevidence.Identity `json:"inventory"`
			}{executionArchiveArtifactPaths[component], identity}
			if err := builder.add(record); err != nil {
				return result, err
			}
		}
		result[stage] = builder.finish()
	}
	if result[0] != result[1] || result[0] != result[2] {
		return result, errExecutionAttempts
	}
	return result, nil
}
