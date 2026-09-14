package t421

import (
	"math"
	"slices"
)

// Join population facts from actual accepted F snapshots with successful SB2
// source-build terminals and actual authored change inventories. No plan oracle
// supplies an observed count. Physical passes here mean completed source-owner
// traversals whose native manifest was validated, written and directory-synced;
// failed B/E byte prefixes and retained-source reopens are not full passes.
func composeExecutionPopulationMetrics(plan Plan, work executionJoinedWork, inspection []ExecutionPhaseInspection, authors []ExecutionAuthorResult, out *executionReceiptMetrics) error {
	if out == nil || plan.Schema != PlanV3Schema || len(plan.PhaseOrder) != 15 || len(inspection) > 14 || len(authors) > 3 {
		return errExecutionReceiptMetrics
	}
	seen := [15]bool{}
	for _, row := range inspection {
		index := slices.Index(plan.PhaseOrder, row.Phase)
		if index < 0 {
			return errExecutionReceiptMetrics
		}
		if !row.SelectorAccepted || row.Final == nil {
			continue
		}
		full := row.Phase == "cold" || row.Phase == "physical_delta_b" || row.Phase == "return_a"
		logical := full || row.Phase == "logical_delta_b" || row.Phase == "archive_restore"
		if !logical {
			continue
		}
		if seen[index] || row.Final.Projection.Phase != row.Phase || !row.Final.Authority.Current {
			return errExecutionReceiptMetrics
		}
		seen[index] = true
		projection := row.Final.Projection
		if !validSetIdentity(projection.Catalog) || !validSetIdentity(projection.MembershipSet) {
			return errExecutionReceiptMetrics
		}
		metric := &out.Metrics[index]
		metric.ServiceRows = CountMetric(projection.Catalog.Records)
		metric.LogicalMemberships = CountMetric(projection.MembershipSet.Records)
		if !full {
			continue
		}
		if !validSetIdentity(projection.SearchInventory) || projection.SearchInventory != row.Final.Authority.SearchInventory || projection.ExtractionRoots == nil {
			return errExecutionReceiptMetrics
		}
		var passes, owners uint64
		sourceObserved := false
		for slot, record := range work.Records {
			if record.Producer == 0 {
				continue
			}
			if executionJoinedWorkSlot(record.Producer) != slot || record.Input == ([32]byte{}) {
				return errExecutionReceiptMetrics
			}
			if !slices.Contains(executionProducerPhases(record.Producer), uint32(index+1)) {
				continue
			}
			census := record.Attempts.SourceCensus
			if !record.Joined || !record.SessionEmpty || !census.Bound || !record.Attempts.Complete && !record.Attempts.ScanComplete || census.Started[index] != census.Finished[index] || census.Succeeded[index] > census.Finished[index] {
				return errExecutionReceiptMetrics
			}
			sourceObserved = true
			if !addExecutionJoinedMetric(&passes, census.Succeeded[index]) || !addExecutionJoinedMetric(&owners, census.RegularOwners[index]) {
				return errExecutionReceiptMetrics
			}
		}
		// The fixed execution profile's source/search inventory consists solely
		// of regular owners. Bind its final population to the real terminal counts.
		if !sourceObserved || passes == 0 || projection.SearchInventory.Records > math.MaxUint64/passes || owners != passes*projection.SearchInventory.Records {
			return errExecutionReceiptMetrics
		}
		metric.PhysicalCorpusPasses = CountMetric(passes)
		metric.CombinedPhysicalOwners = CountMetric(projection.SearchInventory.Records)
		var partitions uint64
		domains := make(map[string]bool, len(projection.ExtractionRoots))
		for _, root := range projection.ExtractionRoots {
			if root.Domain == "" || domains[root.Domain] || root.MemberPartitions > math.MaxUint64-root.TypedPartitions || root.ApplicablePartitions != root.MemberPartitions+root.TypedPartitions || root.Availability != "admitted" && root.Availability != "empty" || !addExecutionJoinedMetric(&partitions, root.ApplicablePartitions) {
				return errExecutionReceiptMetrics
			}
			domains[root.Domain] = true
		}
		metric.ApplicablePartitions = CountMetric(partitions)
		metric.PublishedDomains = CountMetric(len(domains))
		if row.Phase == "cold" {
			if row.Final.CatalogPopulation == nil || row.Final.CatalogPopulation.AcceptedServices > projection.Catalog.Records {
				return errExecutionReceiptMetrics
			}
			metric.ChangedLogicalServices = CountMetric(row.Final.CatalogPopulation.AcceptedServices)
		}
		var author *ExecutionAuthorResult
		for i := range authors {
			if authors[i].Revision == row.Final.Authority.PhysicalRevision {
				if author != nil {
					return errExecutionReceiptMetrics
				}
				author = &authors[i]
			}
		}
		expectedProducer := uint32(7)
		switch row.Phase {
		case "physical_delta_b":
			expectedProducer = 8
		case "return_a":
			expectedProducer = 9
		}
		if author == nil || author.ProducerID != expectedProducer || !author.Completed || !author.RootStarted || !author.RootJoined || !author.SessionEmpty || author.Response == nil || !validChangedPhysicalFiles(author.Response.ChangedPhysicalFiles, true) ||
			author.Response.Result.Name != author.Revision || author.Response.Result.Commit != row.Final.Authority.PhysicalCommit || author.Response.Result.Tree != row.Final.Authority.PhysicalTree {
			return errExecutionReceiptMetrics
		}
		metric.ChangedPhysicalFiles = CountMetric(author.Response.ChangedPhysicalFiles.Count)
	}
	return nil
}
