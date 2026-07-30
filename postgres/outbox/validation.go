package outbox

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.idp.xyz/idp-bento-go/eventing"
)

func validateClaim(claim eventing.OutboxClaim) error {
	if claim.Now.IsZero() {
		return invalidRequest("claim time is zero")
	}
	if claim.Limit < 1 || claim.Limit > eventing.MaxOutboxClaimLimit {
		return invalidRequest("claim limit must be between 1 and %d", eventing.MaxOutboxClaimLimit)
	}
	if claim.LeaseFor <= 0 {
		return invalidRequest("claim lease duration must be positive")
	}
	if claim.MaxAttempts == 0 {
		return invalidRequest("claim max attempts must be positive")
	}
	return nil
}

func validateDeliveryRef(ref eventing.DeliveryRef) error {
	if err := validateText("source", ref.Source, eventing.MaxSourceLength); err != nil {
		return err
	}
	if err := validateText("event ID", string(ref.EventID), eventing.MaxEventIDLength); err != nil {
		return err
	}
	if !validUUID(ref.LeaseToken) {
		return invalidRequest("lease token is not a UUID")
	}
	return nil
}

func validateFailureCode(code eventing.FailureCode) error {
	value := string(code)
	if len(value) == 0 || len(value) > eventing.MaxFailureCodeLength {
		return invalidRequest("failure code must contain 1 to %d bytes", eventing.MaxFailureCodeLength)
	}
	for index, char := range []byte(value) {
		valid := char >= 'a' && char <= 'z' || char >= '0' && char <= '9'
		if index > 0 {
			valid = valid || char == '.' || char == '_' || char == '-'
		}
		if !valid {
			return invalidRequest("failure code has an invalid format")
		}
	}
	return nil
}

func validateText(name, value string, max int) error {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || len(value) > max {
		return invalidRequest("%s is invalid", name)
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return invalidRequest("%s contains a control character", name)
		}
	}
	return nil
}

func invalidRequest(format string, args ...any) error {
	return fmt.Errorf("%w: outbox: %s", eventing.ErrInvalidRequest, fmt.Sprintf(format, args...))
}

func leaseLost(format string, args ...any) error {
	return fmt.Errorf("%w: outbox: %s", eventing.ErrLeaseLost, fmt.Sprintf(format, args...))
}

func consistencyError(format string, args ...any) error {
	return fmt.Errorf("outbox: consistency failure: %s", fmt.Sprintf(format, args...))
}

func normalizeTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func newLeaseToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("outbox: generate lease token: %w", err)
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return string(encoded), nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, char := range []byte(value) {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}
