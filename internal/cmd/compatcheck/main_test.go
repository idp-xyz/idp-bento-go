package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunAcceptsUnchangedSeparateBaseline(t *testing.T) {
	t.Parallel()
	candidate := filepath.Join(repositoryRoot(t), "api")
	baseline := filepath.Join(t.TempDir(), "api")
	copyDirectory(t, candidate, baseline)
	var output bytes.Buffer
	if err := run([]string{"-baseline", baseline, "-candidate", candidate}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "compatible and unchanged") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunRequiresImmutableSeparateBaseline(t *testing.T) {
	t.Parallel()
	if err := run(nil, io.Discard); err == nil || !strings.Contains(err.Error(), "-baseline is required") {
		t.Fatalf("missing baseline error = %v", err)
	}
	apiDir := filepath.Join(repositoryRoot(t), "api")
	if err := run([]string{"-baseline", apiDir, "-candidate", apiDir}, io.Discard); err == nil || !strings.Contains(err.Error(), "different directories") {
		t.Fatalf("same directory error = %v", err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine repository root")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "api")); err != nil {
		t.Fatal(err)
	}
	return root
}

func copyDirectory(t *testing.T, source, destination string) {
	t.Helper()
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
}
