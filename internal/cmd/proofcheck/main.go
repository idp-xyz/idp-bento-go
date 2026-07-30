package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"go.idp.xyz/idp-bento-go/internal/consumerproof"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "proofcheck:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("proofcheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	candidate := flags.String("candidate", "", "expected prerelease candidate version")
	checksum := flags.String("checksum", "", "expected Go module h1 checksum")
	contractSuite := flags.String("contract-suite", "", "expected contract suite version")
	tmsCommit := flags.String("tms-commit", "", "expected TMS consumer Git commit")
	parcelCommit := flags.String("parcel-commit", "", "expected Parcel consumer Git commit")
	tmsPath := flags.String("tms", "", "TMS proof JSON path")
	parcelPath := flags.String("parcel", "", "Parcel proof JSON path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *candidate == "" || *checksum == "" || *contractSuite == "" || *tmsCommit == "" || *parcelCommit == "" || *tmsPath == "" || *parcelPath == "" {
		return errors.New("-candidate, -checksum, -contract-suite, -tms-commit, -parcel-commit, -tms and -parcel are required")
	}
	tms, err := consumerproof.Read(*tmsPath)
	if err != nil {
		return fmt.Errorf("read TMS proof: %w", err)
	}
	parcel, err := consumerproof.Read(*parcelPath)
	if err != nil {
		return fmt.Errorf("read Parcel proof: %w", err)
	}
	expected := consumerproof.Expectation{
		CandidateVersion:     *candidate,
		ModuleChecksum:       *checksum,
		ContractSuiteVersion: *contractSuite,
		TMSConsumerCommit:    *tmsCommit,
		ParcelConsumerCommit: *parcelCommit,
	}
	if err := consumerproof.ValidatePair(tms, parcel, expected); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "proofcheck: TMS %s and Parcel %s passed %s %s with suite %s\n",
		tms.ConsumerCommit, parcel.ConsumerCommit, expected.CandidateVersion, expected.ModuleChecksum, expected.ContractSuiteVersion)
	return nil
}
