package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"go.idp.xyz/idp-bento-go/internal/consumerproof"
	"go.idp.xyz/idp-bento-go/internal/releaseevidence"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "evidencecheck:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("evidencecheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	manifestPath := flags.String("manifest", "", "release evidence manifest JSON path")
	tmsPath := flags.String("tms-proof", "", "original TMS proof JSON path")
	parcelPath := flags.String("parcel-proof", "", "original Parcel proof JSON path")
	requestID := flags.String("request-id", "", "expected coordination request ID")
	candidate := flags.String("candidate", "", "expected prerelease candidate version")
	frameworkCommit := flags.String("framework-commit", "", "expected framework Git commit")
	checksum := flags.String("checksum", "", "expected Go module h1 checksum")
	contractSuite := flags.String("contract-suite", "", "expected contract suite version")
	tmsCommit := flags.String("tms-commit", "", "expected TMS consumer Git commit")
	parcelCommit := flags.String("parcel-commit", "", "expected Parcel consumer Git commit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *manifestPath == "" || *tmsPath == "" || *parcelPath == "" || *requestID == "" || *candidate == "" || *frameworkCommit == "" || *checksum == "" || *contractSuite == "" || *tmsCommit == "" || *parcelCommit == "" {
		return errors.New("-manifest, -tms-proof, -parcel-proof, -request-id, -candidate, -framework-commit, -checksum, -contract-suite, -tms-commit and -parcel-commit are required")
	}
	manifest, err := releaseevidence.Read(*manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	tmsProof, tmsBytes, err := readProof(*tmsPath)
	if err != nil {
		return fmt.Errorf("read TMS proof: %w", err)
	}
	parcelProof, parcelBytes, err := readProof(*parcelPath)
	if err != nil {
		return fmt.Errorf("read Parcel proof: %w", err)
	}
	expected := releaseevidence.Expectation{
		RequestID:            *requestID,
		CandidateVersion:     *candidate,
		FrameworkCommit:      *frameworkCommit,
		ModuleChecksum:       *checksum,
		ContractSuiteVersion: *contractSuite,
		TMSConsumerCommit:    *tmsCommit,
		ParcelConsumerCommit: *parcelCommit,
	}
	if err := releaseevidence.ValidateBundle(manifest, tmsProof, parcelProof, tmsBytes, parcelBytes, expected); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "evidencecheck: request %s passed %s with TMS %s and Parcel %s\n", manifest.RequestID, expected.CandidateVersion, expected.TMSConsumerCommit, expected.ParcelConsumerCommit)
	return nil
}

func readProof(path string) (consumerproof.Proof, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return consumerproof.Proof{}, nil, err
	}
	proof, err := consumerproof.Read(path)
	if err != nil {
		return consumerproof.Proof{}, nil, err
	}
	return proof, data, nil
}
