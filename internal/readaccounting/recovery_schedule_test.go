package readaccounting

import "testing"

func TestRecoveryScheduleObservationRetainsLastNativeRead(t *testing.T) {
	ctx, scope := CaptureRecoverySchedule(t.Context())
	if _, ok := scope.Observation(); ok {
		t.Fatal("unread schedule was observed")
	}
	ObserveRecoverySchedule(ctx, "actual-digest", 9, 2)
	ObserveRecoverySchedule(ctx, "actual-digest", 9, 9)
	got, ok := scope.Observation()
	if !ok || got.ScheduleSHA256 != "actual-digest" || got.Chunks != 9 || got.Successes != 9 {
		t.Fatal("native schedule snapshot changed", got)
	}
	ObserveRecoverySchedule(ctx, "actual-digest", 9, 10)
	if _, ok := scope.Observation(); ok {
		t.Fatal("invalid later observation accepted")
	}
	ObserveRecoverySchedule(t.Context(), "ordinary", 1, 1)
}
