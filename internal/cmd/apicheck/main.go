package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"go.idp.xyz/idp-bento-go/internal/compatibility"
	"golang.org/x/exp/apidiff"
)

type compareFunc func(snapshotPath, importPath string) (apidiff.Report, error)

func main() {
	if err := run(os.Args[1:], os.Stdout, compatibility.CompareSnapshotToPackage); err != nil {
		fmt.Fprintln(os.Stderr, "apicheck:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, compare compareFunc) error {
	flags := flag.NewFlagSet("apicheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	snapshotPath := flags.String("snapshot", "", "committed API snapshot")
	importPath := flags.String("package", "", "current package import path")
	label := flags.String("label", "", "package label for failures")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(*snapshotPath) == "" || strings.TrimSpace(*importPath) == "" {
		return fmt.Errorf("-snapshot and -package are required")
	}
	if strings.TrimSpace(*label) == "" {
		*label = *importPath
	}

	report, err := compare(*snapshotPath, *importPath)
	if err != nil {
		return err
	}
	if len(report.Changes) == 0 {
		return nil
	}
	if err := report.Text(stdout); err != nil {
		return fmt.Errorf("write API change report: %w", err)
	}
	return fmt.Errorf("%s API differs from its snapshot", *label)
}
