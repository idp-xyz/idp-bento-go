package releaseevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"go.idp.xyz/idp-bento-go/internal/consumerproof"

	"golang.org/x/mod/semver"
)

const (
	FormatVersion         = 1
	ModulePath            = consumerproof.ModulePath
	GitOrigin             = "https://github.com/idp-xyz/idp-bento-go"
	ResultPass            = "PASS"
	EventWorkflowDispatch = "workflow_dispatch"
	ConclusionSuccess     = "success"
	BaselineInitialV01    = "initial-v0-1"
	BaselineImmutable     = "immutable"
	MaxCoordinationTime   = 40 * time.Minute
)

var (
	requestIDPattern      = regexp.MustCompile(`^[0-9a-f]{32}$`)
	fullGitObjectID       = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	sha256DigestPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	repositoryPattern     = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	goVersionPattern      = regexp.MustCompile(`^go[0-9]+\.[0-9]+\.[0-9]+$`)
	containerImagePattern = regexp.MustCompile(`^[^\s]+@sha256:[0-9a-f]{64}$`)
)

// Manifest 是写入 WORM 前生成的最小发布证据清单。
type Manifest struct {
	FormatVersion        int          `json:"format_version"`
	RequestID            string       `json:"request_id"`
	Candidate            Candidate    `json:"candidate"`
	ContractSuiteVersion string       `json:"contract_suite_version"`
	RequestedAt          string       `json:"requested_at"`
	DeadlineAt           string       `json:"deadline_at"`
	Framework            FrameworkRun `json:"framework"`
	TMS                  ConsumerRun  `json:"tms"`
	Parcel               ConsumerRun  `json:"parcel"`
	CreatedAt            string       `json:"created_at"`
}

type Candidate struct {
	Version         string        `json:"version"`
	FrameworkCommit string        `json:"framework_commit"`
	ModulePath      string        `json:"module_path"`
	ModuleChecksum  string        `json:"module_checksum"`
	GitOrigin       string        `json:"git_origin"`
	APIComparison   APIComparison `json:"api_comparison"`
}

type APIComparison struct {
	Mode            string `json:"mode"`
	BaselineVersion string `json:"baseline_version"`
	BaselineSHA256  string `json:"baseline_sha256"`
	CandidateSHA256 string `json:"candidate_sha256"`
}

type FrameworkRun struct {
	RepositoryID               int64  `json:"repository_id"`
	WorkflowID                 int64  `json:"workflow_id"`
	WorkflowSHA                string `json:"workflow_sha"`
	WorkflowRef                string `json:"workflow_ref"`
	RunID                      int64  `json:"run_id"`
	RunAttempt                 int    `json:"run_attempt"`
	RunnerImage                string `json:"runner_image"`
	GoVersion                  string `json:"go_version"`
	PostgresImage              string `json:"postgres_image"`
	Result                     string `json:"result"`
	CandidatecheckOutputSHA256 string `json:"candidatecheck_output_sha256"`
	CompletedAt                string `json:"completed_at"`

	// GovernanceMode 是治理模式的权威声明。两份消费者证明记录的是对它的确认，
	// ValidateBundle 比对三者一致。
	GovernanceMode string `json:"governance_mode"`
}

type ConsumerRun struct {
	RepositoryID   int64  `json:"repository_id"`
	Repository     string `json:"repository"`
	ExpectedCommit string `json:"expected_commit"`
	WorkflowID     int64  `json:"workflow_id"`
	WorkflowSHA    string `json:"workflow_sha"`
	WorkflowRef    string `json:"workflow_ref"`
	RunID          int64  `json:"run_id"`
	RunAttempt     int    `json:"run_attempt"`
	ActorID        int64  `json:"actor_id"`
	Event          string `json:"event"`
	Conclusion     string `json:"conclusion"`
	ArtifactID     int64  `json:"artifact_id"`
	ArtifactDigest string `json:"artifact_digest"`
	ProofSHA256    string `json:"proof_sha256"`
	StartedAt      string `json:"started_at"`
	CompletedAt    string `json:"completed_at"`
}

type Expectation struct {
	RequestID            string
	CandidateVersion     string
	FrameworkCommit      string
	ModuleChecksum       string
	ContractSuiteVersion string
	TMSConsumerCommit    string
	ParcelConsumerCommit string
}

// Read 从严格 JSON 文件读取 manifest，拒绝未知字段和尾随内容。
func Read(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, errors.New("manifest contains trailing JSON value")
		}
		return Manifest{}, fmt.Errorf("decode trailing manifest content: %w", err)
	}
	return manifest, nil
}

