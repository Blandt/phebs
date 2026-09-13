package t421

import (
	"bytes"
	"fmt"
	"slices"
)

type ExecutionReuseDecision string

const (
	ExecutionReuseCurrent     ExecutionReuseDecision = "current"
	ExecutionReuseReactivated ExecutionReuseDecision = "prior_reactivation"
)

type ExecutionReusePhase struct {
	Source, Search, Observation, Catalog, Relationship ExecutionReuseDecision
	Complete                                           bool
}

// A complete phase has one successful final-authority report terminal. Empty
// lane values are then observed zero; without that terminal they are unknown.
// This is retained joined-work evidence only. It is not projected into signed
// phase metrics until the ceremony author gains an exact joined-work consumer.
type ExecutionReuseObservation struct {
	Phases          [15]ExecutionReusePhase
	Bound, Complete bool
}

func observeReuseEvent(line []byte, plan Plan, producer uint32, input string, out *ExecutionReuseObservation) (bool, error) {
	if bytes.HasPrefix(line, []byte("RUB")) {
		want := fmt.Sprintf("RUB1:%d:%s\n", producer, input)
		if out.Bound || string(line) != want {
			return true, errExecutionAttempts
		}
		out.Bound = true
		return true, nil
	}
	if !bytes.HasPrefix(line, []byte("RU")) {
		return false, nil
	}
	if !out.Bound || len(line) != 14 || !bytes.Equal(line[:4], []byte("RU1:")) ||
		line[4] != executionWorkProducerByte(producer) || line[5] != ':' || line[7] != ':' || line[13] != '\n' {
		return true, errExecutionAttempts
	}
	phase := bytes.IndexByte([]byte("0123456789ABCDEF"), line[6])
	if phase < 1 || !slices.Contains(executionProducerPhases(producer), uint32(phase)) ||
		plan.PhaseOrder[phase-1] != plan.WorkEnvelope.Phases[phase-1].Phase {
		return true, errExecutionAttempts
	}
	value := &out.Phases[phase-1]
	if value.Complete || line[8] != line[9] {
		return true, errExecutionAttempts
	}
	decisions := [5]*ExecutionReuseDecision{
		&value.Source, &value.Search, &value.Observation, &value.Catalog, &value.Relationship,
	}
	for lane, code := range line[8:13] {
		switch code {
		case '0':
		case 'c':
			*decisions[lane] = ExecutionReuseCurrent
		case 'p':
			if lane > 1 {
				return true, errExecutionAttempts
			}
			*decisions[lane] = ExecutionReuseReactivated
		default:
			return true, errExecutionAttempts
		}
	}
	value.Complete = true
	return true, nil
}

func reservedReuseEvent(line []byte) bool {
	return bytes.Contains(line, []byte("RUB")) || bytes.Contains(line, []byte("RU1:"))
}
