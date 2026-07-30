package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	modulePath     = "go.idp.xyz/idp-bento-go"
	postgresDSNEnv = "IDP_BENTO_POSTGRES_DSN"
)

type packageManifest struct {
	FormatVersion int               `json:"format_version"`
	Packages      []manifestPackage `json:"packages"`
}

type manifestPackage struct {
	Path      string `json:"path"`
	Stability string `json:"stability"`
}

type commandSpec struct {
	Label          string
	Name           string
	Args           []string
	RejectOutput   bool
	ExpectedOutput string
}

type commandRunner interface {
	Run(commandSpec) error
}

type execRunner struct {
	dir    string
	stdout io.Writer
	stderr io.Writer
}

func (r execRunner) Run(spec commandSpec) error {
	fmt.Fprintf(r.stdout, "candidatecheck: %s\n", spec.Label)
	command := exec.Command(spec.Name, spec.Args...)
	command.Dir = r.dir
	command.Stderr = r.stderr
	var captured bytes.Buffer
	if spec.RejectOutput || spec.ExpectedOutput != "" {
		command.Stdout = &captured
	} else {
		command.Stdout = r.stdout
	}
	if err := command.Run(); err != nil {
		if captured.Len() > 0 {
			_, _ = io.Copy(r.stdout, &captured)
		}
		return fmt.Errorf("%s: %w", spec.Label, err)
	}
	if err := validateCommandOutput(spec, captured.Bytes()); err != nil {
		_, _ = io.Copy(r.stdout, &captured)
		return err
	}
	if spec.ExpectedOutput != "" {
		fmt.Fprintln(r.stdout, strings.TrimSpace(captured.String()))
	}
	return nil
}

func validateCommandOutput(spec commandSpec, output []byte) error {
	if spec.RejectOutput && len(output) > 0 {
		return fmt.Errorf("%s produced a change report", spec.Label)
	}
	if spec.ExpectedOutput != "" {
		actual := strings.TrimSpace(string(output))
		if actual != spec.ExpectedOutput {
			return fmt.Errorf("%s = %q, want %q", spec.Label, actual, spec.ExpectedOutput)
		}
	}
	return nil
}

func main() {
	runner := execRunner{dir: ".", stdout: os.Stdout, stderr: os.Stderr}
	if err := run(os.Args[1:], ".", os.Getenv, runner, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "candidatecheck:", err)
		os.Exit(1)
	}
}

func run(args []string, dir string, getenv func(string) string, runner commandRunner, stdout io.Writer) error {
	flags := flag.NewFlagSet("candidatecheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	initialV01 := flags.Bool("initial-v0-1", false, "declare that no previous release baseline exists")
	baseline := flags.String("baseline", "", "immutable previous release api directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *initialV01 == (*baseline != "") {
		return errors.New("exactly one of -initial-v0-1 or -baseline is required")
	}
	if strings.TrimSpace(getenv(postgresDSNEnv)) == "" {
		return fmt.Errorf("%s is required; candidate tests must not skip PostgreSQL", postgresDSNEnv)
	}

	candidateAPI := filepath.Join(dir, "api")
	if *baseline != "" {
		same, err := sameDirectory(*baseline, candidateAPI)
		if err != nil {
			return err
		}
		if same {
			return errors.New("-baseline must be independent from the candidate api directory")
		}
	}

	goFiles, err := discoverGoFiles(dir)
	if err != nil {
		return err
	}
	packagePaths, err := readPackagePaths(filepath.Join(candidateAPI, "packages.json"))
	if err != nil {
		return err
	}
	steps := candidateSteps(goFiles, packagePaths, *baseline, candidateAPI)
	for _, step := range steps {
		if err := runner.Run(step); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "candidatecheck: %d gates passed; external RC and consumer evidence are still required\n", len(steps))
	return nil
}

func candidateSteps(goFiles, packagePaths []string, baseline, candidateAPI string) []commandSpec {
	steps := []commandSpec{
		{Label: "verify Go toolchain", Name: "go", Args: []string{"env", "GOVERSION"}, ExpectedOutput: "go1.26.5"},
		{Label: "verify module identity", Name: "go", Args: []string{"list", "-m"}, ExpectedOutput: modulePath},
		{Label: "check module files", Name: "go", Args: []string{"mod", "tidy", "-diff"}},
		{Label: "check Go formatting", Name: "gofmt", Args: append([]string{"-l", "-e"}, goFiles...), RejectOutput: true},
		{Label: "check API snapshots", Name: "go", Args: []string{"run", "./internal/cmd/apisnapshot", "-require-all"}},
		{Label: "check Envelope V1 manifest", Name: "go", Args: []string{"run", "./internal/cmd/compatmanifest", "-mode", "version", "-out", "api/behavior/envelope-v1.json"}},
		{Label: "check current Envelope manifest", Name: "go", Args: []string{"run", "./internal/cmd/compatmanifest", "-mode", "current", "-source", "api/behavior/envelope-v1.json", "-out", "api/behavior/envelope-current.json"}},
		{Label: "check production dependencies", Name: "go", Args: []string{"run", "./internal/cmd/dependencycheck"}},
	}
	for _, packagePath := range packagePaths {
		filename := strings.ReplaceAll(packagePath, "/", "-") + ".apidiff"
		steps = append(steps, commandSpec{
			Label:        "compare API " + packagePath,
			Name:         "go",
			Args:         []string{"tool", "apidiff", filepath.Join(candidateAPI, "snapshots", filename), modulePath + "/" + packagePath},
			RejectOutput: true,
		})
	}
	if baseline != "" {
		steps = append(steps, commandSpec{
			Label: "compare immutable release baseline",
			Name:  "go",
			Args:  []string{"run", "./internal/cmd/compatcheck", "-baseline", baseline, "-candidate", candidateAPI},
		})
	}
	steps = append(steps,
		commandSpec{Label: "run static analysis", Name: "go", Args: []string{"vet", "./..."}},
		commandSpec{Label: "run uncached test suite", Name: "go", Args: []string{"test", "-count=1", "./..."}},
	)
	return steps
}

func discoverGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == "backup" || entry.Name() == ".git") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			absolute, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			files = append(files, absolute)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover Go files: %w", err)
	}
	if len(files) == 0 {
		return nil, errors.New("no Go files found")
	}
	sort.Strings(files)
	return files, nil
}

func readPackagePaths(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open package manifest: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(bufio.NewReader(file))
	decoder.DisallowUnknownFields()
	var manifest packageManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode package manifest: %w", err)
	}
	if manifest.FormatVersion != 1 || len(manifest.Packages) == 0 {
		return nil, errors.New("package manifest must use format version 1 and contain packages")
	}
	paths := make([]string, 0, len(manifest.Packages))
	seen := make(map[string]bool, len(manifest.Packages))
	for _, entry := range manifest.Packages {
		if entry.Path == "" || entry.Stability == "" || seen[entry.Path] {
			return nil, fmt.Errorf("invalid or duplicate package path %q", entry.Path)
		}
		seen[entry.Path] = true
		paths = append(paths, entry.Path)
	}
	return paths, nil
}

func sameDirectory(left, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, fmt.Errorf("stat baseline: %w", err)
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return false, fmt.Errorf("stat candidate API: %w", err)
	}
	return os.SameFile(leftInfo, rightInfo), nil
}
