package t422q

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bmeddeb/phebs/spike/t4013"
)

func TestEpisodeJSONLExcludesLocalResolutionFromModelInput(t *testing.T) {
	episodes, err := ProjectReceipt("receipt_001", t4013.Receipt{
		Schema:  t4013.ReceiptSchemaV31,
		Outcome: "stopped",
		ConvergenceWaits: []t4013.ConvergenceWaitObservation{{
			Profile: "semantic-262144-v1", Label: "stale-worker", Revision: "a",
			Outcome: "diagnostic_limit", LastStage: "extraction_publication", DeadlineMS: 120_000,
			InspectionTransitions: []t4013.ConvergenceTransitionObservation{{
				Stage: "extraction_publication", Class: "status", HTTPStatus: 409,
				HTTPReason: "status_other", WallMS: 60_000,
			}},
		}},
		Failures: []t4013.FailureObservation{{Phase: "stale_worker", Class: "oracle", Code: "transition_limit"}},
		Decision: t4013.DecisionObservation{Selected: "unclassified", Reason: "missing_detail"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := EncodeEpisodes(&encoded, episodes); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeEpisodes(strings.NewReader(encoded.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1 || decoded[0].Resolution.FailureCode != "transition_limit" {
		t.Fatalf("round trip differs: %#v", decoded)
	}
	request := newJevRequest(decoded[0].ModelInput)
	if request.State.Schema != StateSchema || request.State.HTTPStatus != 409 {
		t.Fatalf("request state differs: %#v", request.State)
	}
	requestBytes := encodedJevRequestForTest(t, request)
	for _, forbidden := range []string{"episode_id", "receipt_group", "transition_limit", "missing_detail", "unclassified"} {
		if strings.Contains(string(requestBytes), forbidden) {
			t.Fatalf("Jev request contains local-only value %q: %s", forbidden, requestBytes)
		}
	}
}

func encodedJevRequestForTest(t *testing.T, request jevRequest) []byte {
	t.Helper()
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	if err := encoder.Encode(request); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
