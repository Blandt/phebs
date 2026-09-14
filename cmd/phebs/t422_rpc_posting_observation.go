package main

import "errors"

// These counters are observed while F already validates every immutable RPC
// component posting. They neither derive unresolved from abstentions nor add
// another posting scan. The enclosing exact F binds their root identity.
type t422RPCPostingObservation struct {
	Resolved   uint64 `json:"resolved"`
	NameMatch  uint64 `json:"name_match"`
	Unresolved uint64 `json:"unresolved"`
}

func (counts *t422RPCPostingObservation) observe(class string) error {
	switch class {
	case "resolved":
		counts.Resolved++
	case "name_match":
		counts.NameMatch++
	case "unresolved":
		counts.Unresolved++
	default:
		return errors.New("unknown RPC posting observation class")
	}
	return nil
}
