package t421

import (
	"math"
	"reflect"
	"testing"
)

func populationMetricFixture(t *testing.T) (Plan, executionJoinedWork, []ExecutionPhaseInspection, []ExecutionAuthorResult) {
	t.Helper()
	plan := logicalStoreWorkTestPlan(t)
	id := func(n uint64) SetIdentity {
		return SetIdentity{Records: n, FramedBytes: n + 1, SHA256: testDigest("population")}
	}
	final := &ExecutionInspectionFinal{Authority: AuthorityState{PhysicalRevision: "a", PhysicalCommit: "commit", PhysicalTree: "tree", Current: true, SearchInventory: id(9)}, CatalogPopulation: &ExecutionCatalogPopulation{AcceptedServices: 5},
		Projection: PhaseStateProjection{Phase: "cold", Catalog: id(7), MembershipSet: id(19), SearchInventory: id(9), ExtractionRoots: []ExtractionRootProjection{
			{Domain: "one", Availability: "admitted", ApplicablePartitions: 3, MemberPartitions: 2, TypedPartitions: 1},
			{Domain: "empty", Availability: "empty"},
		}}}
	work := executionJoinedWork{}
	work.Records[0] = executionJoinedWorkRecord{Producer: 2, Input: [32]byte{1}, Joined: true, SessionEmpty: true, Attempts: ExecutionAttemptObservation{Complete: true, SourceCensus: ExecutionSourceCensusObservation{Bound: true, Complete: true}}}
	census := &work.Records[0].Attempts.SourceCensus
	census.Started[1] = 3
	census.Finished[1] = 3
	census.Succeeded[1] = 2
	census.RegularOwners[1] = 18
	authors := []ExecutionAuthorResult{{Revision: "a", ProducerID: 7, Completed: true, RootStarted: true, RootJoined: true, SessionEmpty: true, Response: &ExecutionCorpusAuthorResponse{Result: AuthoredExecutionRevision{Name: "a", Commit: "commit", Tree: "tree"}, ChangedPhysicalFiles: &ExecutionChangedPhysicalFiles{Count: 4, Complete: true}}}}
	return plan, work, []ExecutionPhaseInspection{{ServerEpoch: 1, Phase: "cold", SelectorAccepted: true, Final: final}}, authors
}

func TestComposeExecutionPopulationUsesNativeValues(t *testing.T) {
	plan, work, rows, authors := populationMetricFixture(t)
	out := executionReceiptMetrics{}
	if err := composeExecutionPopulationMetrics(plan, work, rows, authors, &out); err != nil {
		t.Fatal(err)
	}
	got := out.Metrics[1]
	want := ReceiptMetrics{PhysicalCorpusPasses: 2, CombinedPhysicalOwners: 9, ChangedPhysicalFiles: 4, ChangedLogicalServices: 5, ServiceRows: 7, LogicalMemberships: 19, ApplicablePartitions: 3, PublishedDomains: 2}
	if got != want {
		t.Fatalf("actual values replaced by frozen expectations: %+v", got)
	}
	if out.Metrics[2] != (ReceiptMetrics{}) {
		t.Fatal("invented no-op population work")
	}
	copyFinal := cloneInspectionFinal(*rows[0].Final)
	copyFinal.CatalogPopulation.AcceptedServices++
	if rows[0].Final.CatalogPopulation.AcceptedServices != 5 {
		t.Fatal("accepted population aliases native snapshot")
	}
}

func TestComposeExecutionPopulationRejectsUnboundEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*executionJoinedWork, []ExecutionPhaseInspection, []ExecutionAuthorResult)
	}{
		{"missing terminal", func(w *executionJoinedWork, _ []ExecutionPhaseInspection, _ []ExecutionAuthorResult) {
			w.Records[0].Attempts.SourceCensus.Bound = false
		}},
		{"failed invocation is no full pass", func(w *executionJoinedWork, _ []ExecutionPhaseInspection, _ []ExecutionAuthorResult) {
			w.Records[0].Attempts.SourceCensus.Succeeded[1] = 0
		}},
		{"population mismatch", func(w *executionJoinedWork, _ []ExecutionPhaseInspection, _ []ExecutionAuthorResult) {
			w.Records[0].Attempts.SourceCensus.RegularOwners[1]++
		}},
		{"incomplete source census", func(w *executionJoinedWork, _ []ExecutionPhaseInspection, _ []ExecutionAuthorResult) {
			w.Records[0].Attempts.SourceCensus.Finished[1]--
		}},
		{"overflow", func(w *executionJoinedWork, r []ExecutionPhaseInspection, _ []ExecutionAuthorResult) {
			r[0].Final.Projection.SearchInventory.Records = math.MaxUint64
			r[0].Final.Authority.SearchInventory = r[0].Final.Projection.SearchInventory
		}},
		{"absent native accepted count", func(_ *executionJoinedWork, r []ExecutionPhaseInspection, _ []ExecutionAuthorResult) {
			r[0].Final.CatalogPopulation = nil
		}},
		{"duplicate domain", func(_ *executionJoinedWork, r []ExecutionPhaseInspection, _ []ExecutionAuthorResult) {
			r[0].Final.Projection.ExtractionRoots[1].Domain = "one"
		}},
		{"authored different tree", func(_ *executionJoinedWork, _ []ExecutionPhaseInspection, a []ExecutionAuthorResult) {
			a[0].Response.Result.Tree = "different"
		}},
		{"author not joined", func(_ *executionJoinedWork, _ []ExecutionPhaseInspection, a []ExecutionAuthorResult) {
			a[0].RootJoined = false
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, work, rows, authors := populationMetricFixture(t)
			tc.mutate(&work, rows, authors)
			out := executionReceiptMetrics{}
			if composeExecutionPopulationMetrics(plan, work, rows, authors, &out) == nil {
				t.Fatal("accepted missing/incoherent native facts")
			}
		})
	}
}

