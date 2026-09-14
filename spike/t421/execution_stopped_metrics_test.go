package t421

import (
	"math"
	"slices"
	"strings"
	"testing"
)

func TestExecutionCleanScanPreservesPastPhaseClosure(t *testing.T) {
	plan := logicalStoreWorkTestPlan(t)
	raw := strings.ReplaceAll(attemptTestBindings(), "RU1:2:4:00000\n", "")
	raw = strings.ReplaceAll(raw, "UF1:2:4:00000000\n", "")
	raw += "A3j1\n"
	for _, tc := range []struct {
		name, tail string
		clean      bool
	}{{"future rows omitted", "", true}, {"partial report", "A3j", false}, {"malformed report", "A3x1\n", false}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := observeExecutionAttempts([]byte(raw+tc.tail), plan, 2, [32]byte{1}, true)
			if err == nil || got.Complete || got.ScanComplete != tc.clean || got.Phases[2].JobAttempts != 1 {
				t.Fatalf("scan=%+v error=%v", got, err)
			}
			indexRaw := "IXB1:2:sha256:01" + strings.Repeat("00", 31) + "\n"
			ix, _ := observeExecutionIndexOffers([]byte(indexRaw), plan, 2, [32]byte{1}, true, false)
			record := executionJoinedWorkRecord{Producer: 2, Input: [32]byte{1}, Joined: true, SessionEmpty: true, Attempts: got, IndexOffers: ix}
			if executionWorkPhaseClosed(plan, record, 1) != tc.clean {
				t.Fatal("past phase did not preserve exact clean prefix")
			}
			if executionWorkPhaseClosed(plan, record, 3) {
				t.Fatal("missing future phase acquired coverage")
			}
		})
	}
}

func TestExecutionStoppedMetricPrefixPreservesCountersAndCoverage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		truncate bool
	}{{"clean stopped tail", false}, {"damaged output", true}} {
		t.Run(tc.name, func(t *testing.T) {
			plan, work, dispatch, store, inspection, _ := receiptMetricTestEvidence(t)
			// Modeled valid phase-two acceptance followed by an ordinary phase-three
			// stop. Future producers have never started and supply no observed zeros.
			inspection = inspection[:2]
			inspection[1].SelectorAccepted = false
			inspection[1].Final = nil
			record := &work.Records[0]
			setJoinedMetricsTestValues(record, 2, 2)
			record.Attempts.Complete = false
			record.Attempts.ScanComplete = !tc.truncate
			record.IndexOffers.Complete = false
			record.IndexOffers.ScanComplete = !tc.truncate
			record.Attempts.Phases[2].ResolverBlobReads = 5
			record.Attempts.Phases[2].ResolverBlobBytes = 19
			record.Attempts.Reuse.Phases[3] = ExecutionReusePhase{}
			for i := 1; i < len(work.Records); i++ {
				work.Records[i] = executionJoinedWorkRecord{}
			}
			dispatch.Complete = false
			store.PrefixesClosed = false
			store.Store.PrefixesClosed = false
			got, err := composeExecutionStoppedMetricPrefix(plan, "warm_noop", work, dispatch, store, inspection)
			if err != nil {
				t.Fatal(err)
			}
			if got.Metrics.Metrics[1].GitReads != 2 || got.Metrics.Metrics[2].ResolverBlobReads != 5 || got.Metrics.Metrics[2].ResolverBlobBytes != 19 {
				t.Fatal("lost positive producer prefix")
			}
			if got.Metrics.Coverage[1].Producer == tc.truncate {
				t.Fatal("earlier phase coverage conflated with later failure")
			}
			if got.Metrics.Coverage[2].Producer || !slices.Contains(got.Unavailable[2], "resolver_blob_reads") || !validUnavailableMetricsForPlan(got.Unavailable[2], PlanV3Schema) {
				t.Fatalf("stopped row falsely complete: %+v", got.Unavailable[2])
			}
			if !got.Metrics.Coverage[1].Dispatch || !got.Metrics.Coverage[1].Store {
				t.Fatal("actual accepted phase fences lost controller prefix coverage")
			}
		})
	}
}

func TestExecutionStoppedMetricAdditionNeverWraps(t *testing.T) {
	metric := ReceiptMetrics{GitReads: CountMetric(math.MaxUint64)}
	record := executionJoinedWorkRecord{Producer: 2}
	record.Attempts.Phases[1].SourceBlobAttempts = 1
	if addExecutionWorkRecordMetrics(&metric, record, 1) || metric.GitReads != CountMetric(math.MaxUint64) {
		t.Fatal("overflow erased positive prefix")
	}
}
