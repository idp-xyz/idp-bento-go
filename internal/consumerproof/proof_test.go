package consumerproof

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePair(t *testing.T) {
	t.Parallel()
	expected := validExpectation()
	if err := ValidatePair(validProof(ConsumerTMS, expected), validProof(ConsumerParcel, expected), expected); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePairRejectsInvalidEvidence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*Proof, *Proof, *Expectation)
		wantErr string
	}{
		{name: "stable version", mutate: func(_ *Proof, _ *Proof, expected *Expectation) { expected.CandidateVersion = "v0.1.0" }, wantErr: "prerelease SemVer"},
		{name: "invalid expected checksum", mutate: func(_ *Proof, _ *Proof, expected *Expectation) { expected.ModuleChecksum = "sha256:not-a-go-sum" }, wantErr: "h1 SHA-256"},
		{name: "invalid expected TMS commit", mutate: func(_ *Proof, _ *Proof, expected *Expectation) { expected.TMSConsumerCommit = "deadbeef" }, wantErr: "expected TMS consumer commit"},
		{name: "invalid expected Parcel commit", mutate: func(_ *Proof, _ *Proof, expected *Expectation) { expected.ParcelConsumerCommit = "deadbeef" }, wantErr: "expected Parcel consumer commit"},
		{name: "wrong consumer", mutate: func(tms *Proof, _ *Proof, _ *Expectation) { tms.Consumer = ConsumerParcel }, wantErr: "TMS proof"},
		{name: "short commit", mutate: func(tms *Proof, _ *Proof, _ *Expectation) { tms.ConsumerCommit = "deadbeef" }, wantErr: "full lowercase"},
		{name: "TMS commit mismatch", mutate: func(tms *Proof, _ *Proof, _ *Expectation) { tms.ConsumerCommit = strings.Repeat("c", 40) }, wantErr: "consumer_commit"},
		{name: "Parcel commit mismatch", mutate: func(_ *Proof, parcel *Proof, _ *Expectation) { parcel.ConsumerCommit = strings.Repeat("c", 40) }, wantErr: "consumer_commit"},
		{name: "candidate mismatch", mutate: func(_ *Proof, parcel *Proof, _ *Expectation) { parcel.CandidateVersion = "v0.1.0-rc.2" }, wantErr: "candidate_version"},
		{name: "module path mismatch", mutate: func(_ *Proof, parcel *Proof, _ *Expectation) { parcel.ModulePath = "example.invalid/module" }, wantErr: "module_path"},
		{name: "checksum mismatch", mutate: func(_ *Proof, parcel *Proof, _ *Expectation) { parcel.ModuleChecksum = checksum(1) }, wantErr: "module_checksum"},
		{name: "suite mismatch", mutate: func(_ *Proof, parcel *Proof, _ *Expectation) { parcel.ContractSuiteVersion = "r05-v2" }, wantErr: "contract_suite_version"},
		{name: "failed contract", mutate: func(_ *Proof, parcel *Proof, _ *Expectation) { parcel.Result = ResultFail }, wantErr: "result is FAIL"},
		{name: "unknown result", mutate: func(_ *Proof, parcel *Proof, _ *Expectation) { parcel.Result = "SKIPPED" }, wantErr: "unsupported result"},
		{name: "invalid completion time", mutate: func(_ *Proof, parcel *Proof, _ *Expectation) { parcel.CompletedAt = "yesterday" }, wantErr: "RFC3339Nano"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			expected := validExpectation()
			tms := validProof(ConsumerTMS, expected)
			parcel := validProof(ConsumerParcel, expected)
			test.mutate(&tms, &parcel, &expected)
			err := ValidatePair(tms, parcel, expected)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestReadRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for name, content := range map[string]string{
		"unknown.json":  `{"format_version":1,"business_payload":{"secret":true}}`,
		"trailing.json": `{}` + "\n{}",
	} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(path); err == nil {
			t.Fatalf("Read(%s) accepted invalid evidence", name)
		}
	}
}

func TestReadAcceptsCanonicalProof(t *testing.T) {
	t.Parallel()
	expected := validExpectation()
	want := validProof(ConsumerTMS, expected)
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tms.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("proof = %#v, want %#v", got, want)
	}
}

func validExpectation() Expectation {
	return Expectation{
		CandidateVersion:     "v0.1.0-rc.1",
		ModuleChecksum:       checksum(0),
		ContractSuiteVersion: "r05-v1",
		TMSConsumerCommit:    strings.Repeat("a", 40),
		ParcelConsumerCommit: strings.Repeat("b", 40),
	}
}

func validProof(consumer string, expected Expectation) Proof {
	commitDigit := "a"
	if consumer == ConsumerParcel {
		commitDigit = "b"
	}
	return Proof{
		FormatVersion:        FormatVersion,
		Consumer:             consumer,
		ConsumerCommit:       strings.Repeat(commitDigit, 40),
		CandidateVersion:     expected.CandidateVersion,
		ModulePath:           ModulePath,
		ModuleChecksum:       expected.ModuleChecksum,
		ContractSuiteVersion: expected.ContractSuiteVersion,
		Result:               ResultPass,
		CompletedAt:          "2026-07-30T08:00:00.123456789Z",
	}
}

func checksum(fill byte) string {
	return "h1:" + base64.StdEncoding.EncodeToString(bytesOf(fill, 32))
}

func bytesOf(value byte, size int) []byte {
	result := make([]byte, size)
	for index := range result {
		result[index] = value
	}
	return result
}
