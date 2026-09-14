package recovery

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bmeddeb/phebs/internal/archiveevidence"
)

func TestArchiveDatabasePayloadSpans(t *testing.T) {
	for _, body := range []string{
		"INSERT [{id: repo:one}];",
		"INSERT [{id: repo:one}, \n {id: repo:two}]; -- ignored\n",
		"INSERT [" + strings.Repeat("{id: repo:one},\n", 512) + "{id: repo:two}];",
		"INSERT [{id: repo:one, value: '" + strings.Repeat("x", 2*restoreReplayBufferBytes) + "'}];",
	} {
		raw := "-- native export\nOPTION IMPORT;\n" + body
		scanner := newRestoreReplayScanner(strings.NewReader(raw))
		scanner.measurePayloads = true
		var want archiveevidence.Stream
		for ordinal := uint64(1); ; ordinal++ {
			unit, err := scanner.next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			actual := sha256.Sum256([]byte(raw[unit.Span.Start:unit.Span.End]))
			if actual != unit.PayloadSHA256 {
				t.Fatalf("payload digest differs at unit %d", ordinal)
			}
			if err := want.Add(databasePayloadRecord(ordinal, unit, actual)); err != nil {
				t.Fatal(err)
			}
		}
		got, err := scanDatabasePayloads(strings.NewReader(raw))
		if err != nil || got != want.Identity() {
			t.Fatalf("payload inventory = %+v, %v", got, err)
		}
	}
}

func TestArchiveDatabaseCommittedPayloadObservation(t *testing.T) {
	for _, failedCommit := range []bool{false, true} {
		t.Run(fmt.Sprint(failedCommit), func(t *testing.T) {
			const raw = "OPTION IMPORT; INSERT [{id: repo:one, value: 'committed'}];"
			path, artifact := restoreReplayTestArtifact(t, raw)
			var reports []archiveevidence.Observation
			ctx, err := archiveevidence.WithObserver(t.Context(), func(value archiveevidence.Observation) error { reports = append(reports, value); return nil })
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := prepareRestoreReplay(ctx, path, artifact)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = prepared.close() }()
			const ok = `{"result":null,"status":"OK","time":"0ns","type":null}`
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if _, err := io.Copy(io.Discard, request.Body); err != nil {
					t.Error(err)
				}
				if request.URL.Path == "/sql" {
					_, _ = io.WriteString(writer, "["+ok+","+ok+","+ok+"]")
					return
				}
				commit := ok
				if failedCommit {
					commit = `{"result":"failed","status":"ERR","time":"0ns","type":null}`
				}
				_, _ = io.WriteString(writer, `[{"result":[],"status":"OK","time":"0ns","type":null},`+commit+"]")
			}))
			defer server.Close()
			err = executeRestoreReplay(ctx, prepared, t.TempDir(), strings.Replace(server.URL, "http://", "ws://", 1), DatabaseIdentity{Namespace: "phebs", Database: "phebs"}, nil)
			if (err != nil) != failedCommit {
				t.Fatalf("native replay = %v", err)
			}
			if failedCommit {
				if len(reports) != 1 {
					t.Fatalf("failed commit emitted after: %+v", reports)
				}
				return
			}
			if len(reports) != 2 || reports[0].Stage != archiveevidence.Archived || reports[1].Stage != archiveevidence.After || reports[0].Identity != reports[1].Identity {
				t.Fatalf("committed observations = %+v", reports)
			}
		})
	}
}

func TestArchiveDatabaseBeforeAndArchiveAreIndependent(t *testing.T) {
	const raw = "OPTION IMPORT; INSERT [{id: repo:one, value: 'preserved'}];\n"
	path, artifact := restoreReplayTestArtifact(t, raw)
	var reports []archiveevidence.Observation
	ctx, err := archiveevidence.WithObserver(t.Context(), func(value archiveevidence.Observation) error { reports = append(reports, value); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := digestObservedFile(ctx, path, artifact.Size, true); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareRestoreReplay(ctx, path, artifact)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = prepared.close() }()
	if len(reports) != 2 || reports[0].Stage != archiveevidence.Before || reports[1].Stage != archiveevidence.Archived || reports[0].Identity != reports[1].Identity {
		t.Fatalf("independent observations = %+v", reports)
	}
}
