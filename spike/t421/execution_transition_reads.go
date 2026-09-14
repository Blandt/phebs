package t421

import "strings"

// Record the actual validated trailer before body interpretation. A failed
// decoder keeps its positive transition prefix just like the whole-phase
// ledger. Endpoint classification adds no request and uses the owning phase's
// already selected read class; source-frozen bounds never supply observations.
func (reader *executionEpochInspection) retainTransitionReads(path string, report epochInspectionReport) error {
	transition := path == "/api/t422/retention/current-prior" || path == "/api/t422/archive/transition" ||
		strings.HasPrefix(path, "/api/t422/logical-activation/") || strings.HasPrefix(path, "/api/t422/return-a-marker/") ||
		strings.HasPrefix(path, "/api/t422/stale-lease/") || strings.HasPrefix(path, "/api/t422/checkpoint/") ||
		strings.HasPrefix(path, "/api/t422/lifecycle/")
	if !transition {
		return nil
	}
	if reader.bounds.TransitionReadClass == "" || len(reader.evidence.rows) == 0 {
		return errEpochInspection
	}
	row := &reader.evidence.rows[len(reader.evidence.rows)-1]
	value := TransitionReadSubtotal{Schema: "t422-transition-read-accounting-v1", Class: reader.bounds.TransitionReadClass}
	if row.TransitionReads != nil {
		value = *row.TransitionReads
		if value.Class != reader.bounds.TransitionReadClass {
			return errEpochInspection
		}
	}
	for _, item := range []struct {
		out       *uint64
		increment uint64
	}{
		{&value.ReportCalls, 1}, {&value.ControlFileReads, report.ControlFileReads},
		{&value.StoreReadAttempts, report.StoreReadAttempts}, {&value.MemberReads, report.MemberVisits},
		{&value.StoreWriteAttempts, report.StoreWriteAttempts},
	} {
		var err error
		*item.out, err = checkedInspectionReadSum(*item.out, item.increment)
		if err != nil {
			return errEpochInspection
		}
	}
	row.TransitionReads = &value
	return nil
}
