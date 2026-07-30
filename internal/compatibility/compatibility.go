package compatibility

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/exp/apidiff"
	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"
)

// Change is one compatibility finding between an immutable baseline and a candidate.
type Change struct {
	Layer      string
	Message    string
	Compatible bool
}

type packageManifest struct {
	FormatVersion int               `json:"format_version"`
	Packages      []manifestPackage `json:"packages"`
}

type manifestPackage struct {
	Path      string `json:"path"`
	Stability string `json:"stability"`
}

// CompareAPI compares package ownership and semantic API snapshots under two api directories.
func CompareAPI(baselineDir, candidateDir string) ([]Change, error) {
	baseline, err := readPackageManifest(filepath.Join(baselineDir, "packages.json"))
	if err != nil {
		return nil, fmt.Errorf("read baseline package manifest: %w", err)
	}
	candidate, err := readPackageManifest(filepath.Join(candidateDir, "packages.json"))
	if err != nil {
		return nil, fmt.Errorf("read candidate package manifest: %w", err)
	}

	changes := comparePackageManifests(baseline, candidate)
	candidateByPath := manifestPackagesByPath(candidate.Packages)
	for _, oldEntry := range baseline.Packages {
		if _, ok := candidateByPath[oldEntry.Path]; !ok {
			continue
		}
		filename := snapshotFilename(oldEntry.Path)
		oldPackage, err := readSnapshot(filepath.Join(baselineDir, "snapshots", filename))
		if err != nil {
			return nil, fmt.Errorf("read baseline snapshot %q: %w", oldEntry.Path, err)
		}
		newPackage, err := readSnapshot(filepath.Join(candidateDir, "snapshots", filename))
		if err != nil {
			return nil, fmt.Errorf("read candidate snapshot %q: %w", oldEntry.Path, err)
		}
		changes = append(changes, compareTypePackages(oldEntry.Path, oldPackage, newPackage)...)
	}
	sortChanges(changes)
	return changes, nil
}

func compareTypePackages(relativePath string, oldPackage, newPackage *types.Package) []Change {
	var changes []Change
	for _, apiChange := range apidiff.Changes(oldPackage, newPackage).Changes {
		changes = append(changes, Change{
			Layer:      "api",
			Message:    relativePath + ": " + apiChange.Message,
			Compatible: apiChange.Compatible,
		})
	}
	oldConstraints := exportedGenericConstraints(oldPackage)
	newConstraints := exportedGenericConstraints(newPackage)
	for name, oldConstraint := range oldConstraints {
		newConstraint, ok := newConstraints[name]
		if ok && oldConstraint != newConstraint {
			changes = append(changes, Change{
				Layer:   "api",
				Message: fmt.Sprintf("%s: %s generic constraint changed from %s to %s", relativePath, name, oldConstraint, newConstraint),
			})
		}
	}
	return changes
}

// LoadCanonicalPackage loads one package for the platform used by committed API snapshots.
func LoadCanonicalPackage(importPath string) (*types.Package, error) {
	config := &packages.Config{
		Mode: packages.LoadTypes | packages.NeedName | packages.NeedTypes | packages.NeedImports | packages.NeedDeps,
		Env:  canonicalBuildEnvironment(os.Environ()),
	}
	loaded, err := packages.Load(config, importPath)
	if err != nil {
		return nil, fmt.Errorf("load package %q: %w", importPath, err)
	}
	if len(loaded) != 1 {
		return nil, fmt.Errorf("load package %q: got %d packages", importPath, len(loaded))
	}
	if len(loaded[0].Errors) > 0 {
		messages := make([]string, 0, len(loaded[0].Errors))
		for _, packageError := range loaded[0].Errors {
			messages = append(messages, packageError.Error())
		}
		return nil, fmt.Errorf("load package %q: %s", importPath, strings.Join(messages, "; "))
	}
	return loaded[0].Types, nil
}

// CompareSnapshotToPackage compares committed export data with current source under the canonical target.
func CompareSnapshotToPackage(snapshotPath, importPath string) (apidiff.Report, error) {
	baseline, err := readSnapshot(snapshotPath)
	if err != nil {
		return apidiff.Report{}, fmt.Errorf("read snapshot: %w", err)
	}
	candidate, err := LoadCanonicalPackage(importPath)
	if err != nil {
		return apidiff.Report{}, err
	}
	return apidiff.Changes(baseline, candidate), nil
}

func canonicalBuildEnvironment(base []string) []string {
	overrides := []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"}
	overridden := make(map[string]bool, len(overrides))
	for _, entry := range overrides {
		key, _, _ := strings.Cut(entry, "=")
		overridden[strings.ToUpper(key)] = true
	}

	merged := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok && overridden[strings.ToUpper(key)] {
			continue
		}
		merged = append(merged, entry)
	}
	return append(merged, overrides...)
}

