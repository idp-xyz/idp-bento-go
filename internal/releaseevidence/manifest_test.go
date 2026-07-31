package releaseevidence

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.idp.xyz/idp-bento-go/internal/consumerproof"
)

func TestValidateBundle(t *testing.T) {
	t.Parallel()
	manifest, tmsProof, parcelProof, tmsBytes, parcelBytes, expected := validBundle(t)
	if err := ValidateBundle(manifest, tmsProof, parcelProof, tmsBytes, parcelBytes, expected); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBundleFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*Manifest, *[]byte, *Expectation)
		wantErr string
	}{
		{name: "request ID", mutate: func(manifest *Manifest, _ *[]byte, _ *Expectation) { manifest.RequestID = "short" }, wantErr: "request_id"},
		{name: "candidate mismatch", mutate: func(manifest *Manifest, _ *[]byte, _ *Expectation) { manifest.Candidate.Version = "v0.1.0-rc.2" }, wantErr: "expected prerelease"},
		{name: "initial baseline fields", mutate: func(manifest *Manifest, _ *[]byte, _ *Expectation) {
			manifest.Candidate.APIComparison.BaselineVersion = "v0.0.1"
		}, wantErr: "initial-v0-1"},
		{name: "mutable postgres image", mutate: func(manifest *Manifest, _ *[]byte, _ *Expectation) { manifest.Framework.PostgresImage = "postgres:16" }, wantErr: "immutable sha256"},
		{name: "framework after request", mutate: func(manifest *Manifest, _ *[]byte, _ *Expectation) {
			manifest.Framework.CompletedAt = "2026-07-30T08:01:00Z"
		}, wantErr: "must not be after"},
		{name: "long coordination window", mutate: func(manifest *Manifest, _ *[]byte, _ *Expectation) { manifest.DeadlineAt = "2026-07-30T08:41:00Z" }, wantErr: "no more than"},
		{name: "wrong event", mutate: func(manifest *Manifest, _ *[]byte, _ *Expectation) { manifest.TMS.Event = "repository_dispatch" }, wantErr: "workflow_dispatch"},
		{name: "proof bytes changed", mutate: func(_ *Manifest, tmsBytes *[]byte, _ *Expectation) { *tmsBytes = append(*tmsBytes, '\n') }, wantErr: "proof_sha256"},
		{name: "duplicate consumer repository", mutate: func(manifest *Manifest, _ *[]byte, _ *Expectation) {
			manifest.Parcel.RepositoryID = manifest.TMS.RepositoryID
		}, wantErr: "different repositories"},
		{name: "created before consumers", mutate: func(manifest *Manifest, _ *[]byte, _ *Expectation) { manifest.CreatedAt = "2026-07-30T08:05:00Z" }, wantErr: "both consumer completions"},
		{name: "unexpected TMS baseline", mutate: func(_ *Manifest, _ *[]byte, expected *Expectation) {
			expected.TMSConsumerCommit = strings.Repeat("9", 40)
		}, wantErr: "consumer_commit"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manifest, tmsProof, parcelProof, tmsBytes, parcelBytes, expected := validBundle(t)
			test.mutate(&manifest, &tmsBytes, &expected)
			err := ValidateBundle(manifest, tmsProof, parcelProof, tmsBytes, parcelBytes, expected)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateBundleRequiresAgreedGovernanceMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*Manifest, *consumerproof.Proof, *consumerproof.Proof)
		wantErr string
	}{
		{
			name: "consumers disagree",
			mutate: func(_ *Manifest, _ *consumerproof.Proof, parcel *consumerproof.Proof) {
				parcel.AcknowledgedGovernanceMode = "DUAL_MAINTAINER"
			},
			wantErr: "both consumers must acknowledge the same mode",
		},
		{
			name: "consumers agree but contradict the framework",
			mutate: func(_ *Manifest, tms *consumerproof.Proof, parcel *consumerproof.Proof) {
				tms.AcknowledgedGovernanceMode = "DUAL_MAINTAINER"
				parcel.AcknowledgedGovernanceMode = "DUAL_MAINTAINER"
			},
			wantErr: "the framework declares",
		},
		{
			name: "framework declares a lowercase mode",
			mutate: func(manifest *Manifest, _ *consumerproof.Proof, _ *consumerproof.Proof) {
				manifest.Framework.GovernanceMode = "solo_bootstrap"
			},
			wantErr: "governance_mode",
		},
		{
			name: "framework omits the mode",
			mutate: func(manifest *Manifest, _ *consumerproof.Proof, _ *consumerproof.Proof) {
				manifest.Framework.GovernanceMode = ""
			},
			wantErr: "governance_mode",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manifest, tmsProof, parcelProof, tmsBytes, parcelBytes, expected := validBundle(t)
			test.mutate(&manifest, &tmsProof, &parcelProof)
			err := ValidateBundle(manifest, tmsProof, parcelProof, tmsBytes, parcelBytes, expected)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestReadRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"unknown.json":  `{"format_version":1,"business_payload":{"secret":true}}`,
		"trailing.json": `{}` + "\n{}",
	} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(path); err == nil {
			t.Fatalf("Read(%s) accepted invalid evidence", name)
		}
	}
}

