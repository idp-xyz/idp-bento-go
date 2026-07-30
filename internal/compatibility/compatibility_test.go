package compatibility

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/gcexportdata"
)

func TestAPINegativeFixtures(t *testing.T) {
	t.Parallel()
	fixtures := []struct {
		name string
		old  string
		new  string
	}{
		{
			name: "delete function",
			old:  "package fixture\nfunc Removed() {}",
			new:  "package fixture",
		},
		{
			name: "add interface method",
			old:  "package fixture\ntype Port interface { Read() }",
			new:  "package fixture\ntype Port interface { Read(); Write() }",
		},
		{
			name: "narrow generic constraint",
			old:  "package fixture\nfunc Keep[T ~int | ~string](value T) {}",
			new:  "package fixture\nfunc Keep[T ~int](value T) {}",
		},
		{
			name: "change struct field",
			old:  "package fixture\ntype Item struct { Value string }",
			new:  "package fixture\ntype Item struct { Value int }",
		},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			changes := compareTypePackages("fixture", typeCheckPackage(t, fixture.old), typeCheckPackage(t, fixture.new))
			if !hasIncompatibleChange(changes) {
				t.Fatalf("negative fixture was accepted: %#v", changes)
			}
		})
	}
}

func TestPackageDeletionIsIncompatible(t *testing.T) {
	t.Parallel()
	old := packageManifest{FormatVersion: 1, Packages: []manifestPackage{
		{Path: "domain", Stability: "STABLE-CORE"},
		{Path: "repository", Stability: "STABLE-CORE"},
	}}
	new := packageManifest{FormatVersion: 1, Packages: []manifestPackage{
		{Path: "domain", Stability: "STABLE-CORE"},
	}}
	changes := comparePackageManifests(old, new)
	assertIncompatibleMessage(t, changes, "package repository removed")
}

func TestCurrentAPISnapshotsCompareCleanly(t *testing.T) {
	t.Parallel()
	apiDir := filepath.Join(repositoryRoot(t), "api")
	changes, err := CompareAPI(apiDir, apiDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("current snapshots differ from themselves: %#v", changes)
	}
}

func TestCanonicalBuildEnvironment(t *testing.T) {
	t.Parallel()
	got := canonicalBuildEnvironment([]string{
		"PATH=fixture",
		"GOOS=windows",
		"goarch=386",
		"CGO_ENABLED=1",
	})
	want := []string{
		"PATH=fixture",
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("environment = %q, want %q", got, want)
	}
}