func exportedGenericConstraints(pkg *types.Package) map[string]string {
	result := make(map[string]string)
	qualifier := func(imported *types.Package) string {
		if imported.Path() == pkg.Path() {
			return ""
		}
		return imported.Path()
	}
	for _, name := range pkg.Scope().Names() {
		if !token.IsExported(name) {
			continue
		}
		object := pkg.Scope().Lookup(name)
		var parameters *types.TypeParamList
		switch typed := object.(type) {
		case *types.Func:
			parameters = typed.Type().(*types.Signature).TypeParams()
		case *types.TypeName:
			named, ok := typed.Type().(*types.Named)
			if !ok {
				continue
			}
			parameters = named.TypeParams()
		}
		if parameters == nil || parameters.Len() == 0 {
			continue
		}
		constraints := make([]string, 0, parameters.Len())
		for index := 0; index < parameters.Len(); index++ {
			constraints = append(constraints, types.TypeString(parameters.At(index).Constraint(), qualifier))
		}
		result[name] = strings.Join(constraints, ", ")
	}
	return result
}

func comparePackageManifests(baseline, candidate packageManifest) []Change {
	oldByPath := manifestPackagesByPath(baseline.Packages)
	newByPath := manifestPackagesByPath(candidate.Packages)
	var changes []Change
	for path, oldEntry := range oldByPath {
		newEntry, ok := newByPath[path]
		if !ok {
			changes = append(changes, Change{Layer: "packages", Message: fmt.Sprintf("package %s removed", path)})
			continue
		}
		if oldEntry.Stability != newEntry.Stability {
			changes = append(changes, Change{
				Layer:   "packages",
				Message: fmt.Sprintf("package %s stability changed from %s to %s", path, oldEntry.Stability, newEntry.Stability),
			})
		}
	}
	for path := range newByPath {
		if _, ok := oldByPath[path]; !ok {
			changes = append(changes, Change{Layer: "packages", Message: fmt.Sprintf("package %s added", path), Compatible: true})
		}
	}
	return changes
}