func TestReadAcceptsCanonicalManifest(t *testing.T) {
	t.Parallel()
	manifest, _, _, _, _, _ := validBundle(t)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustJSON(t, got), data) {
		t.Fatalf("manifest changed during strict read")
	}
}

func validBundle(t *testing.T) (Manifest, consumerproof.Proof, consumerproof.Proof, []byte, []byte, Expectation) {
	t.Helper()
	expected := Expectation{
		RequestID:            strings.Repeat("1", 32),
		CandidateVersion:     "v0.1.0-rc.1",
		FrameworkCommit:      strings.Repeat("c", 40),
		ModuleChecksum:       evidenceChecksum(0),
		ContractSuiteVersion: "r05-v1",
		TMSConsumerCommit:    strings.Repeat("a", 40),
		ParcelConsumerCommit: strings.Repeat("b", 40),
	}
	tmsProof := evidenceProof(consumerproof.ConsumerTMS, expected.TMSConsumerCommit, expected, "2026-07-30T08:09:00Z")
	parcelProof := evidenceProof(consumerproof.ConsumerParcel, expected.ParcelConsumerCommit, expected, "2026-07-30T08:10:00Z")
	tmsBytes := mustJSON(t, tmsProof)
	parcelBytes := mustJSON(t, parcelProof)
	manifest := Manifest{
		FormatVersion:        FormatVersion,
		RequestID:            expected.RequestID,
		ContractSuiteVersion: expected.ContractSuiteVersion,
		RequestedAt:          "2026-07-30T08:00:00Z",
		DeadlineAt:           "2026-07-30T08:40:00Z",
		CreatedAt:            "2026-07-30T08:12:00Z",
		Candidate: Candidate{
			Version:         expected.CandidateVersion,
			FrameworkCommit: expected.FrameworkCommit,
			ModulePath:      ModulePath,
			ModuleChecksum:  expected.ModuleChecksum,
			GitOrigin:       GitOrigin,
			APIComparison: APIComparison{
				Mode:            BaselineInitialV01,
				CandidateSHA256: evidenceSHA("3"),
			},
		},
		Framework: FrameworkRun{
			RepositoryID:               100,
			WorkflowID:                 101,
			WorkflowSHA:                strings.Repeat("d", 40),
			WorkflowRef:                "idp-xyz/idp-bento-go/.github/workflows/release-candidate.yml@refs/heads/main",
			RunID:                      102,
			RunAttempt:                 1,
			RunnerImage:                "github-hosted:ubuntu-24.04/20260727.1",
			GoVersion:                  "go1.26.5",
			PostgresImage:              "postgres:16.12-alpine@" + evidenceSHA("4"),
			Result:                     ResultPass,
			CandidatecheckOutputSHA256: evidenceSHA("5"),
			CompletedAt:                "2026-07-30T07:59:00Z",

			GovernanceMode: "SOLO_BOOTSTRAP",
		},
		TMS:    evidenceConsumerRun(200, "idp-repo/idp-tms", expected.TMSConsumerCommit, "6", 201, 202, 203, tmsBytes, "2026-07-30T08:01:00Z", "2026-07-30T08:10:00Z"),
		Parcel: evidenceConsumerRun(300, "idp-repo/idp-parcel", expected.ParcelConsumerCommit, "7", 301, 302, 303, parcelBytes, "2026-07-30T08:02:00Z", "2026-07-30T08:11:00Z"),
	}
	return manifest, tmsProof, parcelProof, tmsBytes, parcelBytes, expected
}

