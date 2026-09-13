package t421

import (
	"errors"
	"math"
	"testing"
)

func joinedMetricsTestRecord(plan Plan, producer uint32) executionJoinedWorkRecord {
	record := joinedWorkTestRecord(producer)
	record.Attempts = ExecutionAttemptObservation{
		Complete: true, SourceBound: true, ObservationBound: true, PublicationBound: true,
		ResolverBound: true, RelationshipBound: true,
		Cache:         ExecutionCacheObservation{Bound: true, Complete: true},
		SourceCensus:  ExecutionSourceCensusObservation{Bound: true, Complete: true},
		CatalogCensus: ExecutionCatalogCensusObservation{Bound: true, Complete: true},
	}
	if producer <= 6 {
		record.Attempts.AttemptBound = true
		record.Attempts.Lifecycle = ExecutionLifecycleObservation{Bound: true, Complete: true}
		record.Attempts.UnsupportedSource = ExecutionUnsupportedSourceObservation{Bound: true, Complete: true}
		record.Attempts.Reuse = ExecutionReuseObservation{Bound: true, Complete: true}
		for _, phase := range executionProducerPhases(producer) {
			record.Attempts.Reuse.Phases[phase-1].Complete = true
			if plan.WorkEnvelope.Phases[phase-1].ObservationParses.Maximum != 0 {
				record.Attempts.UnsupportedSource.Reports[phase-1] = 1
			}
		}
	} else {
		record.Attempts.UnsupportedSource.Complete = true
	}
	return record
}

func joinedMetricsTestWork(plan Plan) executionJoinedWork {
	var work executionJoinedWork
	for _, producer := range []uint32{2, 3, 4, 5, 6, 10, 11} {
		work.Records[executionJoinedWorkSlot(producer)] = joinedMetricsTestRecord(plan, producer)
	}
	return work
}

func setJoinedMetricsTestValues(record *executionJoinedWorkRecord, phase uint32, value uint64) {
	index := phase - 1
	record.Attempts.Phases[index] = ExecutionAttemptCount{
		SourceBlobAttempts: value, ObservationParses: value, PublicationWrites: value, ResolverBlobReads: value, ResolverBlobBytes: value,
		RelationshipBuildAttempts: value, RelationshipProjections: value, ServiceReferences: value,
		SourceLogicalBytes: value, SourceUniqueBytes: value, CensusChildren: value, CensusRecords: value,
	}
	record.Attempts.Cache.Phases[index] = ExecutionCacheCount{
		Lookups: 3 * value, Hits: value, Misses: 2 * value, RootReads: value, MemberReads: value,
		RootValidations: value, MemberValidations: value,
	}
	record.Attempts.SourceCensus.Started[index] = 2
	record.Attempts.SourceCensus.Finished[index] = 2
	record.Attempts.SourceCensus.Succeeded[index] = 1
	record.Attempts.SourceCensus.RegularOwners[index] = value
	record.Attempts.CatalogCensus.Started[index] = value
	record.Attempts.CatalogCensus.Finished[index] = value
	record.Attempts.CatalogCensus.ClosedChildren[index] = value
	if record.Producer <= 6 {
		record.Attempts.Phases[index].JobAttempts = value
		record.Attempts.Phases[index].Retries = value
		record.Attempts.Phases[index].MaxRetriesUnit = value
		record.Attempts.Lifecycle.Phases[index] = ExecutionLifecycleCount{
			ReturnedTicks: value, OwnerTurns: value, Deleted: value * value, MaxDeleted: value,
		}
		record.Attempts.Reuse.Phases[index] = ExecutionReusePhase{
			Source: ExecutionReuseCurrent, Search: ExecutionReuseCurrent, Observation: ExecutionReuseCurrent,
			Catalog: ExecutionReuseCurrent, Relationship: ExecutionReuseCurrent, Complete: true,
		}
		record.IndexOffers.Phases[index] = ExecutionIndexOfferCount{
			Offers: value, StartedChildren: 1, EndedChildren: 1, SettledOffers: value,
		}
	}
}

