package postgres

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

const schemaToken = "{{IDP_BENTO_SCHEMA}}"

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Migration struct {
	ID                  string
	Checksum            string
	MinFrameworkVersion string
}

type migrationAsset struct {
	migration Migration
	path      string
}

var migrationAssets = []migrationAsset{
	{
		migration: Migration{
			ID:                  "000001_eventing_store",
			Checksum:            "sha256:71eaecb1597bf0b2048ad80328dd03dea9aa8a8cc4f8eda571907bc9f35b4397",
			MinFrameworkVersion: "v0.1.0",
		},
		path: "migrations/000001_eventing_store.sql",
	},
}

func Migrations() []Migration {
	result := make([]Migration, len(migrationAssets))
	for index, asset := range migrationAssets {
		result[index] = asset.migration
	}
	return result
}

func RenderMigration(migrationID string, schema string) (string, error) {
	if err := validateSchema(schema); err != nil {
		return "", invalidConfig("schema %q: %v", schema, err)
	}
	var selected *migrationAsset
	for index := range migrationAssets {
		if migrationAssets[index].migration.ID == migrationID {
			selected = &migrationAssets[index]
			break
		}
	}
	if selected == nil {
		return "", fmt.Errorf("%w: %q", ErrMigrationNotFound, migrationID)
	}
	data, err := migrationFiles.ReadFile(selected.path)
	if err != nil {
		return "", migrationIntegrity(migrationID, "read asset: %v", err)
	}
	normalized, err := normalizeMigration(data)
	if err != nil {
		return "", migrationIntegrity(migrationID, "%v", err)
	}
	if checksumMigration(normalized) != selected.migration.Checksum {
		return "", migrationIntegrity(migrationID, "checksum mismatch")
	}
	if strings.Count(normalized, schemaToken) == 0 {
		return "", migrationIntegrity(migrationID, "schema token is missing")
	}
	withoutKnownToken := strings.ReplaceAll(normalized, schemaToken, "")
	if strings.Contains(withoutKnownToken, "{{IDP_BENTO_") {
		return "", migrationIntegrity(migrationID, "unknown framework token")
	}
	rendered := strings.ReplaceAll(normalized, schemaToken, quoteIdentifier(schema))
	if strings.Contains(rendered, "{{IDP_BENTO_") {
		return "", migrationIntegrity(migrationID, "unresolved framework token")
	}
	return rendered, nil
}

func normalizeMigration(data []byte) (string, error) {
	if !utf8.Valid(data) {
		return "", fmt.Errorf("migration is not valid UTF-8")
	}
	text := string(data)
	if strings.HasPrefix(text, "\ufeff") {
		text = strings.TrimPrefix(text, "\ufeff")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimRight(text, "\n") + "\n"
	return text, nil
}

func checksumMigration(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func migrationIntegrity(migrationID, format string, args ...any) error {
	detail := fmt.Sprintf(format, args...)
	return fmt.Errorf("%w: %s: %s", ErrMigrationIntegrity, migrationID, detail)
}
