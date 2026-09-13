package t421

import (
	"strings"
	"testing"
)

func TestExecutionUnsupportedSourceObservation(t *testing.T) {
	base := accountingTestPlan(t)
	binding := "UFB1:2:sha256:01" + strings.Repeat("00", 31) + "\n"
	other := strings.Replace(lifecycleTestBindings(2), binding, "", 1)
	other = strings.ReplaceAll(other, "UF1:2:2:00000000\n", "")
	other = strings.ReplaceAll(other, "UF1:2:4:00000000\n", "")
	phase4 := "UF1:2:4:00000000\n"
	for _, test := range []struct {
		name, raw string
		reports   uint64
		valid     bool
	}{
		{name: "zero", raw: binding + "UF1:2:2:00000000\n" + phase4, reports: 1, valid: true},
		{name: "repeat", raw: binding + "UF1:2:2:00000000\nUF1:2:2:00000000\n" + phase4, reports: 2, valid: true},
		{name: "unsupported", raw: binding + "UF1:2:2:00000001\n" + phase4},
		{name: "missing_phase", raw: binding + "UF1:2:2:00000000\n", reports: 1},
		{name: "wrong_phase", raw: binding + "UF1:2:5:00000000\n" + phase4},
		{name: "malformed", raw: binding + "UF1:2:2:0000000\n" + phase4},
		{name: "unbound", raw: "UF1:2:2:00000000\n" + phase4},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := observeExecutionAttempts([]byte(other+test.raw), base, 2, [32]byte{1}, true)
			if (err == nil) != test.valid || got.UnsupportedSource.Reports[1] != test.reports {
				t.Fatal(got, err)
			}
		})
	}
}

func TestExecutionUnsupportedSourceObservationBoundedRepeats(t *testing.T) {
	plan := accountingTestPlan(t)
	raw := lifecycleTestBindings(2)
	raw = strings.Replace(raw, "UF1:2:2:00000000\n", strings.Repeat("UF1:2:2:00000000\n", 5), 1)
	if got, err := observeExecutionAttempts([]byte(raw), plan, 2, [32]byte{1}, true); err != nil || got.UnsupportedSource.Reports[1] != 5 {
		t.Fatal(got, err)
	}
	raw = strings.Replace(raw, strings.Repeat("UF1:2:2:00000000\n", 5), strings.Repeat("UF1:2:2:00000000\n", 6), 1)
	if got, err := observeExecutionAttempts([]byte(raw), plan, 2, [32]byte{1}, true); err == nil || got.Complete {
		t.Fatal(got, err)
	}
}

func TestExecutionUnsupportedSourceObservationRejectsOfflineFamily(t *testing.T) {
	plan := accountingTestPlan(t)
	raw := archiveWorkTestBindings(10) + "UFB1:10:sha256:01" + strings.Repeat("00", 31) + "\n"
	if got, err := observeExecutionAttempts([]byte(raw), plan, 10, [32]byte{1}, true); err == nil || got.Complete {
		t.Fatal(got, err)
	}
}

func TestExecutionUnsupportedSourceObservationRequiresFrozenZero(t *testing.T) {
	plan := accountingTestPlan(t)
	plan.WorkEnvelope.Phases[1].UnsupportedSourceFiles.Maximum = 1
	got, err := observeExecutionAttempts([]byte(lifecycleTestBindings(2)), plan, 2, [32]byte{1}, true)
	if err == nil || got.UnsupportedSource.Bound || got.Complete {
		t.Fatal(got, err)
	}
}
