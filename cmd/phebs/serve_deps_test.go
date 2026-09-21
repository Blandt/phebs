package main

import (
	"slices"
	"testing"
)

func TestServeDepsRunDeferredContinuesAfterPanic(t *testing.T) {
	d := &serveDeps{}
	var order []int
	d.deferFunc(func(*error) { order = append(order, 1) })
	d.deferFunc(func(*error) { panic("cleanup failed") })
	d.deferFunc(func(*error) { order = append(order, 3) })

	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		d.runDeferred(new(error))
	}()

	if !panicked {
		t.Fatal("cleanup panic was not propagated")
	}
	if want := []int{3, 1}; !slices.Equal(order, want) {
		t.Fatalf("cleanup order = %v, want %v", order, want)
	}
}
