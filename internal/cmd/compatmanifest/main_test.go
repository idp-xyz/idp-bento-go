package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVersionManifestIsDeterministicAndComplete(t *testing.T) {
	first := buildVersionManifest()
	second := buildVersionManifest()
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatal("consecutive version manifests differ")
	}
	if first.FormatVersion != 2 {
		t.Fatalf("format version = %d, want 2", first.FormatVersion)
	}
	for property, satisfied := range first.Properties {
		if !satisfied {
			t.Errorf("property %q is false", property)
		}
	}
	for id, wantValid := range map[string]bool{
		"valid":                              true,
		"unknown_spec_version":               false,
		"source_multibyte_exact_byte_limit":  true,
		"source_multibyte_over_byte_limit":   false,
		"traceparent_semantics_not_enforced": true,
		"payload_exact_byte_limit":           true,
		"payload_over_byte_limit":            false,
		"unmarshal_missing_source":           false,
		"unmarshal_null_correlation_id":      true,
		"unmarshal_unknown_field":            true,
		"unmarshal_unknown_spec_version":     false,
	} {
		testCase, found := behaviorCaseByID(first.Cases, id)
		if !found {
			t.Errorf("case %q is missing", id)
			continue
		}
		if testCase.Valid != wantValid {
			t.Errorf("case %q valid = %t, want %t", id, testCase.Valid, wantValid)
		}
	}
	exactPayload, found := behaviorCaseByID(first.Cases, "payload_exact_byte_limit")
	if !found || exactPayload.WireBytes == 0 || exactPayload.WireSHA256 == "" || len(exactPayload.Wire) != 0 {
		t.Errorf("large payload wire evidence = %#v", exactPayload)
	}
}

func TestCompareOrUpdate(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(destination, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	different, err := compareOrUpdate(destination, []byte("new"), false)
	if err != nil || !different {
		t.Fatalf("check = %t, %v", different, err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "old" {
		t.Fatalf("check changed destination: %q, %v", got, err)
	}
	different, err = compareOrUpdate(destination, []byte("new"), true)
	if err != nil || !different {
		t.Fatalf("update = %t, %v", different, err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "new" {
		t.Fatalf("update content = %q, %v", got, err)
	}
}

func behaviorCaseByID(cases []behaviorCase, id string) (behaviorCase, bool) {
	for _, testCase := range cases {
		if testCase.ID == id {
			return testCase, true
		}
	}
	return behaviorCase{}, false
}