func TestExecutionPopulationMandatoryFieldsInventory(t *testing.T) {
	plan := logicalStoreWorkTestPlan(t)
	// Every positive metric bound is owned by existing byte/work/controller/
	// native families or this population adapter. Topology and legacy process
	// fields are separately zero-only for the frozen V3 execution profile.
	owned := map[string]bool{}
	for _, name := range []string{"physical_corpus_passes", "changed_physical_files", "applicable_partitions", "published_domains", "combined_physical_owners", "logical_memberships", "service_rows", "changed_logical_services", "store_rows", "store_transactions", "control_reads", "member_reads"} {
		owned[name] = true
	}
	for _, bounds := range plan.WorkEnvelope.Phases {
		for _, metric := range boundedPhaseMetricValues(ReceiptMetrics{}, bounds) {
			if metric.bound.Minimum > 0 && !owned[metric.name] && !executionJoinedMetric(metric.name) {
				t.Fatalf("unmapped positive minimum %s/%s", bounds.Phase, metric.name)
			}
		}
	}
	// The adapter is source-preserving even on logical-only publication: it
	// projects actual state rows without claiming another source traversal.
	p, w, r, a := populationMetricFixture(t)
	r[0].Phase = "logical_delta_b"
	r[0].Final.Projection.Phase = r[0].Phase
	r[0].ServerEpoch = 2
	out := executionReceiptMetrics{}
	if err := composeExecutionPopulationMetrics(p, w, r, a, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Metrics[4], ReceiptMetrics{ServiceRows: 7, LogicalMemberships: 19}) {
		t.Fatal(out.Metrics[4])
	}
}

func TestExecutionPopulationPhysicalPhaseBindings(t *testing.T) {
	for _, tc := range []struct {
		phase, revision  string
		producer, author uint32
		index            int
	}{
		{"physical_delta_b", "b", 2, 8, 3}, {"return_a", "a-return", 4, 9, 5},
	} {
		t.Run(tc.phase, func(t *testing.T) {
			plan, work, rows, authors := populationMetricFixture(t)
			record := work.Records[0]
			work = executionJoinedWork{}
			record.Producer = tc.producer
			c := record.Attempts.SourceCensus
			record.Attempts.SourceCensus = ExecutionSourceCensusObservation{Bound: true, Complete: true}
			record.Attempts.SourceCensus.Started[tc.index] = c.Started[1]
			record.Attempts.SourceCensus.Finished[tc.index] = c.Finished[1]
			record.Attempts.SourceCensus.Succeeded[tc.index] = c.Succeeded[1]
			record.Attempts.SourceCensus.RegularOwners[tc.index] = c.RegularOwners[1]
			work.Records[executionJoinedWorkSlot(tc.producer)] = record
			rows[0].Phase = tc.phase
			rows[0].ServerEpoch = uint64(tc.producer - 1)
			rows[0].Final.Projection.Phase = tc.phase
			rows[0].Final.Authority.PhysicalRevision = tc.revision
			authors[0].Revision = tc.revision
			authors[0].ProducerID = tc.author
			authors[0].Response.Result.Name = tc.revision
			out := executionReceiptMetrics{}
			out.Metrics[tc.index].ChangedLogicalServices = 3
			if err := composeExecutionPopulationMetrics(plan, work, rows, authors, &out); err != nil {
				t.Fatal(err)
			}
			if out.Metrics[tc.index].PhysicalCorpusPasses != 2 || out.Metrics[tc.index].ChangedPhysicalFiles != 4 || out.Metrics[tc.index].ChangedLogicalServices != 3 {
				t.Fatal("actual delta facts lost", out.Metrics[tc.index])
			}
			authors[0].ProducerID = 7
			if composeExecutionPopulationMetrics(plan, work, rows, authors, &out) == nil {
				t.Fatal("accepted wrong author lifetime")
			}
		})
	}
}
