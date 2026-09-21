package t422q

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestJevWireAndBoundaries(t *testing.T) {
	state := ShadowState{
		Schema: StateSchema, Profile: "semantic-262144-v1", WaitLabel: "stale-worker", Revision: "a",
		Stage: "extraction_publication", Class: "status", HTTPStatus: 409, HTTPReason: "409_stale",
		ProgressChanges: 3, ElapsedMS: 65_000, DeadlineMS: 120_000,
	}
	tests := []struct {
		terminal float64
		repair   float64
		want     string
	}{
		{0.10, 0.10, ClassificationBenignCandidate},
		{0.90, 0.10, ClassificationTerminalCandidate},
		{0.90, 0.90, ClassificationTerminalCandidate},
		{0.89, 0.90, ClassificationRepairCandidate},
		{0.11, 0.89, ClassificationReview},
	}
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		index := int(calls.Add(1) - 1)
		if request.Method != http.MethodPost || request.URL.Path != "/v1/systemone" || request.URL.RawQuery != "" ||
			request.Header.Get("Authorization") != "Bearer test-key" || request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %s headers=%v", request.Method, request.URL.String(), request.Header)
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		decoder := json.NewDecoder(io.LimitReader(request.Body, maxJevRequestBytes+1))
		decoder.DisallowUnknownFields()
		var got jevRequest
		if err := decoder.Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			t.Errorf("request trailing JSON: %v", err)
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		if want := newJevRequest(state); !reflect.DeepEqual(got, want) {
			t.Errorf("request = %#v, want %#v", got, want)
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		if index >= len(tests) {
			t.Errorf("unexpected call %d", index+1)
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{
			"model": JevModel,
			"answers": map[string]any{
				"observation_terminal": map[string]any{"type": "noul", "noul": tests[index].terminal},
				"repair_required":      map[string]any{"type": "noul", "noul": tests[index].repair},
			},
			"usage": map[string]any{"input_tokens": 100, "output_tokens": 2},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	for _, test := range tests {
		result, err := evaluate(context.Background(), server.Client(), server.URL+"/v1/systemone", "test-key", state)
		if err != nil {
			t.Fatal(err)
		}
		if result.Model != JevModel || result.ObservationTerminal != test.terminal || result.RepairRequired != test.repair ||
			result.Classification != test.want || result.InputTokens != 100 || result.OutputTokens != 2 {
			t.Fatalf("result = %+v, want terminal=%v repair=%v classification=%q", result, test.terminal, test.repair, test.want)
		}
	}
	if got := calls.Load(); got != int64(len(tests)) {
		t.Fatalf("calls = %d, want %d", got, len(tests))
	}
	invalid := state
	invalid.Profile = "private-profile"
	if _, err := evaluate(context.Background(), server.Client(), server.URL+"/v1/systemone", "test-key", invalid); err == nil {
		t.Fatal("unclosed state crossed the Jev boundary")
	}
	if got := calls.Load(); got != int64(len(tests)) {
		t.Fatalf("invalid state made a request: calls = %d", got)
	}
}
