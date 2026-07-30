package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"go.idp.xyz/idp-bento-go/internal/compatibility"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "compatcheck:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("compatcheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	baseline := flags.String("baseline", "", "immutable previous release api directory")
	candidate := flags.String("candidate", "api", "candidate api directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *baseline == "" {
		return errors.New("-baseline is required and must point to an immutable previous release")
	}
	same, err := sameDirectory(*baseline, *candidate)
	if err != nil {
		return err
	}
	if same {
		return errors.New("-baseline and -candidate must be different directories")
	}
	apiChanges, err := compatibility.CompareAPI(*baseline, *candidate)
	if err != nil {
		return err
	}
	behaviorChanges, err := compatibility.CompareBehavior(*baseline, *candidate)
	if err != nil {
		return err
	}
	changes := append(apiChanges, behaviorChanges...)
	if len(changes) == 0 {
		fmt.Fprintln(stdout, "compatcheck: immutable baseline and candidate are compatible and unchanged")
		return nil
	}
	if err := compatibility.WriteReport(stdout, changes); err != nil {
		return err
	}
	return fmt.Errorf("candidate has %d public compatibility change(s); release governance evidence is required", len(changes))
}

func sameDirectory(left, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, fmt.Errorf("stat baseline: %w", err)
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return false, fmt.Errorf("stat candidate: %w", err)
	}
	return os.SameFile(leftInfo, rightInfo), nil
}