// ValidateBundle 验证 manifest 与两份原始证明的内容合同，不验证 WORM 或签名来源。
func ValidateBundle(manifest Manifest, tmsProof, parcelProof consumerproof.Proof, tmsBytes, parcelBytes []byte, expected Expectation) error {
	proofExpectation := consumerproof.Expectation{
		CandidateVersion:     expected.CandidateVersion,
		ModuleChecksum:       expected.ModuleChecksum,
		ContractSuiteVersion: expected.ContractSuiteVersion,
		TMSConsumerCommit:    expected.TMSConsumerCommit,
		ParcelConsumerCommit: expected.ParcelConsumerCommit,
	}
	if err := consumerproof.ValidatePair(tmsProof, parcelProof, proofExpectation); err != nil {
		return err
	}
	if manifest.FormatVersion != FormatVersion {
		return fmt.Errorf("format_version = %d, want %d", manifest.FormatVersion, FormatVersion)
	}
	if manifest.RequestID != expected.RequestID || !requestIDPattern.MatchString(manifest.RequestID) {
		return errors.New("request_id must match the expected 32-character lowercase hexadecimal value")
	}
	if err := validateCandidate(manifest.Candidate, expected); err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	if manifest.ContractSuiteVersion != expected.ContractSuiteVersion {
		return errors.New("contract_suite_version does not match the expected suite")
	}
	requestedAt, err := parseTime("requested_at", manifest.RequestedAt)
	if err != nil {
		return err
	}
	deadlineAt, err := parseTime("deadline_at", manifest.DeadlineAt)
	if err != nil {
		return err
	}
	if !deadlineAt.After(requestedAt) || deadlineAt.Sub(requestedAt) > MaxCoordinationTime {
		return fmt.Errorf("coordination window must be greater than zero and no more than %s", MaxCoordinationTime)
	}
	frameworkCompletedAt, err := validateFramework(manifest.Framework)
	if err != nil {
		return fmt.Errorf("framework: %w", err)
	}
	if frameworkCompletedAt.After(requestedAt) {
		return errors.New("framework completed_at must not be after requested_at")
	}
	tmsCompletedAt, err := validateConsumer("TMS", manifest.TMS, expected.TMSConsumerCommit, tmsProof, tmsBytes, requestedAt, deadlineAt)
	if err != nil {
		return err
	}
	parcelCompletedAt, err := validateConsumer("Parcel", manifest.Parcel, expected.ParcelConsumerCommit, parcelProof, parcelBytes, requestedAt, deadlineAt)
	if err != nil {
		return err
	}
	if manifest.TMS.RepositoryID == manifest.Parcel.RepositoryID || manifest.TMS.Repository == manifest.Parcel.Repository {
		return errors.New("TMS and Parcel evidence must come from different repositories")
	}
	if manifest.Framework.RepositoryID == manifest.TMS.RepositoryID || manifest.Framework.RepositoryID == manifest.Parcel.RepositoryID {
		return errors.New("framework and consumer repository IDs must be different")
	}
	acknowledged := consumerproof.GovernanceMode(tmsProof, parcelProof)
	if acknowledged == "" {
		return fmt.Errorf("TMS acknowledged governance mode %q but Parcel acknowledged %q; both consumers must acknowledge the same mode",
			tmsProof.AcknowledgedGovernanceMode, parcelProof.AcknowledgedGovernanceMode)
	}
	if acknowledged != manifest.Framework.GovernanceMode {
		return fmt.Errorf("consumers acknowledged governance mode %q but the framework declares %q",
			acknowledged, manifest.Framework.GovernanceMode)
	}
	createdAt, err := parseTime("created_at", manifest.CreatedAt)
	if err != nil {
		return err
	}
	latestCompletion := tmsCompletedAt
	if parcelCompletedAt.After(latestCompletion) {
		latestCompletion = parcelCompletedAt
	}
	if createdAt.Before(latestCompletion) || createdAt.After(deadlineAt) {
		return errors.New("created_at must be at or after both consumer completions and no later than deadline_at")
	}
	return nil
}

func validateCandidate(candidate Candidate, expected Expectation) error {
	if candidate.Version != expected.CandidateVersion || !semver.IsValid(candidate.Version) || semver.Prerelease(candidate.Version) == "" {
		return errors.New("version does not match the expected prerelease candidate")
	}
	if candidate.FrameworkCommit != expected.FrameworkCommit || !fullGitObjectID.MatchString(candidate.FrameworkCommit) {
		return errors.New("framework_commit does not match the expected full Git object ID")
	}
	if candidate.ModulePath != ModulePath {
		return fmt.Errorf("module_path = %q, want %q", candidate.ModulePath, ModulePath)
	}
	if candidate.ModuleChecksum != expected.ModuleChecksum {
		return errors.New("module_checksum does not match the expected candidate")
	}
	if candidate.GitOrigin != GitOrigin {
		return fmt.Errorf("git_origin = %q, want %q", candidate.GitOrigin, GitOrigin)
	}
	comparison := candidate.APIComparison
	if !sha256DigestPattern.MatchString(comparison.CandidateSHA256) {
		return errors.New("api_comparison.candidate_sha256 must be a SHA-256 digest")
	}
	switch comparison.Mode {
	case BaselineInitialV01:
		if candidate.Version != "v0.1.0-rc.1" || comparison.BaselineVersion != "" || comparison.BaselineSHA256 != "" {
			return errors.New("initial-v0-1 is only valid for v0.1.0-rc.1 without a baseline version or digest")
		}
	case BaselineImmutable:
		if !semver.IsValid(comparison.BaselineVersion) || !sha256DigestPattern.MatchString(comparison.BaselineSHA256) {
			return errors.New("immutable API comparison requires a valid baseline version and SHA-256 digest")
		}
	default:
		return fmt.Errorf("unsupported api comparison mode %q", comparison.Mode)
	}
	return nil
}

