package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"

	"github.com/bmeddeb/phebs/internal/candidate"
	"github.com/bmeddeb/phebs/internal/dispatchadmission"
	"github.com/bmeddeb/phebs/internal/extractionpublication"
	"github.com/bmeddeb/phebs/internal/lifecycle"
	"github.com/bmeddeb/phebs/internal/observationpublication"
	"github.com/bmeddeb/phebs/internal/relationshippublication"
	"github.com/bmeddeb/phebs/internal/store"
	"github.com/bmeddeb/phebs/internal/storeaccounting"
)

// Shared with the actual serve scheduler class construction, not an expected
// execution-profile projection. These do not assert that workers were started.
const (
	observationIOConcurrency  = 1
	observationCPUConcurrency = 2
	relationshipConcurrency   = 1
	t422RuntimeFactsCommand   = "t422-runtime-facts"
	t422RuntimeFactsSchema    = "t422-runtime-facts-v2"
)

type t422ScheduleFacts struct {
	MaxAttempts      int `json:"max_attempts"`
	RepositoryTokens int `json:"repository_tokens"`
}

// This private recipe reports native configuration and the selected registry
// shape only. The target partition total remains an observed plan/config input;
// this record supplies its independent native ceiling.
type t422RuntimeFacts struct {
	Schema                           string            `json:"schema"`
	StoreRunnerDefaultMaxAttempts    int               `json:"store_runner_default_max_attempts"`
	StoreRunnerConcurrencyPerKind    int               `json:"store_runner_concurrency_per_kind"`
	ObservationIOConcurrency         int               `json:"observation_io_concurrency"`
	ObservationCPUConcurrency        int               `json:"observation_cpu_concurrency"`
	RelationshipConcurrency          int               `json:"relationship_concurrency"`
	ExtractionConcurrency            int               `json:"extraction_concurrency"`
	ObservationPlanning              t422ScheduleFacts `json:"observation_planning"`
	ObservationInventory             t422ScheduleFacts `json:"observation_inventory"`
	ObservationExecution             t422ScheduleFacts `json:"observation_execution"`
	Relationship                     t422ScheduleFacts `json:"relationship"`
	Extraction                       t422ScheduleFacts `json:"extraction"`
	NativeMaximumAggregatePartitions int               `json:"native_maximum_aggregate_partitions"`
	StoreGenerationMaxAttempts       int               `json:"store_generation_max_attempts"`
	SelectedJobAcceptedAttempts      int               `json:"selected_job_accepted_attempts"`
	SelectedChunkAcceptedAttempts    int               `json:"selected_chunk_accepted_attempts"`
	MaximumStoreRowsPerTransaction   int               `json:"maximum_store_rows_per_transaction"`
	MaximumLifecycleDeletesPerTurn   int               `json:"maximum_lifecycle_deletes_per_turn"`
	RegisteredExtractionDomains      []string          `json:"registered_extraction_domains"`
}

func configuredT422RuntimeFacts() t422RuntimeFacts {
	return t422RuntimeFacts{
		Schema:                           t422RuntimeFactsSchema,
		StoreRunnerDefaultMaxAttempts:    store.DefaultRunnerMaxAttempts,
		StoreRunnerConcurrencyPerKind:    store.RunnerConcurrencyPerKind,
		ObservationIOConcurrency:         observationIOConcurrency,
		ObservationCPUConcurrency:        observationCPUConcurrency,
		RelationshipConcurrency:          relationshipConcurrency,
		ExtractionConcurrency:            extractionpublication.ScheduleClassConcurrency,
		ObservationPlanning:              t422ScheduleFacts{MaxAttempts: observationpublication.PlanningScheduleMaxAttempts, RepositoryTokens: observationpublication.PlanningScheduleRepositoryTokens},
		ObservationInventory:             t422ScheduleFacts{MaxAttempts: observationpublication.InventoryScheduleMaxAttemptsV2, RepositoryTokens: observationpublication.InventoryScheduleRepositoryTokensV2},
		ObservationExecution:             t422ScheduleFacts{MaxAttempts: observationpublication.ScheduleMaxAttempts, RepositoryTokens: observationpublication.ScheduleRepositoryTokens},
		Relationship:                     t422ScheduleFacts{MaxAttempts: relationshippublication.ScheduleMaxAttempts, RepositoryTokens: relationshippublication.ScheduleRepositoryTokens},
		Extraction:                       t422ScheduleFacts{MaxAttempts: extractionpublication.ScheduleMaxAttempts, RepositoryTokens: extractionpublication.ScheduleRepositoryTokens},
		NativeMaximumAggregatePartitions: candidate.MaxSparseAggregatePartitions,
		StoreGenerationMaxAttempts:       store.MaxGenerationAttempts,
		SelectedJobAcceptedAttempts:      t422JobAcceptedAttempts,
		SelectedChunkAcceptedAttempts:    t422ChunkAcceptedAttempts,
		MaximumStoreRowsPerTransaction:   storeaccounting.MaximumRows,
		MaximumLifecycleDeletesPerTurn:   lifecycle.SelectedCleanupObservationDeletes,
		RegisteredExtractionDomains:      t422RegisteredExtractionDomains(),
	}
}

func t422RegisteredExtractionDomains() []string {
	extractors := evidenceExtractors(true, true, false, true)
	domains := make([]string, 0, len(extractors))
	for _, extractor := range extractors {
		domains = append(domains, extractor.Domain())
	}
	sort.Strings(domains)
	return domains
}

// runStoreRunner makes the production per-kind topology consume the same
// compiled constant reported by the private facts command.
func runStoreRunner(ctx context.Context, runBackground func(func()), runner *store.Runner) {
	for range store.RunnerConcurrencyPerKind {
		runBackground(func() { runner.Run(ctx) })
	}
}

// runPhebs reaches this only after its unchanged bootstrap and command checks.
// A selected server/archive lifetime is never a no-work probe. The future
// parent must independently protect, launch, join and bind this binary; printing
// these bytes neither admits the tool nor issues a profile.
func writeT422RuntimeFacts(ctx context.Context, args []string, output io.Writer, lifetime *dispatchadmission.ProductionLifetime) error {
	if ctx == nil || ctx.Err() != nil || len(args) != 0 || output == nil || lifetime != nil {
		return errors.New("runtime facts require an unbound no-argument command")
	}
	raw, err := json.Marshal(configuredT422RuntimeFacts())
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := output.Write(raw)
	if err != nil {
		return err
	}
	if n != len(raw) {
		return io.ErrShortWrite
	}
	return ctx.Err()
}
