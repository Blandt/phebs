package t422s

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestJevRequestIsOneStrictSixQuestionCall(t *testing.T) {
	const wantContract = "sha256:621d00ce75292a7aec69ceec873ae247605450da1919f531e200f390756e9e3d"
	if got := ContractSHA256(); got != wantContract {
		t.Fatalf("contract digest = %q, want %q", got, wantContract)
	}
	state := ModelState{
		Schema: StateSchema, Language: LanguageGo, ArtifactRole: ArtifactRuntime,
		ChangeKind: ChangeModify, Before: "return old\n", After: "return new\n",
	}
	questionsRaw, err := json.Marshal(hazardQuestions())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(questionsRaw), _inertSourceRule); got != 6 {
		t.Fatalf("inert-source rule occurrences = %d, want 6", got)
	}
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected request: %s headers=%v", request.Method, request.Header)
		}
		decoder := json.NewDecoder(io.LimitReader(request.Body, _maxJevRequestBytes+1))
		decoder.DisallowUnknownFields()
		var got jevRequest
		if err := decoder.Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if !reflect.DeepEqual(got, jevRequest{State: state, Model: JevModel, Questions: hazardQuestions()}) {
			t.Errorf("request = %#v", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		answers := map[string]any{}
		for index, name := range []string{
			"schedule_epoch_state", "job_projection", "transition_accounting",
			"endpoint_status_surface", "checkout_custody", "oracle_classification",
		} {
			answers[name] = map[string]any{"type": "noul", "noul": float64(index+1) / 10}
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"model": JevModel, "answers": answers,
			"usage": map[string]any{"input_tokens": 100, "output_tokens": 6},
		})
	}))
	defer server.Close()

	result, err := evaluate(context.Background(), server.Client(), server.URL, "test-key", state)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || result.Model != JevModel || result.ContractSHA256 != ContractSHA256() ||
		result.Scores.ScheduleEpochState != 0.1 || result.Scores.OracleClassification != 0.6 {
		t.Fatalf("calls=%d result=%+v", calls.Load(), result)
	}

	invalid := state
	invalid.ArtifactRole = "private-path"
	if _, err := evaluate(context.Background(), server.Client(), server.URL, "test-key", invalid); err == nil {
		t.Fatal("invalid state crossed the Jev boundary")
	}
	if calls.Load() != 1 {
		t.Fatalf("invalid state made a request: calls=%d", calls.Load())
	}
}

func TestJevRequestHasPerRequestDeadline(t *testing.T) {
	state := ModelState{
		Schema: StateSchema, Language: LanguageGo, ArtifactRole: ArtifactRuntime,
		ChangeKind: ChangeModify, Before: "old\n", After: "new\n",
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining <= 20*time.Second || remaining > _jevRequestTimeout {
			t.Errorf("request deadline present=%t remaining=%s", ok, remaining)
		}
		body := `{"model":"jev-1.13.0","answers":{` +
			`"schedule_epoch_state":{"type":"noul","noul":0.1},` +
			`"job_projection":{"type":"noul","noul":0.1},` +
			`"transition_accounting":{"type":"noul","noul":0.1},` +
			`"endpoint_status_surface":{"type":"noul","noul":0.1},` +
			`"checkout_custody":{"type":"noul","noul":0.1},` +
			`"oracle_classification":{"type":"noul","noul":0.1}},` +
			`"usage":{"input_tokens":1,"output_tokens":6}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	if _, err := evaluate(ctx, client, "https://example.invalid", "test-key", state); err != nil {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestJevResponseRejectsUnknownFieldsWithoutRetry(t *testing.T) {
	state := ModelState{
		Schema: StateSchema, Language: LanguageShell, ArtifactRole: ArtifactCeremony,
		ChangeKind: ChangeModify, Before: "old\n", After: "new\n",
	}
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"jev-1.13.0","answers":{},"usage":{"input_tokens":1,"output_tokens":1},"extra":true}`))
	}))
	defer server.Close()
	if _, err := evaluate(context.Background(), server.Client(), server.URL, "test-key", state); err == nil {
		t.Fatal("unknown response field was accepted")
	}
	if calls.Load() != 1 {
		t.Fatalf("strict decode retried: calls=%d", calls.Load())
	}
}
