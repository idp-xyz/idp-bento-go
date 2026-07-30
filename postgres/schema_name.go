package postgres

import (
	"errors"
	"strings"
)

func validateSchema(schema string) error {
	if len(schema) == 0 || len(schema) > 63 {
		return errors.New("must contain 1 to 63 ASCII bytes")
	}
	if schema == "public" || schema == "information_schema" {
		return errors.New("must be a dedicated framework schema")
	}
	if strings.HasPrefix(schema, "pg_") {
		return errors.New("must not use the pg_ prefix")
	}
	for index, character := range []byte(schema) {
		valid := character == '_' || character >= 'a' && character <= 'z'
		if index > 0 {
			valid = valid || character >= '0' && character <= '9'
		}
		if !valid {
			return errors.New("must match ^[a-z_][a-z0-9_]{0,62}$")
		}
	}
	return nil
}

func quoteIdentifier(identifier string) string {
	return `"` + identifier + `"`
}
