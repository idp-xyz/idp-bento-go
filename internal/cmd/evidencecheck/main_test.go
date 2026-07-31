package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.idp.xyz/idp-bento-go/internal/consumerproof"
	"go.idp.xyz/idp-bento-go/internal/releaseevidence"
)

func TestRunValidatesEvidenceBundle(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	checksum := commandChecksum()
	frameworkCommit := strings.Repeat("c", 40)
	tmsCommit := strings.Repeat("a", 40)
	parcelCommit := strings.Repeat("b", 40)
	tmsProof := commandEvidenceProof(consumerproof.ConsumerTMS, tmsCommit, checksum, "2026-07-30T08:09:00Z")
	parcelProof := commandEvidenceProof(consumerproof.ConsumerParcel, parcelCommit, checksum, "2026-07-30T08:10:00Z")
	tmsBytes := commandJSON(t, tmsProof)
	parcelBytes := commandJSON(t, parcelProof)
	tmsPath := writeEvidenceFile(t, directory, "tms.json", tmsBytes)
	parcelPath := writeEvidenceFile(t, directory, "parcel.json", parcelBytes)
	manifest := commandManifest(frameworkCommit, checksum, tmsCommit, parcelCommit, tmsBytes, parcelBytes)
	manifestPath := writeEvidenceFile(t, directory, "manifest.json", commandJSON(t, manifest))
	var output bytes.Buffer
	err := run([]string{
		"-manifest", manifestPath,
		"-tms-proof", tmsPath,
		"-parcel-proof", parcelPath,
		"-request-id", strings.Repeat("1", 32),
		"-candidate", "v0.1.0-rc.1",
		"-framework-commit", frameworkCommit,
		"-checksum", checksum,
		"-contract-suite", "r05-v1",
		"-tms-commit", tmsCommit,
		"-parcel-commit", parcelCommit,
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "evidencecheck: request") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunRequiresAllInputs(t *testing.T) {
	t.Parallel()
	if err := run(nil, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "are required") {
		t.Fatalf("error = %v", err)
	}
}

func commandManifest(frameworkCommit, checksum, tmsCommit, parcelCommit string, tmsBytes, parcelBytes []byte) releaseevidence.Manifest {
	return releaseevidence.Manifest{
		FormatVersion:        releaseevidence.FormatVersion,
		RequestID:            strings.Repeat("1", 32),
		ContractSuiteVersion: "r05-v1",
		RequestedAt:          "2026-07-30T08:00:00Z",
		DeadlineAt:           "2026-07-30T08:40:00Z",
		CreatedAt:            "2026-07-30T08:12:00Z",
		Candidate: releaseevidence.Candidate{
			Version:         "v0.1.0-rc.1",
			FrameworkCommit: frameworkCommit,
			ModulePath:      releaseevidence.ModulePath,
			ModuleChecksum:  checksum,
			GitOrigin:       releaseevidence.GitOrigin,
			APIComparison: releaseevidence.APIComparison{
				Mode:            releaseevidence.BaselineInitialV01,
				CandidateSHA256: commandSHA("3"),
			},
		},
		Framework: releaseevidence.FrameworkRun{
			RepositoryID:               100,
			WorkflowID:                 101,
			WorkflowSHA:                strings.Repeat("d", 40),
			WorkflowRef:                "idp-xyz/idp-bento-go/.github/workflows/release-candidate.yml@refs/heads/main",
			RunID:                      102,
			RunAttempt:                 1,
			RunnerImage:                "github-hosted:ubuntu-24.04/20260727.1",
			GoVersion:                  "go1.26.5",
			PostgresImage:              "postgres:16.12-alpine@" + commandSHA("4"),
			Result:                     releaseevidence.ResultPass,
			CandidatecheckOutputSHA256: commandSHA("5"),
			CompletedAt:                "2026-07-30T07:59:00Z",

			GovernanceMode: "SOLO_BOOTSTRAP",
		},
		TMS:    commandConsumerRun(200, "idp-repo/idp-tms", tmsCommit, "6", 201, 202, 203, tmsBytes, "2026-07-30T08:01:00Z", "2026-07-30T08:10:00Z"),
		Parcel: commandConsumerRun(300, "idp-repo/idp-parcel", parcelCommit, "7", 301, 302, 303, parcelBytes, "2026-07-30T08:02:00Z", "2026-07-30T08:11:00Z"),
	}
}

func commandConsumerRun(repositoryID int64, repository, expectedCommit, workflowDigit string, workflowID, runID, artifactID int64, proofBytes []byte, startedAt, completedAt string) releaseevidence.ConsumerRun {
	return releaseevidence.ConsumerRun{
		RepositoryID:   repositoryID,
		Repository:     repository,
		ExpectedCommit: expectedCommit,
		WorkflowID:     workflowID,
		WorkflowSHA:    strings.Repeat(workflowDigit, 40),
		WorkflowRef:    repository + "/.github/workflows/bento-contract.yml@refs/heads/main",
		RunID:          runID,
		RunAttempt:     1,
		ActorID:        repositoryID + 10,
		Event:          releaseevidence.EventWorkflowDispatch,
		Conclusion:     releaseevidence.ConclusionSuccess,
		ArtifactID:     artifactID,
		ArtifactDigest: commandSHA("8"),
		ProofSHA256:    commandDigest(proofBytes),
		StartedAt:      startedAt,
		CompletedAt:    completedAt,
	}
}

func commandEvidenceProof(consumer, commit, checksum, completedAt string) consumerproof.Proof {
	return consumerproof.Proof{
		FormatVersion:        consumerproof.FormatVersion,
		Consumer:             consumer,
		ConsumerCommit:       commit,
		CandidateVersion:     "v0.1.0-rc.1",
		ModulePath:           consumerproof.ModulePath,
		ModuleChecksum:       checksum,
		ContractSuiteVersion: "r05-v1",
		Result:               consumerproof.ResultPass,
		CompletedAt:          completedAt,

		AcknowledgedGovernanceMode: "SOLO_BOOTSTRAP",
	}
}

func writeEvidenceFile(t *testing.T, directory, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func commandJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func commandChecksum() string {
	return "h1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0}, 32))
}

func commandSHA(digit string) string {
	return "sha256:" + strings.Repeat(digit, 64)
}

func commandDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
