package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"
)

const modulePath = "go.idp.xyz/idp-bento-go"

type packageManifest struct {
	FormatVersion int               `json:"format_version"`
	Packages      []manifestPackage `json:"packages"`
}

type manifestPackage struct {
	Path      string `json:"path"`
	Stability string `json:"stability"`
}

func main() {
	update := flag.Bool("update", false, "replace API snapshots that differ")
	requireAll := flag.Bool("require-all", false, "fail if a manifest package has no source directory")
	manifestPath := flag.String("manifest", "api/packages.json", "public package manifest")
	outputDir := flag.String("output", "api/snapshots", "snapshot directory")
	flag.Parse()

	if err := run(*manifestPath, *outputDir, *update, *requireAll, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "apisnapshot:", err)
		os.Exit(1)
	}
}

func run(manifestPath, outputDir string, update, requireAll bool, stdout io.Writer) error {
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	var implemented []manifestPackage
	var skipped []string
	for _, entry := range manifest.Packages {
		exists, err := packageDirectoryExists(entry.Path)
		if err != nil {
			return err
		}
		if !exists {
			skipped = append(skipped, entry.Path)
			continue
		}
		implemented = append(implemented, entry)
	}
	if requireAll && len(skipped) > 0 {
		return fmt.Errorf("manifest packages have no source: %s", strings.Join(skipped, ", "))
	}

	expected := make(map[string]bool)
	var changed []string
	for _, entry := range implemented {
		name := snapshotFilename(entry.Path)
		expected[name] = true
		data, err := loadAndEncode(entry.Path)
		if err != nil {
			return err
		}
		different, err := compareOrUpdate(filepath.Join(outputDir, name), data, update)
		if err != nil {
			return err
		}
		if different {
			changed = append(changed, entry.Path)
		}
	}

	stale, err := staleSnapshots(outputDir, expected)
	if err != nil {
		return err
	}
	if len(stale) > 0 {
		return fmt.Errorf("snapshots have no implemented package in %s: %s", manifestPath, strings.Join(stale, ", "))
	}

	if len(skipped) > 0 {
		fmt.Fprintf(stdout, "apisnapshot: skipped packages without source: %s\n", strings.Join(skipped, ", "))
	}
	if len(changed) == 0 {
		fmt.Fprintln(stdout, "apisnapshot: snapshots are clean")
		return nil
	}
	if update {
		fmt.Fprintf(stdout, "apisnapshot: updated: %s\n", strings.Join(changed, ", "))
		return nil
	}
	return fmt.Errorf("snapshots differ: %s; run with -update and review the API change", strings.Join(changed, ", "))
}

func readManifest(path string) (packageManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return packageManifest{}, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(bufio.NewReader(file))
	decoder.DisallowUnknownFields()
	var manifest packageManifest
	if err := decoder.Decode(&manifest); err != nil {
		return packageManifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.FormatVersion != 1 {
		return packageManifest{}, fmt.Errorf("unsupported manifest format version %d", manifest.FormatVersion)
	}
	if len(manifest.Packages) == 0 {
		return packageManifest{}, errors.New("manifest contains no packages")
	}
	seen := make(map[string]bool, len(manifest.Packages))
	for _, entry := range manifest.Packages {
		if !validPackagePath(entry.Path) {
			return packageManifest{}, fmt.Errorf("invalid package path %q", entry.Path)
		}
		if entry.Stability == "" {
			return packageManifest{}, fmt.Errorf("package %q has empty stability", entry.Path)
		}
		if seen[entry.Path] {
			return packageManifest{}, fmt.Errorf("duplicate package path %q", entry.Path)
		}
		seen[entry.Path] = true
	}
	return manifest, nil
}

func validPackagePath(path string) bool {
	if path == "" || path == "." || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.Contains(path, "\\") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func packageDirectoryExists(path string) (bool, error) {
	info, err := os.Stat(filepath.FromSlash(path))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat package %q: %w", path, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("package path %q is not a directory", path)
	}
	return true, nil
}

func loadAndEncode(path string) ([]byte, error) {
	importPath := modulePath + "/" + path
	cfg := &packages.Config{Mode: packages.LoadTypes | packages.NeedName | packages.NeedTypes | packages.NeedImports | packages.NeedDeps}
	loaded, err := packages.Load(cfg, importPath)
	if err != nil {
		return nil, fmt.Errorf("load package %q: %w", path, err)
	}
	if len(loaded) != 1 {
		return nil, fmt.Errorf("load package %q: got %d packages", path, len(loaded))
	}
	if len(loaded[0].Errors) > 0 {
		return nil, fmt.Errorf("load package %q: %s", path, packageErrors(loaded[0].Errors))
	}

	var output bytes.Buffer
	fmt.Fprintln(&output, loaded[0].PkgPath)
	if err := writeExportData(&output, loaded[0].Types); err != nil {
		return nil, fmt.Errorf("encode package %q: %w", path, err)
	}
	return output.Bytes(), nil
}

func writeExportData(output io.Writer, pkg *types.Package) error {
	// Positions are irrelevant to API compatibility and embed checkout and toolchain paths.
	return gcexportdata.Write(output, token.NewFileSet(), pkg)
}

func compareOrUpdate(destination string, data []byte, update bool) (bool, error) {
	temporary, err := os.CreateTemp(filepath.Dir(destination), filepath.Base(destination)+".tmp-")
	if err != nil {
		return false, fmt.Errorf("create temporary snapshot: %w", err)
	}
	temporaryPath := temporary.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(temporaryPath)
		}
	}()

	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return false, fmt.Errorf("write temporary snapshot: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return false, fmt.Errorf("close temporary snapshot: %w", err)
	}

	current, err := os.ReadFile(destination)
	if err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read snapshot %q: %w", destination, err)
	}
	if !update {
		return true, nil
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return false, fmt.Errorf("replace snapshot %q: %w", destination, err)
	}
	renamed = true
	return true, nil
}

func staleSnapshots(outputDir string, expected map[string]bool) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(outputDir, "*.apidiff"))
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	var stale []string
	for _, match := range matches {
		name := filepath.Base(match)
		if !expected[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	return stale, nil
}

func snapshotFilename(path string) string {
	return strings.ReplaceAll(path, "/", "-") + ".apidiff"
}

func packageErrors(packageErrors []packages.Error) string {
	messages := make([]string, 0, len(packageErrors))
	for _, packageError := range packageErrors {
		messages = append(messages, packageError.Error())
	}
	return strings.Join(messages, "; ")
}
