package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.idp.xyz/idp-bento-go/internal/consumerproof"
)

func TestRunValidatesTwoConsumerProofs(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	checksum := proofChecksum(0)
	tmsPath := writeProof(t, directory, "tms.json", commandProof(consumerproof.ConsumerTMS, "a", checksum))
	parcelPath := writeProof(t, directory, "parcel.json", commandProof(consumerproof.ConsumerParcel, "b", checksum))
	var output bytes.Buffer
	err := run([]string{
		"-candidate", "v0.1.0-rc.1",
		"-checksum", checksum,
		"-contract-suite", "r05-v1",
		"-tms-commit", strings.Repeat("a", 40),
		"-parcel-commit", strings.Repeat("b", 40),
		"-tms", tmsPath,
		"-parcel", parcelPath,
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "TMS "+strings.Repeat("a", 40)) || !strings.Contains(output.String(), "Parcel "+strings.Repeat("b", 40)) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunFailsClosed(t *testing.T) {
	t.Parallel()
	checksum := proofChecksum(0)
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing flags", want: "are required"},
		{name: "missing proof", args: []string{"-candidate", "v0.1.0-rc.1", "-checksum", checksum, "-contract-suite", "r05-v1", "-tms-commit", strings.Repeat("a", 40), "-parcel-commit", strings.Repeat("b", 40), "-tms", "missing", "-parcel", "missing"}, want: "read TMS proof"},
		{name: "unexpected consumer commit", args: proofcheckArgs(t, checksum, strings.Repeat("c", 40), strings.Repeat("b", 40)), want: "consumer_commit"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := run(test.args, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func proofcheckArgs(t *testing.T, checksum, tmsCommit, parcelCommit string) []string {
	t.Helper()
	directory := t.TempDir()
	return []string{
		"-candidate", "v0.1.0-rc.1",
		"-checksum", checksum,
		"-contract-suite", "r05-v1",
		"-tms-commit", tmsCommit,
		"-parcel-commit", parcelCommit,
		"-tms", writeProof(t, directory, "tms.json", commandProof(consumerproof.ConsumerTMS, "a", checksum)),
		"-parcel", writeProof(t, directory, "parcel.json", commandProof(consumerproof.ConsumerParcel, "b", checksum)),
	}
}

func commandProof(consumer, commitDigit, checksum string) consumerproof.Proof {
	return consumerproof.Proof{
		FormatVersion:        consumerproof.FormatVersion,
		Consumer:             consumer,
		ConsumerCommit:       strings.Repeat(commitDigit, 40),
		CandidateVersion:     "v0.1.0-rc.1",
		ModulePath:           consumerproof.ModulePath,
		ModuleChecksum:       checksum,
		ContractSuiteVersion: "r05-v1",
		Result:               consumerproof.ResultPass,
		CompletedAt:          "2026-07-30T08:00:00Z",
	}
}

func writeProof(t *testing.T, directory, name string, proof consumerproof.Proof) string {
	t.Helper()
	data, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func proofChecksum(fill byte) string {
	return "h1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))
}