func TestExecutionJoinedWorkReceiptMetrics(t *testing.T) {
	plan := logicalStoreWorkTestPlan(t)
	work := joinedMetricsTestWork(plan)
	setJoinedMetricsTestValues(&work.Records[executionJoinedWorkSlot(4)], 8, 2)
	setJoinedMetricsTestValues(&work.Records[executionJoinedWorkSlot(5)], 8, 3)
	setJoinedMetricsTestValues(&work.Records[executionJoinedWorkSlot(6)], 12, 1)
	setJoinedMetricsTestValues(&work.Records[executionJoinedWorkSlot(10)], 12, 2)
	setJoinedMetricsTestValues(&work.Records[executionJoinedWorkSlot(11)], 12, 3)
	got, err := work.receiptMetrics(plan)
	if err != nil {
		t.Fatal(err)
	}
	wantEight := ReceiptMetrics{
		JobAttempts: 5, Retries: 5, MaxRetriesUnit: 3, GitReads: 5, ObservationParses: 5,
		PublicationWrites: 5, ResolverBlobReads: 5, ResolverBlobBytes: 5,
		RelationshipBuildAttempts: 5, RelationshipProjections: 5, ServiceReferences: 5,
		SourceLogicalBytes: 5, SourceUniqueBytes: 5, CensusChildren: 5, CensusRecords: 5,
		CacheLookups: 15, CacheHits: 5, CacheMisses: 10, CacheRootReads: 5, CacheMemberReads: 5,
		CacheRootValidations: 5, CacheMemberValidations: 5,
		LifecycleOwnerTurns: 5, LifecycleDeleted: 13, MaxLifecycleDeletesTurn: 3,
		SourceReuseDecisions: 2, SearchReuseDecisions: 2, ObservationReuseDecisions: 2,
		CatalogReuseDecisions: 2, RelationshipReuseDecisions: 2, ReuseDecisions: 10, IndexFiles: 5,
	}
	if got.Metrics[7] != wantEight {
		t.Fatalf("phase eight producer addition differs: %+v", got.Metrics[7])
	}
	if got.Metrics[11].GitReads != 6 || got.Metrics[11].JobAttempts != 1 || got.Metrics[11].IndexFiles != 1 ||
		got.Metrics[11].CacheLookups != 18 || got.Metrics[11].ReuseDecisions != 5 {
		t.Fatalf("phase twelve server/archive addition differs: %+v", got.Metrics[11])
	}
	for index, covered := range got.Covered {
		if covered != (index >= 1 && index <= 13) {
			t.Fatalf("phase %d coverage=%t", index+1, covered)
		}
	}
	if got.Metrics[0] != (ReceiptMetrics{}) || got.Metrics[14] != (ReceiptMetrics{}) ||
		got.Metrics[7].PhysicalCorpusPasses != 0 || got.Metrics[7].ChangedPhysicalFiles != 0 ||
		got.Metrics[7].StoreRows != 0 || got.Metrics[7].DataLogicalBytes != 0 || got.Metrics[7].ControlReads != 0 {
		t.Fatal("uncomposed evidence became a receipt metric")
	}
}

func TestExecutionJoinedWorkReceiptMetricsRefusesIncompleteExcessAndOverflow(t *testing.T) {
	plan := logicalStoreWorkTestPlan(t)
	for _, test := range []struct {
		name string
		edit func(*executionJoinedWork)
	}{
		{"missing producer", func(work *executionJoinedWork) { work.Records[0] = executionJoinedWorkRecord{} }},
		{"missing family binding", func(work *executionJoinedWork) { work.Records[0].Attempts.Cache.Bound = false }},
		{"cache prefix", func(work *executionJoinedWork) { work.Records[0].Attempts.Cache.Phases[1].Lookups = 1 }},
		{"source census prefix", func(work *executionJoinedWork) { work.Records[0].Attempts.SourceCensus.Succeeded[1] = 1 }},
		{"catalog census prefix", func(work *executionJoinedWork) { work.Records[0].Attempts.CatalogCensus.ClosedChildren[1] = 1 }},
		{"lifecycle prefix", func(work *executionJoinedWork) { work.Records[0].Attempts.Lifecycle.Phases[1].FailedTicks = 1 }},
		{"index prefix", func(work *executionJoinedWork) { work.Records[0].IndexOffers.Phases[1].StartedChildren = 1 }},
		{"unowned phase", func(work *executionJoinedWork) { work.Records[2].Attempts.Phases[4].SourceBlobAttempts = 1 }},
		{"archive server attempt", func(work *executionJoinedWork) { work.Records[5].Attempts.Phases[11].JobAttempts = 1 }},
		{"unknown reuse", func(work *executionJoinedWork) { work.Records[2].Attempts.Reuse.Phases[5].Source = "unknown" }},
		{"overflow", func(work *executionJoinedWork) {
			work.Records[2].Attempts.Phases[7].SourceBlobAttempts = math.MaxUint64
			work.Records[3].Attempts.Phases[7].SourceBlobAttempts = 1
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			work := joinedMetricsTestWork(plan)
			test.edit(&work)
			if _, err := work.receiptMetrics(plan); !errors.Is(err, errExecutionAttempts) {
				t.Fatal("invalid joined work accepted", err)
			}
		})
	}
}

func TestExecutionJoinedWorkIndexPrefixUsesNativeClosureShape(t *testing.T) {
	plan := logicalStoreWorkTestPlan(t)
	for _, test := range []struct {
		name  string
		count ExecutionIndexOfferCount
		valid bool
	}{
		{"successful zero-offer child", ExecutionIndexOfferCount{StartedChildren: 1, EndedChildren: 1}, true},
		{"failed-before-offer retry", ExecutionIndexOfferCount{Offers: 1, StartedChildren: 2, EndedChildren: 2, FailedChildren: 1, SettledOffers: 1}, true},
		{"positive offer without child", ExecutionIndexOfferCount{Offers: 1, SettledOffers: 1}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			work := joinedMetricsTestWork(plan)
			work.Records[0].IndexOffers.Phases[1] = test.count
			_, err := work.receiptMetrics(plan)
			if (err == nil) != test.valid {
				t.Fatal("native index prefix closure decision differs", err)
			}
		})
	}
}