func validateFramework(run FrameworkRun) (time.Time, error) {
	if run.RepositoryID <= 0 || run.WorkflowID <= 0 || run.RunID <= 0 || run.RunAttempt <= 0 {
		return time.Time{}, errors.New("repository_id, workflow_id, run_id and run_attempt must be positive")
	}
	if !fullGitObjectID.MatchString(run.WorkflowSHA) {
		return time.Time{}, errors.New("workflow_sha must be a full lowercase Git object ID")
	}
	if !validToken(run.WorkflowRef, 512) || !validToken(run.RunnerImage, 256) {
		return time.Time{}, errors.New("workflow_ref and runner_image must be printable non-whitespace tokens")
	}
	if !goVersionPattern.MatchString(run.GoVersion) {
		return time.Time{}, errors.New("go_version must include an exact patch version")
	}
	if !containerImagePattern.MatchString(run.PostgresImage) {
		return time.Time{}, errors.New("postgres_image must include an immutable sha256 digest")
	}
	if run.Result != ResultPass {
		return time.Time{}, fmt.Errorf("result = %q, want %q", run.Result, ResultPass)
	}
	if !sha256DigestPattern.MatchString(run.CandidatecheckOutputSHA256) {
		return time.Time{}, errors.New("candidatecheck_output_sha256 must be a SHA-256 digest")
	}
	if !validGovernanceMode(run.GovernanceMode) {
		return time.Time{}, errors.New("governance_mode must be an uppercase printable ASCII token of at most 64 bytes")
	}
	return parseTime("completed_at", run.CompletedAt)
}

func validateConsumer(name string, run ConsumerRun, expectedCommit string, proof consumerproof.Proof, proofBytes []byte, requestedAt, deadlineAt time.Time) (time.Time, error) {
	if run.RepositoryID <= 0 || run.WorkflowID <= 0 || run.RunID <= 0 || run.RunAttempt <= 0 || run.ActorID <= 0 || run.ArtifactID <= 0 {
		return time.Time{}, fmt.Errorf("%s source IDs and run_attempt must be positive", name)
	}
	if !repositoryPattern.MatchString(run.Repository) {
		return time.Time{}, fmt.Errorf("%s repository must be owner/name", name)
	}
	if run.ExpectedCommit != expectedCommit || !fullGitObjectID.MatchString(run.ExpectedCommit) {
		return time.Time{}, fmt.Errorf("%s expected_commit does not match the approved baseline", name)
	}
	if !fullGitObjectID.MatchString(run.WorkflowSHA) {
		return time.Time{}, fmt.Errorf("%s workflow_sha must be a full lowercase Git object ID", name)
	}
	if !validToken(run.WorkflowRef, 512) {
		return time.Time{}, fmt.Errorf("%s workflow_ref must be a printable non-whitespace token", name)
	}
	if run.Event != EventWorkflowDispatch || run.Conclusion != ConclusionSuccess {
		return time.Time{}, fmt.Errorf("%s workflow must be a successful workflow_dispatch run", name)
	}
	if !sha256DigestPattern.MatchString(run.ArtifactDigest) {
		return time.Time{}, fmt.Errorf("%s artifact_digest must be a SHA-256 digest", name)
	}
	if run.ProofSHA256 != digest(proofBytes) {
		return time.Time{}, fmt.Errorf("%s proof_sha256 does not match the original proof bytes", name)
	}
	startedAt, err := parseTime(name+" started_at", run.StartedAt)
	if err != nil {
		return time.Time{}, err
	}
	completedAt, err := parseTime(name+" completed_at", run.CompletedAt)
	if err != nil {
		return time.Time{}, err
	}
	proofCompletedAt, err := parseTime(name+" proof completed_at", proof.CompletedAt)
	if err != nil {
		return time.Time{}, err
	}
	if startedAt.Before(requestedAt) || completedAt.Before(startedAt) || completedAt.After(deadlineAt) {
		return time.Time{}, fmt.Errorf("%s run must start within the coordination window and complete by deadline_at", name)
	}
	if proofCompletedAt.Before(startedAt) || proofCompletedAt.After(completedAt) {
		return time.Time{}, fmt.Errorf("%s proof completed_at must fall within the workflow run", name)
	}
	return completedAt, nil
}

func parseTime(field, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339Nano: %w", field, err)
	}
	return parsed, nil
}

// validGovernanceMode 要求大写可打印 ASCII，上限 64 字节，与 consumerproof
// 保持同一取值面，否则三方比对会因大小写变体而失败。
func validGovernanceMode(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
		if character >= 'a' && character <= 'z' {
			return false
		}
	}
	return true
}

func validToken(value string, maxLength int) bool {
	if value == "" || len(value) > maxLength {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
