package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"golang.org/x/exp/apidiff"
)

func TestRunIsSilentWhenSnapshotMatches(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	compare := func(snapshotPath, importPath string) (apidiff.Report, error) {
		if snapshotPath != "fixture.apidiff" || importPath != "example.com/fixture" {
			t.Fatalf("compare(%q, %q)", snapshotPath, importPath)
		}
		return apidiff.Report{}, nil
	}
	err := run([]string{"-snapshot", "fixture.apidiff", "-package", "example.com/fixture"}, &stdout, compare)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunPrintsOfficialReportAndFailsOnChanges(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	compare := func(string, string) (apidiff.Report, error) {
		return apidiff.Report{Changes: []apidiff.Change{{Message: "Removed: removed", Compatible: false}}}, nil
	}
	err := run([]string{"-snapshot", "fixture.apidiff", "-package", "example.com/fixture", "-label", "fixture"}, &stdout, compare)
	if err == nil || !strings.Contains(err.Error(), "fixture API differs") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Removed: removed") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunRejectsMissingRequiredFlags(t *testing.T) {
	t.Parallel()
	err := run(nil, &bytes.Buffer{}, func(string, string) (apidiff.Report, error) {
		return apidiff.Report{}, errors.New("must not run")
	})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("error = %v", err)
	}
}
