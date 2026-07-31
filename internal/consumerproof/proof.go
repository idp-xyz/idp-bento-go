package consumerproof

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	FormatVersion  = 1
	ModulePath     = "go.idp.xyz/idp-bento-go"
	ConsumerTMS    = "TMS"
	ConsumerParcel = "PARCEL"
	ResultPass     = "PASS"
	ResultFail     = "FAIL"
)

var fullGitObjectID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// Proof 是消费者返回给框架的最小合同结果，不包含业务源码或测试数据。
type Proof struct {
	FormatVersion        int    `json:"format_version"`
	Consumer             string `json:"consumer"`
	ConsumerCommit       string `json:"consumer_commit"`
	CandidateVersion     string `json:"candidate_version"`
	ModulePath           string `json:"module_path"`
	ModuleChecksum       string `json:"module_checksum"`
	ContractSuiteVersion string `json:"contract_suite_version"`
	Result               string `json:"result"`
	CompletedAt          string `json:"completed_at"`

	// AcknowledgedGovernanceMode 是消费者对框架治理模式的确认，不是声明。
	// 权威值在发布证据 manifest 的 framework 块，由 evidencecheck 比对。
	AcknowledgedGovernanceMode string `json:"acknowledged_governance_mode"`
}

// Expectation 固定协调作业正在核验的不可变候选与消费者基线身份。
type Expectation struct {
	CandidateVersion     string
	ModuleChecksum       string
	ContractSuiteVersion string
	TMSConsumerCommit    string
	ParcelConsumerCommit string
}

// GovernanceMode 报告两个消费者一致确认的治理模式。只有 ValidatePair 通过后
// 该值才有意义，调用方据此与发布证据 manifest 的权威值比对。
func GovernanceMode(tms, parcel Proof) string {
	if tms.AcknowledgedGovernanceMode != parcel.AcknowledgedGovernanceMode {
		return ""
	}
	return tms.AcknowledgedGovernanceMode
}

// Read 从严格 JSON 文件读取一份证明，拒绝未知字段和尾随内容。
func Read(path string) (Proof, error) {
	file, err := os.Open(path)
	if err != nil {
		return Proof{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var proof Proof
	if err := decoder.Decode(&proof); err != nil {
		return Proof{}, fmt.Errorf("decode proof: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Proof{}, errors.New("proof contains trailing JSON value")
		}
		return Proof{}, fmt.Errorf("decode trailing proof content: %w", err)
	}
	return proof, nil
}

// ValidatePair 验证两个消费者对同一候选和合同套件均返回 PASS。
func ValidatePair(tms, parcel Proof, expected Expectation) error {
	if err := validateExpectation(expected); err != nil {
		return err
	}
	if err := validateProof(tms, ConsumerTMS, expected.TMSConsumerCommit, expected); err != nil {
		return fmt.Errorf("TMS proof: %w", err)
	}
	if err := validateProof(parcel, ConsumerParcel, expected.ParcelConsumerCommit, expected); err != nil {
		return fmt.Errorf("Parcel proof: %w", err)
	}
	return nil
}

func validateExpectation(expected Expectation) error {
	if !semver.IsValid(expected.CandidateVersion) || semver.Prerelease(expected.CandidateVersion) == "" {
		return fmt.Errorf("candidate version %q must be valid prerelease SemVer", expected.CandidateVersion)
	}
	if !validModuleChecksum(expected.ModuleChecksum) {
		return errors.New("candidate module checksum must be an h1 SHA-256 checksum")
	}
	if !validToken(expected.ContractSuiteVersion) {
		return errors.New("contract suite version must be a non-empty printable ASCII token")
	}
	if !fullGitObjectID.MatchString(expected.TMSConsumerCommit) {
		return errors.New("expected TMS consumer commit must be a full lowercase SHA-1 or SHA-256 Git object ID")
	}
	if !fullGitObjectID.MatchString(expected.ParcelConsumerCommit) {
		return errors.New("expected Parcel consumer commit must be a full lowercase SHA-1 or SHA-256 Git object ID")
	}
	return nil
}

func validateProof(proof Proof, consumer, expectedCommit string, expected Expectation) error {
	if proof.FormatVersion != FormatVersion {
		return fmt.Errorf("format_version = %d, want %d", proof.FormatVersion, FormatVersion)
	}
	if proof.Consumer != consumer {
		return fmt.Errorf("consumer = %q, want %q", proof.Consumer, consumer)
	}
	if !fullGitObjectID.MatchString(proof.ConsumerCommit) {
		return errors.New("consumer_commit must be a full lowercase SHA-1 or SHA-256 Git object ID")
	}
	if proof.ConsumerCommit != expectedCommit {
		return fmt.Errorf("consumer_commit = %q, want %q", proof.ConsumerCommit, expectedCommit)
	}
	if proof.CandidateVersion != expected.CandidateVersion {
		return fmt.Errorf("candidate_version = %q, want %q", proof.CandidateVersion, expected.CandidateVersion)
	}
	if proof.ModulePath != ModulePath {
		return fmt.Errorf("module_path = %q, want %q", proof.ModulePath, ModulePath)
	}
	if proof.ModuleChecksum != expected.ModuleChecksum || !validModuleChecksum(proof.ModuleChecksum) {
		return errors.New("module_checksum does not match the expected candidate")
	}
	if proof.ContractSuiteVersion != expected.ContractSuiteVersion || !validToken(proof.ContractSuiteVersion) {
		return errors.New("contract_suite_version does not match the expected suite")
	}
	if proof.Result != ResultPass {
		if proof.Result == ResultFail {
			return errors.New("result is FAIL")
		}
		return fmt.Errorf("unsupported result %q", proof.Result)
	}
	if _, err := time.Parse(time.RFC3339Nano, proof.CompletedAt); err != nil {
		return fmt.Errorf("completed_at must be RFC3339Nano: %w", err)
	}
	if !validGovernanceMode(proof.AcknowledgedGovernanceMode) {
		return errors.New("acknowledged_governance_mode must be an uppercase printable ASCII token of at most 64 bytes")
	}
	return nil
}

func validModuleChecksum(value string) bool {
	if !strings.HasPrefix(value, "h1:") {
		return false
	}
	digest, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "h1:"))
	return err == nil && len(digest) == 32
}

// validGovernanceMode 比 validToken 更严：大写、上限 64 字节。该值随发布证据
// 写入 WORM 后不可撤回，所以取值面在这里收窄，避免大小写变体产生两个含义相同
// 却比对失败的取值。
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

func validToken(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
