package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type recordingRunner struct {
	steps     []commandSpec
	failLabel string
}

func (r *recordingRunner) Run(spec commandSpec) error {
	r.steps = append(r.steps, spec)
	if spec.Label == r.failLabel {
		return errors.New("injected failure")
	}
	return nil
}

func TestRunRequiresExplicitReleaseMode(t *testing.T) {
	t.Parallel()
	runner := &recordingRunner{}
	getenv := func(string) string { return "postgres://fixture" }
	for _, args := range [][]string{nil, {"-initial-v0-1", "-baseline", t.TempDir()}} {
		if err := run(args, repositoryRoot(t), getenv, runner, io.Discard); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("run(%v) error = %v", args, err)
		}
	}
}

func TestValidateCommandOutput(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		spec    commandSpec
		output  string
		wantErr string
	}{
		{name: "clean diff", spec: commandSpec{Label: "apidiff", RejectOutput: true}},
		{name: "reported diff", spec: commandSpec{Label: "apidiff", RejectOutput: true}, output: "changed\n", wantErr: "change report"},
		{name: "exact identity", spec: commandSpec{Label: "module", ExpectedOutput: modulePath}, output: modulePath + "\n"},
		{name: "identity drift", spec: commandSpec{Label: "module", ExpectedOutput: modulePath}, output: "example.invalid/module\n", wantErr: "want"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateCommandOutput(test.spec, []byte(test.output))
			if test.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestRunRequiresPostgresDSN(t *testing.T) {
	t.Parallel()
	runner := &recordingRunner{}
	err := run([]string{"-initial-v0-1"}, repositoryRoot(t), func(string) string { return "" }, runner, io.Discard)
	if err == nil || !strings.Contains(err.Error(), postgresDSNEnv) {
		t.Fatalf("error = %v", err)
	}
	if len(runner.steps) != 0 {
		t.Fatalf("executed %d steps before DSN preflight", len(runner.steps))
	}
}

func TestInitialV01RunsAllRepositoryGatesWithoutBaselineComparison(t *testing.T) {
	t.Parallel()
	runner := &recordingRunner{}
	err := run([]string{"-initial-v0-1"}, repositoryRoot(t), fixtureGetenv, runner, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	assertStep(t, runner.steps, "run uncached test suite", "go", "test", "-count=1", "./...")
	assertStep(t, runner.steps, "check production dependencies", "go", "run", "./internal/cmd/dependencycheck")
	assertExpectedOutput(t, runner.steps, "verify Go toolchain", "go1.26.5")
	assertExpectedOutput(t, runner.steps, "verify module identity", modulePath)
	assertAPIDiffSteps(t, runner.steps, 8)
	if len(runner.steps) != 18 {
		t.Fatalf("initial v0.1 gates = %d, want 18", len(runner.steps))
	}
	if hasStep(runner.steps, "compare immutable release baseline") {
		t.Fatal("initial v0.1 unexpectedly compared a previous baseline")
	}
}

func TestLaterCandidateRequiresIndependentBaselineComparison(t *testing.T) {
	t.Parallel()
	baseline := t.TempDir()
	runner := &recordingRunner{}
	err := run([]string{"-baseline", baseline}, repositoryRoot(t), fixtureGetenv, runner, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	assertStep(t, runner.steps, "compare immutable release baseline", "go", "run", "./internal/cmd/compatcheck", "-baseline", baseline, "-candidate", filepath.Join(repositoryRoot(t), "api"))
	if len(runner.steps) != 19 {
		t.Fatalf("later candidate gates = %d, want 19", len(runner.steps))
	}
}

func TestCandidateAPISnapshotCannotBeUsedAsBaseline(t *testing.T) {
	t.Parallel()
	root := repositoryRoot(t)
	err := run([]string{"-baseline", filepath.Join(root, "api")}, root, fixtureGetenv, &recordingRunner{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "independent") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunStopsAtFirstFailedGate(t *testing.T) {
	t.Parallel()
	runner := &recordingRunner{failLabel: "check production dependencies"}
	err := run([]string{"-initial-v0-1"}, repositoryRoot(t), fixtureGetenv, runner, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("error = %v", err)
	}
	if hasStep(runner.steps, "run static analysis") || hasStep(runner.steps, "run uncached test suite") {
		t.Fatal("later gates ran after a failure")
	}
}

func fixtureGetenv(name string) string {
	if name == postgresDSNEnv {
		return "postgres://fixture"
	}
	return ""
}

func assertAPIDiffSteps(t *testing.T, steps []commandSpec, want int) {
	t.Helper()
	count := 0
	for _, step := range steps {
		if strings.HasPrefix(step.Label, "compare API ") {
			count++
			if !step.RejectOutput {
				t.Fatalf("API diff step %q does not reject output", step.Label)
			}
		}
	}
	if count != want {
		t.Fatalf("API diff steps = %d, want %d", count, want)
	}
}

func assertStep(t *testing.T, steps []commandSpec, label, name string, args ...string) {
	t.Helper()
	for _, step := range steps {
		if step.Label == label {
			if step.Name != name || strings.Join(step.Args, "\x00") != strings.Join(args, "\x00") {
				t.Fatalf("step %q = %s %v, want %s %v", label, step.Name, step.Args, name, args)
			}
			return
		}
	}
	t.Fatalf("missing step %q", label)
}

func assertExpectedOutput(t *testing.T, steps []commandSpec, label, want string) {
	t.Helper()
	for _, step := range steps {
		if step.Label == label {
			if step.ExpectedOutput != want {
				t.Fatalf("step %q expected output = %q, want %q", label, step.ExpectedOutput, want)
			}
			return
		}
	}
	t.Fatalf("missing step %q", label)
}

func hasStep(steps []commandSpec, label string) bool {
	for _, step := range steps {
		if step.Label == label {
			return true
		}
	}
	return false
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine repository root")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	return root
}