func TestCurrentSnapshotMatchesCanonicalPackage(t *testing.T) {
	repository := repositoryRoot(t)
	report, err := CompareSnapshotToPackage(
		filepath.Join(repository, "api", "snapshots", "domain.apidiff"),
		"go.idp.xyz/idp-bento-go/domain",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 0 {
		t.Fatalf("canonical package differs from snapshot: %s", report.String())
	}
}

func TestCandidateSnapshotUpdateCannotRewriteImmutableAPIBaseline(t *testing.T) {
	t.Parallel()
	baselineDir := filepath.Join(t.TempDir(), "baseline")
	candidateDir := filepath.Join(t.TempDir(), "candidate")
	manifest := packageManifest{FormatVersion: 1, Packages: []manifestPackage{{Path: "fixture", Stability: "STABLE-CORE"}}}
	for _, directory := range []string{baselineDir, candidateDir} {
		if err := os.MkdirAll(filepath.Join(directory, "snapshots"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeJSON(t, filepath.Join(directory, "packages.json"), manifest)
	}
	writeSnapshot(t, filepath.Join(baselineDir, "snapshots", "fixture.apidiff"), typeCheckPackage(t, "package fixture\nfunc Removed() {}"))
	writeSnapshot(t, filepath.Join(candidateDir, "snapshots", "fixture.apidiff"), typeCheckPackage(t, "package fixture"))

	changes, err := CompareAPI(baselineDir, candidateDir)
	if err != nil {
		t.Fatal(err)
	}
	assertIncompatibleMessage(t, changes, "Removed: removed")
}

func TestBehaviorNegativeFixtures(t *testing.T) {
	t.Parallel()
	fixtures := []struct {
		name   string
		mutate func(*versionManifest)
		want   string
	}{
		{
			name: "reduce length limit",
			mutate: func(candidate *versionManifest) {
				candidate.Constants["MaxSourceLength"] = json.RawMessage("127")
			},
			want: "constant MaxSourceLength changed",
		},
		{
			name: "reject old valid payload",
			mutate: func(candidate *versionManifest) {
				candidate.Cases[0].Valid = false
				candidate.Cases[0].IsInvalidEnvelope = true
				candidate.Cases[0].Wire = nil
			},
			want: "case payload_old_legal validity changed",
		},
		{
			name: "change wire field",
			mutate: func(candidate *versionManifest) {
				candidate.WireFields[0].Name = "origin"
			},
			want: "wire field source removed",
		},
		{
			name: "drop unsupported version classification",
			mutate: func(candidate *versionManifest) {
				candidate.Cases[1].IsUnsupportedSpecVersion = false
			},
			want: "case unknown_spec ErrUnsupportedSpecVersion classification changed",
		},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			baseline := behaviorFixture()
			candidate := cloneManifest(t, baseline)
			fixture.mutate(&candidate)
			assertIncompatibleMessage(t, compareVersionManifests(baseline, candidate), fixture.want)
		})
	}
}

func TestCandidateUpdateCannotRewriteImmutableBehaviorBaseline(t *testing.T) {
	t.Parallel()
	baselineDir := filepath.Join(t.TempDir(), "baseline")
	candidateDir := filepath.Join(t.TempDir(), "candidate")
	for _, directory := range []string{baselineDir, candidateDir} {
		if err := os.MkdirAll(filepath.Join(directory, "behavior"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeJSON(t, filepath.Join(directory, "behavior", "envelope-current.json"), map[string]any{"current_spec_version": "1.0"})
	}
	baseline := behaviorFixture()
	candidate := cloneManifest(t, baseline)
	candidate.Constants["MaxSourceLength"] = json.RawMessage("127")
	writeJSON(t, filepath.Join(baselineDir, "behavior", "envelope-v1.json"), baseline)
	writeJSON(t, filepath.Join(candidateDir, "behavior", "envelope-v1.json"), candidate)

	changes, err := CompareBehavior(baselineDir, candidateDir)
	if err != nil {
		t.Fatal(err)
	}
	assertIncompatibleMessage(t, changes, "constant MaxSourceLength changed")
}

func typeCheckPackage(t *testing.T, source string) *types.Package {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "fixture.go", source, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	config := types.Config{Importer: importer.Default()}
	checked, err := config.Check("example.com/fixture", fileSet, []*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return checked
}

func hasIncompatibleChange(changes []Change) bool {
	for _, change := range changes {
		if !change.Compatible {
			return true
		}
	}
	return false
}

func behaviorFixture() versionManifest {
	return versionManifest{
		FormatVersion: 2,
		SpecVersion:   "1.0",
		Constants: map[string]json.RawMessage{
			"MaxSourceLength": json.RawMessage("255"),
		},
		WireFields: []wireField{{Name: "source", Required: true}},
		Cases: []behaviorCase{
			{
				ID:    "payload_old_legal",
				Input: "old legal payload",
				Valid: true,
				Wire:  json.RawMessage(`{"source":"idp://fixture","payload":{"value":1}}`),
			},
			{
				ID:                       "unknown_spec",
				Input:                    "unsupported version",
				IsInvalidEnvelope:        true,
				IsUnsupportedSpecVersion: true,
			},
		},
		Properties: map[string]bool{"round_trip_preserves_value": true},
	}
}

func cloneManifest(t *testing.T, source versionManifest) versionManifest {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var clone versionManifest
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func assertIncompatibleMessage(t *testing.T, changes []Change, want string) {
	t.Helper()
	for _, change := range changes {
		if !change.Compatible && strings.Contains(change.Message, want) {
			return
		}
	}
	t.Fatalf("missing incompatible change %q in %#v", want, changes)
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSnapshot(t *testing.T, path string, pkg *types.Package) {
	t.Helper()
	var output bytes.Buffer
	fmt.Fprintln(&output, pkg.Path())
	if err := gcexportdata.Write(&output, token.NewFileSet(), pkg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, output.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine repository root")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
