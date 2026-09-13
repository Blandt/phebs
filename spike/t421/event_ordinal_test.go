package t421

import (
	"math"
	"sort"
	"sync"
	"testing"
)

func TestExecutionEventOrdinalsRequireFinalAdmission(t *testing.T) {
	ordinals := newExecutionEventOrdinals()
	if ordinals.last != 0 || ordinals.failed {
		t.Fatal("new ordinal stream did not start at zero")
	}
	if value, err := (*admittedExecutionEventOrdinals)(nil).next(); err == nil || value != 0 {
		t.Fatal("nil admission handle allocated an event", value, err)
	}
	if value, err := (&admittedExecutionEventOrdinals{}).next(); err == nil || value != 0 {
		t.Fatal("reconstructed admission handle allocated an event", value, err)
	}
	admitted, value, err := ordinals.consumeFinalAdmission()
	if err != nil || admitted == nil || value != 1 || ordinals.last != 1 || ordinals.failed {
		t.Fatal("final admission did not consume ordinal one", value, err)
	}
	if next, err := admitted.next(); err != nil || next != 2 {
		t.Fatal("first operational event did not follow admission", next, err)
	}
}

func TestExecutionEventOrdinalsRejectSecondAdmissionPermanently(t *testing.T) {
	ordinals := newExecutionEventOrdinals()
	admitted, value, err := ordinals.consumeFinalAdmission()
	if err != nil || value != 1 {
		t.Fatal(value, err)
	}
	if second, value, err := ordinals.consumeFinalAdmission(); err == nil || second != nil || value != 0 {
		t.Fatal("second admission replaced the stream", second, value, err)
	}
	if value, err := admitted.next(); err == nil || value != 0 {
		t.Fatal("failed stream resumed", value, err)
	}
}

func TestExecutionEventOrdinalsAreUniqueUnderConcurrency(t *testing.T) {
	ordinals := newExecutionEventOrdinals()
	admitted, value, err := ordinals.consumeFinalAdmission()
	if err != nil || value != 1 {
		t.Fatal(value, err)
	}
	const calls = 256
	values := make(chan uint64, calls)
	errors := make(chan error, calls)
	var group sync.WaitGroup
	for range calls {
		group.Add(1)
		go func() {
			defer group.Done()
			value, err := admitted.next()
			values <- value
			errors <- err
		}()
	}
	group.Wait()
	close(values)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	got := make([]uint64, 0, calls)
	for value := range values {
		got = append(got, value)
	}
	sort.Slice(got, func(left, right int) bool { return got[left] < got[right] })
	for index, value := range got {
		if value != uint64(index)+2 {
			t.Fatal("duplicate or missing concurrent ordinal", index, value)
		}
	}
}

func TestExecutionEventOrdinalOverflowPermanentlyRefuses(t *testing.T) {
	ordinals := newExecutionEventOrdinals()
	admitted, value, err := ordinals.consumeFinalAdmission()
	if err != nil || value != 1 {
		t.Fatal(value, err)
	}
	// This test-only state stands for all preceding representable allocations;
	// production has no setter, decoder, reset, or caller-selected start.
	ordinals.mu.Lock()
	ordinals.last = math.MaxUint64 - 1
	ordinals.mu.Unlock()
	if value, err := admitted.next(); err != nil || value != math.MaxUint64 {
		t.Fatal("last representable ordinal refused", value, err)
	}
	for range 2 {
		if value, err := admitted.next(); err == nil || value != 0 {
			t.Fatal("overflowed stream resumed", value, err)
		}
	}
	if !ordinals.failed || ordinals.last != math.MaxUint64 {
		t.Fatal("overflow did not latch", ordinals.last, ordinals.failed)
	}
}
