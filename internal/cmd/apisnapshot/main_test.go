package main

import (
	"bufio"
	"bytes"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/gcexportdata"
)

func TestValidPackagePath(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		path string
		want bool
	}{
		{path: "domain", want: true},
		{path: "postgres/outbox", want: true},
		{path: "", want: false},
		{path: ".", want: false},
		{path: "../domain", want: false},
		{path: "/domain", want: false},
		{path: "domain/", want: false},
		{path: `postgres\outbox`, want: false},
	} {
		test := test
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()
			if got := validPackagePath(test.path); got != test.want {
				t.Fatalf("validPackagePath(%q) = %t, want %t", test.path, got, test.want)
			}
		})
	}
}

func TestCompareOrUpdate(t *testing.T) {
	t.Parallel()
	destination := filepath.Join(t.TempDir(), "domain.apidiff")
	if err := os.WriteFile(destination, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	different, err := compareOrUpdate(destination, []byte("new"), false)
	if err != nil || !different {
		t.Fatalf("check = %t, %v", different, err)
	}
	assertFileContent(t, destination, "old")

	different, err = compareOrUpdate(destination, []byte("new"), true)
	if err != nil || !different {
		t.Fatalf("update = %t, %v", different, err)
	}
	assertFileContent(t, destination, "new")

	different, err = compareOrUpdate(destination, []byte("new"), false)
	if err != nil || different {
		t.Fatalf("clean check = %t, %v", different, err)
	}
}

func TestLoadAndEncodeIsDeterministicPathFreeExportData(t *testing.T) {
	first, err := loadAndEncode("domain")
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadAndEncode("domain")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("consecutive snapshots differ")
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(first, []byte(workingDirectory)) {
		t.Fatalf("snapshot contains checkout path %q", workingDirectory)
	}

	reader := bufio.NewReader(bytes.NewReader(first))
	packagePath, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	packagePath = strings.TrimSuffix(packagePath, "\n")
	decoded, err := gcexportdata.Read(reader, token.NewFileSet(), map[string]*types.Package{}, packagePath)
	if err != nil {
		t.Fatalf("read export data: %v", err)
	}
	if decoded.Scope().Lookup("Event") == nil {
		t.Fatal("decoded snapshot does not contain domain.Event")
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}