func evidenceConsumerRun(repositoryID int64, repository, expectedCommit, workflowDigit string, workflowID, runID, artifactID int64, proofBytes []byte, startedAt, completedAt string) ConsumerRun {
	return ConsumerRun{
		RepositoryID:   repositoryID,
		Repository:     repository,
		ExpectedCommit: expectedCommit,
		WorkflowID:     workflowID,
		WorkflowSHA:    strings.Repeat(workflowDigit, 40),
		WorkflowRef:    repository + "/.github/workflows/bento-contract.yml@refs/heads/main",
		RunID:          runID,
		RunAttempt:     1,
		ActorID:        repositoryID + 10,
		Event:          EventWorkflowDispatch,
		Conclusion:     ConclusionSuccess,
		ArtifactID:     artifactID,
		ArtifactDigest: evidenceSHA("8"),
		ProofSHA256:    digest(proofBytes),
		StartedAt:      startedAt,
		CompletedAt:    completedAt,
	}
}

func evidenceProof(consumer, commit string, expected Expectation, completedAt string) consumerproof.Proof {
	return consumerproof.Proof{
		FormatVersion:        consumerproof.FormatVersion,
		Consumer:             consumer,
		ConsumerCommit:       commit,
		CandidateVersion:     expected.CandidateVersion,
		ModulePath:           consumerproof.ModulePath,
		ModuleChecksum:       expected.ModuleChecksum,
		ContractSuiteVersion: expected.ContractSuiteVersion,
		Result:               consumerproof.ResultPass,
		CompletedAt:          completedAt,

		AcknowledgedGovernanceMode: "SOLO_BOOTSTRAP",
	}
}

func evidenceChecksum(fill byte) string {
	return "h1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))
}

func evidenceSHA(digit string) string {
	return "sha256:" + strings.Repeat(digit, 64)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestImmutableBaselineMode(t *testing.T) {
	t.Parallel()
	manifest, tmsProof, parcelProof, tmsBytes, parcelBytes, expected := validBundle(t)
	manifest.Candidate.Version = "v0.1.0-rc.2"
	expected.CandidateVersion = manifest.Candidate.Version
	tmsProof.CandidateVersion = manifest.Candidate.Version
	parcelProof.CandidateVersion = manifest.Candidate.Version
	tmsBytes = mustJSON(t, tmsProof)
	parcelBytes = mustJSON(t, parcelProof)
	manifest.TMS.ProofSHA256 = digest(tmsBytes)
	manifest.Parcel.ProofSHA256 = digest(parcelBytes)
	manifest.Candidate.APIComparison = APIComparison{
		Mode:            BaselineImmutable,
		BaselineVersion: "v0.1.0-rc.1",
		BaselineSHA256:  evidenceSHA("2"),
		CandidateSHA256: evidenceSHA("3"),
	}
	if err := ValidateBundle(manifest, tmsProof, parcelProof, tmsBytes, parcelBytes, expected); err != nil {
		t.Fatal(err)
	}
}

func TestCoordinationDurationConstant(t *testing.T) {
	t.Parallel()
	if MaxCoordinationTime != 40*time.Minute {
		t.Fatalf("MaxCoordinationTime = %s", MaxCoordinationTime)
	}
}
