package t421

import (
	"bytes"
	"fmt"
	"math"
	"slices"
)

type ExecutionUnsupportedSourceObservation struct {
	Reports         [15]uint64
	Bound, Complete bool
}

func (measurement *ExecutionUnsupportedSourceObservation) complete(plan Plan, producer uint32) bool {
	if producer >= 10 {
		return !measurement.Bound
	}
	if !measurement.Bound {
		return false
	}
	for _, phase := range executionProducerPhases(producer) {
		index := phase - 1
		row := plan.WorkEnvelope.Phases[index]
		if row.ObservationParses.Maximum == 0 {
			continue
		}
		if measurement.Reports[index] == 0 {
			return false
		}
	}
	return true
}

func reservedUnsupportedSourceEvent(line []byte) bool {
	return bytes.Contains(line, []byte("UFB")) || reservedBlobEvent(line, "UF")
}

func exactZeroUnsupportedSourcePlan(plan Plan, producer uint32) bool {
	for _, phase := range executionProducerPhases(producer) {
		row := plan.WorkEnvelope.Phases[phase-1]
		if row.UnsupportedSourceFiles.Minimum != 0 || row.UnsupportedSourceFiles.Maximum != 0 {
			return false
		}
	}
	return true
}

func parseUnsupportedSourceUint32(raw []byte) (uint64, bool) {
	if len(raw) != 8 {
		return 0, false
	}
	var value uint64
	for _, digit := range raw {
		index := bytes.IndexByte([]byte("0123456789abcdef"), digit)
		if index < 0 {
			return 0, false
		}
		value = value<<4 | uint64(index)
	}
	return value, true
}

func observeUnsupportedSourceEvent(line []byte, plan Plan, producer uint32, input string, out *ExecutionAttemptObservation) (bool, error) {
	if !reservedUnsupportedSourceEvent(line) {
		return false, nil
	}
	measurement := &out.UnsupportedSource
	if bytes.Contains(line, []byte("UFB")) {
		if producer >= 10 || measurement.Bound || !exactZeroUnsupportedSourcePlan(plan, producer) ||
			string(line) != fmt.Sprintf("UFB1:%d:%s\n", producer, input) {
			return true, errExecutionAttempts
		}
		measurement.Bound = true
		return true, nil
	}
	if producer >= 10 || !measurement.Bound || len(line) != 17 || line[len(line)-1] != '\n' {
		return true, errExecutionAttempts
	}
	fields := bytes.Split(line[:len(line)-1], []byte(":"))
	if len(fields) != 4 || !bytes.Equal(fields[0], []byte("UF1")) || len(fields[1]) != 1 ||
		fields[1][0] != executionWorkProducerByte(producer) || len(fields[2]) != 1 {
		return true, errExecutionAttempts
	}
	phase := bytes.IndexByte([]byte("0123456789ABCDEF"), fields[2][0])
	if phase < 1 || !slices.Contains(executionProducerPhases(producer), uint32(phase)) {
		return true, errExecutionAttempts
	}
	unsupported, ok := parseUnsupportedSourceUint32(fields[3])
	if !ok || unsupported != 0 {
		return true, errExecutionAttempts
	}
	index := phase - 1
	if plan.WorkEnvelope.MaximumRetriesPerUnit == 0 || measurement.Reports[index] == math.MaxUint64 ||
		measurement.Reports[index] >= plan.WorkEnvelope.MaximumRetriesPerUnit {
		return true, errExecutionAttempts
	}
	measurement.Reports[index]++
	out.Phases[index].UnsupportedSourceFiles = unsupported
	return true, nil
}
