package postgres

import (
	"errors"
	"strings"
	"testing"
)

func TestSchemaValidation(t *testing.T) {
	valid := []string{"bento", "customer_bento", "_" + strings.Repeat("a", 62)}
	for _, schema := range valid {
		if err := validateSchema(schema); err != nil {
			t.Errorf("validateSchema(%q): %v", schema, err)
		}
	}
	invalid := []string{"", "1bento", "Bento", "bento-name", "中文", strings.Repeat("a", 64), "pg_bento", "public", "information_schema", " bento"}
	for _, schema := range invalid {
		if err := validateSchema(schema); err == nil {
			t.Errorf("validateSchema(%q) returned nil", schema)
		}
	}
}

func TestMigrationsAndRenderMigration(t *testing.T) {
	first := Migrations()
	second := Migrations()
	if len(first) != 1 || first[0].ID != "000001_eventing_store" {
		t.Fatalf("Migrations = %#v", first)
	}
	first[0].ID = "mutated"
	if second[0].ID != "000001_eventing_store" || Migrations()[0].ID != "000001_eventing_store" {
		t.Fatal("Migrations did not return defensive copies")
	}
	sql, err := RenderMigration("000001_eventing_store", "customer_bento")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, schemaToken) || strings.Contains(sql, "bento.outbox") {
		t.Fatal("rendered migration contains an unresolved or unquoted schema")
	}
	for _, expected := range []string{
		"CREATE TABLE \"customer_bento\".outbox",
		"CREATE TABLE \"customer_bento\".outbox_partition",
		"CREATE TABLE \"customer_bento\".inbox",
		"SCHEMA = TG_TABLE_SCHEMA",
	} {
		if !strings.Contains(sql, expected) {
			t.Errorf("rendered migration does not contain %q", expected)
		}
	}
	if !strings.HasSuffix(sql, "\n") || strings.HasSuffix(sql, "\n\n") {
		t.Fatal("rendered migration does not have exactly one trailing newline")
	}
	if _, err := RenderMigration("missing", DefaultSchema); !errors.Is(err, ErrMigrationNotFound) {
		t.Fatalf("missing migration error = %v", err)
	}
	if rendered, err := RenderMigration("000001_eventing_store", "public"); rendered != "" || !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid schema = %q, %v", rendered, err)
	}
}

func TestMigrationNormalization(t *testing.T) {
	inputs := [][]byte{
		[]byte("SELECT 1;\n"),
		[]byte("SELECT 1;\r\n\r\n"),
		[]byte("SELECT 1;\r"),
		append([]byte{0xef, 0xbb, 0xbf}, []byte("SELECT 1;\n")...),
	}
	var checksum string
	for _, input := range inputs {
		normalized, err := normalizeMigration(input)
		if err != nil {
			t.Fatal(err)
		}
		current := checksumMigration(normalized)
		if checksum == "" {
			checksum = current
		} else if current != checksum {
			t.Fatalf("checksum = %s, want %s", current, checksum)
		}
	}
	if _, err := normalizeMigration([]byte{0xff}); err == nil {
		t.Fatal("invalid UTF-8 returned nil error")
	}
}