func readPackageManifest(path string) (packageManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return packageManifest{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest packageManifest
	if err := decoder.Decode(&manifest); err != nil {
		return packageManifest{}, err
	}
	if manifest.FormatVersion != 1 {
		return packageManifest{}, fmt.Errorf("unsupported format version %d", manifest.FormatVersion)
	}
	if len(manifest.Packages) == 0 {
		return packageManifest{}, errors.New("manifest contains no packages")
	}
	seen := make(map[string]bool, len(manifest.Packages))
	for _, entry := range manifest.Packages {
		if entry.Path == "" || entry.Stability == "" {
			return packageManifest{}, errors.New("package path and stability must be non-empty")
		}
		if seen[entry.Path] {
			return packageManifest{}, fmt.Errorf("duplicate package %q", entry.Path)
		}
		seen[entry.Path] = true
	}
	return manifest, nil
}

func manifestPackagesByPath(entries []manifestPackage) map[string]manifestPackage {
	result := make(map[string]manifestPackage, len(entries))
	for _, entry := range entries {
		result[entry.Path] = entry
	}
	return result
}

func readSnapshot(path string) (*types.Package, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	packagePath, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read package path: %w", err)
	}
	packagePath = strings.TrimSuffix(packagePath, "\n")
	if packagePath == "" {
		return nil, errors.New("snapshot has empty package path")
	}
	result, err := gcexportdata.Read(reader, token.NewFileSet(), map[string]*types.Package{}, packagePath)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func snapshotFilename(path string) string {
	return strings.ReplaceAll(path, "/", "-") + ".apidiff"
}

type versionManifest struct {
	FormatVersion int                        `json:"format_version"`
	SpecVersion   string                     `json:"spec_version"`
	Constants     map[string]json.RawMessage `json:"constants"`
	WireFields    []wireField                `json:"wire_fields"`
	Cases         []behaviorCase             `json:"cases"`
	Properties    map[string]bool            `json:"properties"`
}

type wireField struct {
	Name          string `json:"name"`
	Required      bool   `json:"required"`
	Nullable      bool   `json:"nullable"`
	OmitWhenEmpty bool   `json:"omit_when_empty"`
}

type behaviorCase struct {
	ID                       string          `json:"id"`
	Input                    string          `json:"input"`
	Valid                    bool            `json:"valid"`
	IsInvalidEnvelope        bool            `json:"is_invalid_envelope"`
	IsUnsupportedSpecVersion bool            `json:"is_unsupported_spec_version"`
	Wire                     json.RawMessage `json:"wire,omitempty"`
	WireBytes                int             `json:"wire_bytes,omitempty"`
	WireSHA256               string          `json:"wire_sha256,omitempty"`
}

// CompareBehavior compares all immutable version manifests under two api directories.
func CompareBehavior(baselineDir, candidateDir string) ([]Change, error) {
	oldFiles, err := filepath.Glob(filepath.Join(baselineDir, "behavior", "envelope-v*.json"))
	if err != nil {
		return nil, err
	}
	newFiles, err := filepath.Glob(filepath.Join(candidateDir, "behavior", "envelope-v*.json"))
	if err != nil {
		return nil, err
	}
	if len(oldFiles) == 0 {
		return nil, errors.New("baseline has no version behavior manifests")
	}
	oldByName := filesByBase(oldFiles)
	newByName := filesByBase(newFiles)
	var changes []Change
	for name, oldPath := range oldByName {
		newPath, ok := newByName[name]
		if !ok {
			changes = append(changes, Change{Layer: "behavior", Message: name + " removed"})
			continue
		}
		manifestChanges, err := compareBehaviorFiles(oldPath, newPath)
		if err != nil {
			return nil, fmt.Errorf("compare %s: %w", name, err)
		}
		changes = append(changes, manifestChanges...)
	}
	for name := range newByName {
		if _, ok := oldByName[name]; !ok {
			changes = append(changes, Change{Layer: "behavior", Message: name + " added", Compatible: true})
		}
	}
	currentChanged, err := jsonFilesDiffer(
		filepath.Join(baselineDir, "behavior", "envelope-current.json"),
		filepath.Join(candidateDir, "behavior", "envelope-current.json"),
	)
	if err != nil {
		return nil, fmt.Errorf("compare envelope-current.json: %w", err)
	}
	if currentChanged {
		changes = append(changes, Change{Layer: "behavior", Message: "envelope-current.json changed", Compatible: true})
	}
	sortChanges(changes)
	return changes, nil
}

func compareBehaviorFiles(baselinePath, candidatePath string) ([]Change, error) {
	baseline, err := readVersionManifest(baselinePath)
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	candidate, err := readVersionManifest(candidatePath)
	if err != nil {
		return nil, fmt.Errorf("read candidate: %w", err)
	}
	return compareVersionManifests(baseline, candidate), nil
}

func readVersionManifest(path string) (versionManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return versionManifest{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest versionManifest
	if err := decoder.Decode(&manifest); err != nil {
		return versionManifest{}, err
	}
	if manifest.FormatVersion <= 0 || manifest.SpecVersion == "" {
		return versionManifest{}, errors.New("format_version and spec_version are required")
	}
	if err := validateVersionManifest(manifest); err != nil {
		return versionManifest{}, err
	}
	return manifest, nil
}

func validateVersionManifest(manifest versionManifest) error {
	wireNames := make(map[string]bool, len(manifest.WireFields))
	for _, field := range manifest.WireFields {
		if field.Name == "" {
			return errors.New("wire field name is empty")
		}
		if wireNames[field.Name] {
			return fmt.Errorf("duplicate wire field %q", field.Name)
		}
		wireNames[field.Name] = true
	}
	caseIDs := make(map[string]bool, len(manifest.Cases))
	for _, testCase := range manifest.Cases {
		if testCase.ID == "" {
			return errors.New("behavior case ID is empty")
		}
		if caseIDs[testCase.ID] {
			return fmt.Errorf("duplicate behavior case %q", testCase.ID)
		}
		caseIDs[testCase.ID] = true
	}
	return nil
}

func compareVersionManifests(old, new versionManifest) []Change {
	var changes []Change
	add := func(message string, compatible bool) {
		changes = append(changes, Change{Layer: "behavior", Message: message, Compatible: compatible})
	}
	if old.FormatVersion != new.FormatVersion {
		add(fmt.Sprintf("manifest format changed from %d to %d", old.FormatVersion, new.FormatVersion), false)
	}
	if old.SpecVersion != new.SpecVersion {
		add(fmt.Sprintf("spec version changed from %q to %q", old.SpecVersion, new.SpecVersion), false)
	}
	compareConstants(old.Constants, new.Constants, add)
	compareWireFields(old.WireFields, new.WireFields, add)
	compareCases(old.Cases, new.Cases, add)
	compareProperties(old.Properties, new.Properties, add)
	sortChanges(changes)
	return changes
}

func compareConstants(old, new map[string]json.RawMessage, add func(string, bool)) {
	for name, oldValue := range old {
		newValue, ok := new[name]
		if !ok {
			add("constant "+name+" removed", false)
			continue
		}
		if !jsonEqual(oldValue, newValue) {
			add(fmt.Sprintf("constant %s changed from %s to %s", name, oldValue, newValue), false)
		}
	}
	for name := range new {
		if _, ok := old[name]; !ok {
			add("constant "+name+" added", true)
		}
	}
}

func compareWireFields(old, new []wireField, add func(string, bool)) {
	oldByName := wireFieldsByName(old)
	newByName := wireFieldsByName(new)
	for name, oldField := range oldByName {
		newField, ok := newByName[name]
		if !ok {
			add("wire field "+name+" removed", false)
			continue
		}
		if oldField != newField {
			add("wire field "+name+" semantics changed", false)
		}
	}
	for name := range newByName {
		if _, ok := oldByName[name]; !ok {
			add("wire field "+name+" added", true)
		}
	}
}

func compareCases(old, new []behaviorCase, add func(string, bool)) {
	oldByID := casesByID(old)
	newByID := casesByID(new)
	for id, oldCase := range oldByID {
		newCase, ok := newByID[id]
		if !ok {
			add("case "+id+" removed", false)
			continue
		}
		if oldCase.Input != newCase.Input {
			add("case "+id+" input changed", false)
		}
		if oldCase.Valid != newCase.Valid {
			add("case "+id+" validity changed", !oldCase.Valid && newCase.Valid)
		}
		if oldCase.IsInvalidEnvelope != newCase.IsInvalidEnvelope {
			add("case "+id+" ErrInvalidEnvelope classification changed", !oldCase.IsInvalidEnvelope && newCase.IsInvalidEnvelope)
		}
		if oldCase.IsUnsupportedSpecVersion != newCase.IsUnsupportedSpecVersion {
			add("case "+id+" ErrUnsupportedSpecVersion classification changed", !oldCase.IsUnsupportedSpecVersion && newCase.IsUnsupportedSpecVersion)
		}
		if oldCase.Valid && newCase.Valid && !sameWireEvidence(oldCase, newCase) {
			add("case "+id+" canonical wire changed", false)
		}
	}
	for id := range newByID {
		if _, ok := oldByID[id]; !ok {
			add("case "+id+" added", true)
		}
	}
}

func compareProperties(old, new map[string]bool, add func(string, bool)) {
	for name, oldValue := range old {
		newValue, ok := new[name]
		if !ok {
			add("property "+name+" removed", false)
			continue
		}
		if oldValue != newValue {
			add("property "+name+" changed", !oldValue && newValue)
		}
	}
	for name := range new {
		if _, ok := old[name]; !ok {
			add("property "+name+" added", true)
		}
	}
}

func sameWireEvidence(old, new behaviorCase) bool {
	if len(old.Wire) > 0 || len(new.Wire) > 0 {
		return jsonEqual(old.Wire, new.Wire)
	}
	return old.WireBytes == new.WireBytes && old.WireSHA256 == new.WireSHA256
}

func jsonEqual(left, right []byte) bool {
	var leftValue any
	var rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return bytes.Equal(bytes.TrimSpace(left), bytes.TrimSpace(right))
	}
	leftJSON, _ := json.Marshal(leftValue)
	rightJSON, _ := json.Marshal(rightValue)
	return bytes.Equal(leftJSON, rightJSON)
}

func jsonFilesDiffer(leftPath, rightPath string) (bool, error) {
	left, err := os.ReadFile(leftPath)
	if err != nil {
		return false, err
	}
	right, err := os.ReadFile(rightPath)
	if err != nil {
		return false, err
	}
	return !jsonEqual(left, right), nil
}

func wireFieldsByName(fields []wireField) map[string]wireField {
	result := make(map[string]wireField, len(fields))
	for _, field := range fields {
		result[field.Name] = field
	}
	return result
}

func casesByID(cases []behaviorCase) map[string]behaviorCase {
	result := make(map[string]behaviorCase, len(cases))
	for _, testCase := range cases {
		result[testCase.ID] = testCase
	}
	return result
}

func filesByBase(paths []string) map[string]string {
	result := make(map[string]string, len(paths))
	for _, path := range paths {
		result[filepath.Base(path)] = path
	}
	return result
}

func sortChanges(changes []Change) {
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Layer != changes[j].Layer {
			return changes[i].Layer < changes[j].Layer
		}
		if changes[i].Compatible != changes[j].Compatible {
			return !changes[i].Compatible
		}
		return changes[i].Message < changes[j].Message
	})
}

// WriteReport writes a deterministic, review-friendly report.
func WriteReport(w io.Writer, changes []Change) error {
	for _, change := range changes {
		classification := "incompatible"
		if change.Compatible {
			classification = "compatible"
		}
		if _, err := fmt.Fprintf(w, "[%s] %s: %s\n", classification, change.Layer, change.Message); err != nil {
			return err
		}
	}
	return nil
}
