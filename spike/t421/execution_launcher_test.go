package t421

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func testExecutionSelection(t *testing.T) (executionSelectionV1, string) {
	t.Helper()
	value := executionSelectionV1{
		Schema: executionSelectionSchema, CeremonyID: "t422-test",
		RepositoryRoot: "/private/phebs", PlanSourceCommit: strings.Repeat("a", 40),
		IntegratedMainCommit: strings.Repeat("b", 40), SourceCommit: strings.Repeat("c", 40),
		GoRoot: "/opt/phebs-go", ModuleCache: "/Users/test/go/pkg/mod",
		GitBinary: "/usr/bin/git", SurrealBinary: "/opt/phebs-surreal/bin/surreal",
		SignerControlRoot: "/private/phebs-signer",
	}
	return value, encodeExecutionSelection(t, value)
}

func encodeExecutionSelection(t *testing.T, value executionSelectionV1) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestExecutionSelectionUsesOneCanonicalClosedRecord(t *testing.T) {
	want, encoded := testExecutionSelection(t)
	got, err := executionSelection(encoded)
	if err != nil || got != want {
		t.Fatalf("canonical selection = %#v, %v", got, err)
	}
	for _, invalid := range []string{"", encoded + "=", strings.Repeat("a", maxExecutionSelectionCharacters+1)} {
		if _, err := executionSelection(invalid); err != ErrExecutionLauncher {
			t.Fatalf("invalid selection admitted: %q, %v", invalid, err)
		}
	}
}

func TestExecutionSelectionRefusesNoncanonicalOrAliasedInputs(t *testing.T) {
	value, _ := testExecutionSelection(t)
	mutations := []func(*executionSelectionV1){
		func(value *executionSelectionV1) { value.CeremonyID = ".." },
		func(value *executionSelectionV1) { value.SourceCommit = strings.Repeat("A", 40) },
		func(value *executionSelectionV1) { value.PlanSourceCommit = strings.Repeat("a", 64) },
		func(value *executionSelectionV1) { value.IntegratedMainCommit = strings.Repeat("b", 64) },
		func(value *executionSelectionV1) { value.SourceCommit = strings.Repeat("c", 64) },
		func(value *executionSelectionV1) { value.GitBinary = value.RepositoryRoot + "/git" },
		func(value *executionSelectionV1) { value.ModuleCache = value.GoRoot },
		func(value *executionSelectionV1) { value.SurrealBinary = value.GitBinary },
	}
	for index, mutate := range mutations {
		candidate := value
		mutate(&candidate)
		raw, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		encoded := base64.RawURLEncoding.EncodeToString(raw)
		if _, err := executionSelection(encoded); err != ErrExecutionLauncher {
			t.Fatalf("mutation %d admitted: %v", index, err)
		}
	}
}
