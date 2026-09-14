package main

import "testing"

func TestT422RPCPostingObservation(t *testing.T) {
	counts := t422RPCPostingObservation{}
	for _, class := range []string{"resolved", "name_match", "unresolved", "unresolved"} {
		if err := counts.observe(class); err != nil {
			t.Fatal(err)
		}
	}
	want := t422RPCPostingObservation{Resolved: 1, NameMatch: 1, Unresolved: 2}
	if counts != want {
		t.Fatal(counts)
	}
	if err := counts.observe("unknown"); err == nil || counts != want {
		t.Fatal("invalid posting changed observation")
	}
}
