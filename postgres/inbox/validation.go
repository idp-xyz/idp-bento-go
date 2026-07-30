package inbox

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.idp.xyz/idp-bento-go/eventing"
)

func validateKey(key eventing.InboxKey) error {
	if err := validateText("consumer", string(key.Consumer), eventing.MaxInboxConsumerLength); err != nil {
		return err
	}
	if err := validateText("source", key.Source, eventing.MaxSourceLength); err != nil {
		return err
	}
	return validateText("event ID", string(key.EventID), eventing.MaxEventIDLength)
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
	return fmt.Errorf("%w: inbox: %s", eventing.ErrInvalidRequest, fmt.Sprintf(format, args...))
}

func invalidTransition(format string, args ...any) error {
	return fmt.Errorf("%w: inbox: %s", eventing.ErrInvalidTransition, fmt.Sprintf(format, args...))
}

func normalizeTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}
