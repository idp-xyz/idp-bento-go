package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"go.idp.xyz/idp-bento-go/eventing"
	"go.idp.xyz/idp-bento-go/testkit"
)

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

type wireField struct {
	Name          string `json:"name"`
	Required      bool   `json:"required"`
	Nullable      bool   `json:"nullable"`
	OmitWhenEmpty bool   `json:"omit_when_empty"`
}

type versionManifest struct {
	FormatVersion int             `json:"format_version"`
	SpecVersion   string          `json:"spec_version"`
	Constants     map[string]any  `json:"constants"`
	WireFields    []wireField     `json:"wire_fields"`
	Cases         []behaviorCase  `json:"cases"`
	Properties    map[string]bool `json:"properties"`
}

type currentManifest struct {
	FormatVersion      int    `json:"format_version"`
	CurrentSpecVersion string `json:"current_spec_version"`
	VersionManifest    string `json:"version_manifest"`
	VersionManifestSHA string `json:"version_manifest_sha256"`
	ValidEnvelopeValid bool   `json:"valid_envelope_valid"`
	PayloadIndependent bool   `json:"payload_independent"`
}

func main() {
	mode := flag.String("mode", "version", "version or current")
	out := flag.String("out", "", "output file")
	source := flag.String("source", "", "version manifest for current mode")
	update := flag.Bool("update", false, "replace the behavior manifest if it differs")
	flag.Parse()
	if *out == "" {
		fail("-out is required")
	}
	var value any
	switch *mode {
	case "version":
		value = buildVersionManifest()
	case "current":
		if *source == "" {
			fail("-source is required for current mode")
		}
		value = buildCurrentManifest(*source)
	default:
		fail("unsupported mode %q", *mode)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fail("marshal manifest: %v", err)
	}
	data = append(data, '\n')
	different, err := compareOrUpdate(*out, data, *update)
	if err != nil {
		fail("write manifest: %v", err)
	}
	if !different {
		fmt.Printf("compatmanifest: %s is clean\n", *out)
		return
	}
	if *update {
		fmt.Printf("compatmanifest: updated %s\n", *out)
		return
	}
	fail("%s differs; run with -update and review the behavior change", *out)
}

func buildVersionManifest() versionManifest {
	base := testkit.ValidEnvelope()
	cases := []behaviorCase{
		evaluate("valid", "testkit.ValidEnvelope()", base),
		evaluateWith("empty_spec_version", "SpecVersion is empty", base, func(e *eventing.Envelope) { e.SpecVersion = "" }),
		evaluateWith("unknown_spec_version", "SpecVersion is unsupported", base, func(e *eventing.Envelope) { e.SpecVersion = "unknown" }),
		evaluateWith("zero_version", "Version is zero", base, func(e *eventing.Envelope) { e.Version = 0 }),
		evaluateWith("max_version", "Version is max uint32", base, func(e *eventing.Envelope) { e.Version = ^eventing.EventVersion(0) }),
		evaluateWith("zero_occurred_at", "OccurredAt is zero", base, func(e *eventing.Envelope) { e.OccurredAt = time.Time{} }),
		evaluateWith("zero_recorded_at", "RecordedAt is zero", base, func(e *eventing.Envelope) { e.RecordedAt = time.Time{} }),
		evaluateWith("empty_content_type", "ContentType is empty", base, func(e *eventing.Envelope) { e.ContentType = "" }),
		evaluateWith("wrong_content_type", "ContentType is not JSONContentType", base, func(e *eventing.Envelope) { e.ContentType = "text/plain" }),
		evaluateWith("content_type_control_character", "ContentType contains a control character", base, func(e *eventing.Envelope) { e.ContentType = "application/json\n" }),
		evaluateWith("content_type_invalid_utf8", "ContentType contains invalid UTF-8", base, func(e *eventing.Envelope) { e.ContentType = string([]byte{0xff}) }),
		evaluateWith("empty_payload", "Payload is nil", base, func(e *eventing.Envelope) { e.Payload = nil }),
		evaluateWith("empty_object_payload", "Payload is an empty JSON object", base, func(e *eventing.Envelope) { e.Payload = []byte(`{}`) }),
		evaluateWith("array_payload", "Payload is a JSON array", base, func(e *eventing.Envelope) { e.Payload = []byte(`[]`) }),
		evaluateWith("scalar_payload", "Payload is a JSON number", base, func(e *eventing.Envelope) { e.Payload = []byte(`1`) }),
		evaluateWith("null_payload", "Payload is JSON null", base, func(e *eventing.Envelope) { e.Payload = []byte(`null`) }),
		evaluateWith("invalid_json_payload", "Payload is malformed JSON", base, func(e *eventing.Envelope) { e.Payload = []byte(`{"value":`) }),
		evaluateWith("payload_exact_byte_limit", "Payload is exactly MaxPayloadBytes", base, func(e *eventing.Envelope) {
			e.Payload = payloadObject(eventing.MaxPayloadBytes)
		}),
		evaluateWith("payload_over_byte_limit", "Payload is MaxPayloadBytes plus one byte", base, func(e *eventing.Envelope) {
			e.Payload = payloadObject(eventing.MaxPayloadBytes + 1)
		}),
		evaluateWith("all_optional_fields", "All optional propagation fields are present", base, func(e *eventing.Envelope) {
			e.CorrelationID = "correlation-0001"
			e.CausationID = "causation-0001"
			e.TraceParent = "not-semantic-w3c-but-structurally-valid"
			e.TraceState = "vendor=value"
		}),
		evaluateWith("traceparent_semantics_not_enforced", "TraceParent is structurally valid but not parsed as W3C Trace Context", base, func(e *eventing.Envelope) {
			e.TraceParent = "not-a-w3c-traceparent"
		}),
	}
	for _, field := range envelopeStringFields() {
		cases = append(cases, stringBoundaryCases(base, field)...)
	}
	cases = append(cases, unmarshalCases(base)...)
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return versionManifest{
		FormatVersion: 2,
		SpecVersion:   eventing.SpecVersionV1,
		Constants: map[string]any{
			"SpecVersionV1":                   eventing.SpecVersionV1,
			"JSONContentType":                 eventing.JSONContentType,
			"FailureEnvelopeInvalidOnPublish": eventing.FailureEnvelopeInvalidOnPublish,
			"FailureClaimLimitExceeded":       eventing.FailureClaimLimitExceeded,
			"MaxEventIDLength":                eventing.MaxEventIDLength,
			"MaxSourceLength":                 eventing.MaxSourceLength,
			"MaxEventTypeLength":              eventing.MaxEventTypeLength,
			"MaxScopeLength":                  eventing.MaxScopeLength,
			"MaxSubjectLength":                eventing.MaxSubjectLength,
			"MaxPartitionKeyLength":           eventing.MaxPartitionKeyLength,
			"MaxCorrelationIDLength":          eventing.MaxCorrelationIDLength,
			"MaxCausationIDLength":            eventing.MaxCausationIDLength,
			"MaxTraceParentLength":            eventing.MaxTraceParentLength,
			"MaxTraceStateLength":             eventing.MaxTraceStateLength,
			"MaxFailureCodeLength":            eventing.MaxFailureCodeLength,
			"MaxInboxConsumerLength":          eventing.MaxInboxConsumerLength,
			"MaxPayloadBytes":                 eventing.MaxPayloadBytes,
			"MaxOutboxClaimLimit":             eventing.MaxOutboxClaimLimit,
		},
		WireFields: []wireField{
			{Name: "specversion", Required: true},
			{Name: "id", Required: true},
			{Name: "source", Required: true},
			{Name: "type", Required: true},
			{Name: "version", Required: true},
			{Name: "scope", Required: true},
			{Name: "subject", Required: true},
			{Name: "partition_key", Required: true},
			{Name: "occurred_at", Required: true},
			{Name: "recorded_at", Required: true},
			{Name: "correlation_id", Nullable: true, OmitWhenEmpty: true},
			{Name: "causation_id", Nullable: true, OmitWhenEmpty: true},
			{Name: "traceparent", Nullable: true, OmitWhenEmpty: true},
			{Name: "tracestate", Nullable: true, OmitWhenEmpty: true},
			{Name: "content_type", Required: true},
			{Name: "payload", Required: true},
		},
		Cases: cases,
		Properties: map[string]bool{
			"clone_payload_independent":  clonePayloadIndependent(base),
			"optional_fields_omitted":    optionalFieldsOmitted(base),
			"payload_is_embedded_json":   payloadIsEmbeddedJSON(base),
			"round_trip_preserves_value": roundTripPreservesValue(base),
			"times_are_utc_rfc3339nano":  timesAreUTCRFC3339Nano(base),
			"unknown_fields_accepted":    unknownFieldsAccepted(base),
		},
	}
}

func buildCurrentManifest(source string) currentManifest {
	data, err := os.ReadFile(source)
	if err != nil {
		fail("read version manifest: %v", err)
	}
	sum := sha256.Sum256(data)
	first := testkit.ValidEnvelope()
	second := testkit.ValidEnvelope()
	first.Payload[0] = '['
	return currentManifest{
		FormatVersion:      1,
		CurrentSpecVersion: eventing.SpecVersion,
		VersionManifest:    filepath.Base(source),
		VersionManifestSHA: hex.EncodeToString(sum[:]),
		ValidEnvelopeValid: second.Validate() == nil,
		PayloadIndependent: first.Payload[0] != second.Payload[0],
	}
}

type envelopeStringField struct {
	name     string
	maxBytes int
	required bool
	set      func(*eventing.Envelope, string)
}

func envelopeStringFields() []envelopeStringField {
	return []envelopeStringField{
		{name: "id", maxBytes: eventing.MaxEventIDLength, required: true, set: func(e *eventing.Envelope, value string) { e.ID = eventing.EventID(value) }},
		{name: "source", maxBytes: eventing.MaxSourceLength, required: true, set: func(e *eventing.Envelope, value string) { e.Source = value }},
		{name: "type", maxBytes: eventing.MaxEventTypeLength, required: true, set: func(e *eventing.Envelope, value string) { e.Type = eventing.EventType(value) }},
		{name: "scope", maxBytes: eventing.MaxScopeLength, required: true, set: func(e *eventing.Envelope, value string) { e.Scope = value }},
		{name: "subject", maxBytes: eventing.MaxSubjectLength, required: true, set: func(e *eventing.Envelope, value string) { e.Subject = value }},
		{name: "partition_key", maxBytes: eventing.MaxPartitionKeyLength, required: true, set: func(e *eventing.Envelope, value string) { e.PartitionKey = value }},
		{name: "correlation_id", maxBytes: eventing.MaxCorrelationIDLength, set: func(e *eventing.Envelope, value string) { e.CorrelationID = value }},
		{name: "causation_id", maxBytes: eventing.MaxCausationIDLength, set: func(e *eventing.Envelope, value string) { e.CausationID = value }},
		{name: "traceparent", maxBytes: eventing.MaxTraceParentLength, set: func(e *eventing.Envelope, value string) { e.TraceParent = value }},
		{name: "tracestate", maxBytes: eventing.MaxTraceStateLength, set: func(e *eventing.Envelope, value string) { e.TraceState = value }},
	}
}

func stringBoundaryCases(base eventing.Envelope, field envelopeStringField) []behaviorCase {
	var cases []behaviorCase
	if field.required {
		cases = append(cases, evaluateStringField(base, field, "empty", "", "value is empty"))
	}
	cases = append(cases,
		evaluateStringField(base, field, "whitespace", "　", "value becomes empty after trimming"),
		evaluateStringField(base, field, "control_character", "value\ninvalid", "value contains a control character"),
		evaluateStringField(base, field, "invalid_utf8", string([]byte{0xff}), "value contains invalid UTF-8"),
		evaluateStringField(base, field, "ascii_exact_byte_limit", strings.Repeat("a", field.maxBytes), fmt.Sprintf("ASCII value is exactly %d bytes", field.maxBytes)),
		evaluateStringField(base, field, "ascii_over_byte_limit", strings.Repeat("a", field.maxBytes+1), fmt.Sprintf("ASCII value is %d bytes", field.maxBytes+1)),
		evaluateStringField(base, field, "multibyte_exact_byte_limit", multibyteValue(field.maxBytes), fmt.Sprintf("multibyte value is exactly %d bytes", field.maxBytes)),
		evaluateStringField(base, field, "multibyte_over_byte_limit", multibyteValue(field.maxBytes)+"a", fmt.Sprintf("multibyte value is %d bytes", field.maxBytes+1)),
	)
	return cases
}

func evaluateStringField(base eventing.Envelope, field envelopeStringField, suffix, value, input string) behaviorCase {
	return evaluateWith(field.name+"_"+suffix, field.name+": "+input, base, func(e *eventing.Envelope) {
		field.set(e, value)
	})
}

func multibyteValue(size int) string {
	const unit = "界"
	count := size / len(unit)
	return strings.Repeat(unit, count) + strings.Repeat("a", size-count*len(unit))
}

func payloadObject(size int) json.RawMessage {
	const prefix = `{"value":"`
	const suffix = `"}`
	if size < len(prefix)+len(suffix) {
		panic("payloadObject size is too small")
	}
	return json.RawMessage(prefix + strings.Repeat("a", size-len(prefix)-len(suffix)) + suffix)
}

func unmarshalCases(base eventing.Envelope) []behaviorCase {
	withOptional := base.Clone()
	withOptional.CorrelationID = "correlation-0001"
	withOptional.CausationID = "causation-0001"
	withOptional.TraceParent = "not-a-w3c-traceparent"
	withOptional.TraceState = "vendor=value"
	wire, err := withOptional.MarshalJSON()
	if err != nil {
		fail("marshal unmarshal-case base: %v", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(wire, &object); err != nil {
		fail("decode unmarshal-case base: %v", err)
	}

	var cases []behaviorCase
	required := []string{
		"specversion", "id", "source", "type", "version", "scope", "subject",
		"partition_key", "occurred_at", "recorded_at", "content_type", "payload",
	}
	for _, field := range required {
		field := field
		cases = append(cases,
			evaluateUnmarshalMutation("unmarshal_missing_"+field, field+" is omitted", object, func(value map[string]json.RawMessage) {
				delete(value, field)
			}),
			evaluateUnmarshalMutation("unmarshal_null_"+field, field+" is JSON null", object, func(value map[string]json.RawMessage) {
				value[field] = json.RawMessage("null")
			}),
		)
	}
	for _, field := range []string{"correlation_id", "causation_id", "traceparent", "tracestate"} {
		field := field
		cases = append(cases,
			evaluateUnmarshalMutation("unmarshal_missing_"+field, field+" is omitted", object, func(value map[string]json.RawMessage) {
				delete(value, field)
			}),
			evaluateUnmarshalMutation("unmarshal_null_"+field, field+" is JSON null", object, func(value map[string]json.RawMessage) {
				value[field] = json.RawMessage("null")
			}),
		)
	}
	cases = append(cases,
		evaluateUnmarshalMutation("unmarshal_unknown_field", "an unknown future field is present", object, func(value map[string]json.RawMessage) {
			value["future_extension"] = json.RawMessage(`{"enabled":true}`)
		}),
		evaluateUnmarshalMutation("unmarshal_unknown_spec_version", "specversion is unsupported", object, func(value map[string]json.RawMessage) {
			value["specversion"] = json.RawMessage(`"unknown"`)
		}),
		evaluateUnmarshal("unmarshal_malformed_json", "input is malformed JSON", []byte(`{"specversion":`)),
	)
	return cases
}

func evaluateUnmarshalMutation(id, input string, base map[string]json.RawMessage, mutate func(map[string]json.RawMessage)) behaviorCase {
	value := make(map[string]json.RawMessage, len(base))
	for key, raw := range base {
		value[key] = append(json.RawMessage(nil), raw...)
	}
	mutate(value)
	data, err := json.Marshal(value)
	if err != nil {
		fail("marshal case %s: %v", id, err)
	}
	return evaluateUnmarshal(id, input, data)
}

func evaluateUnmarshal(id, input string, data []byte) behaviorCase {
	var envelope eventing.Envelope
	err := envelope.UnmarshalJSON(data)
	result := behaviorCase{
		ID:                       id,
		Input:                    input,
		Valid:                    err == nil,
		IsInvalidEnvelope:        errors.Is(err, eventing.ErrInvalidEnvelope),
		IsUnsupportedSpecVersion: errors.Is(err, eventing.ErrUnsupportedSpecVersion),
	}
	if err == nil {
		wire, marshalErr := envelope.MarshalJSON()
		if marshalErr != nil {
			fail("marshal valid unmarshal case %s: %v", id, marshalErr)
		}
		setWireEvidence(&result, wire)
	}
	return result
}

func evaluateWith(id, input string, base eventing.Envelope, mutate func(*eventing.Envelope)) behaviorCase {
	envelope := base.Clone()
	mutate(&envelope)
	return evaluate(id, input, envelope)
}

func evaluate(id, input string, envelope eventing.Envelope) behaviorCase {
	err := envelope.Validate()
	result := behaviorCase{
		ID:                       id,
		Input:                    input,
		Valid:                    err == nil,
		IsInvalidEnvelope:        errors.Is(err, eventing.ErrInvalidEnvelope),
		IsUnsupportedSpecVersion: errors.Is(err, eventing.ErrUnsupportedSpecVersion),
	}
	if err == nil {
		wire, marshalErr := envelope.MarshalJSON()
		if marshalErr != nil {
			fail("marshal valid case %s: %v", id, marshalErr)
		}
		setWireEvidence(&result, wire)
	}
	return result
}

func setWireEvidence(result *behaviorCase, wire []byte) {
	if len(wire) > 16<<10 {
		sum := sha256.Sum256(wire)
		result.WireBytes = len(wire)
		result.WireSHA256 = "sha256:" + hex.EncodeToString(sum[:])
		return
	}
	result.Wire = wire
}

func clonePayloadIndependent(envelope eventing.Envelope) bool {
	source := envelope.Clone()
	clone := source.Clone()
	source.Payload[0] = '['
	return clone.Payload[0] != source.Payload[0]
}

func payloadIsEmbeddedJSON(envelope eventing.Envelope) bool {
	wire, err := envelope.MarshalJSON()
	if err != nil {
		return false
	}
	var decoded map[string]json.RawMessage
	if json.Unmarshal(wire, &decoded) != nil {
		return false
	}
	return string(decoded["payload"]) == string(envelope.Payload)
}

func optionalFieldsOmitted(envelope eventing.Envelope) bool {
	wire, err := envelope.MarshalJSON()
	if err != nil {
		return false
	}
	var decoded map[string]json.RawMessage
	if json.Unmarshal(wire, &decoded) != nil {
		return false
	}
	for _, field := range []string{"correlation_id", "causation_id", "traceparent", "tracestate"} {
		if _, exists := decoded[field]; exists {
			return false
		}
	}
	return true
}

func roundTripPreservesValue(envelope eventing.Envelope) bool {
	source := envelope.Clone()
	source.OccurredAt = source.OccurredAt.UTC()
	source.RecordedAt = source.RecordedAt.UTC()
	wire, err := source.MarshalJSON()
	if err != nil {
		return false
	}
	var decoded eventing.Envelope
	if err := decoded.UnmarshalJSON(wire); err != nil {
		return false
	}
	return reflect.DeepEqual(decoded, source)
}

func timesAreUTCRFC3339Nano(envelope eventing.Envelope) bool {
	source := envelope.Clone()
	location := time.FixedZone("manifest-offset", 8*60*60)
	source.OccurredAt = time.Date(2026, 1, 2, 11, 4, 5, 123456789, location)
	source.RecordedAt = time.Date(2026, 1, 2, 11, 4, 6, 987654321, location)
	wire, err := source.MarshalJSON()
	if err != nil {
		return false
	}
	var decoded map[string]json.RawMessage
	if json.Unmarshal(wire, &decoded) != nil {
		return false
	}
	return rawJSONString(decoded["occurred_at"]) == source.OccurredAt.UTC().Format(time.RFC3339Nano) &&
		rawJSONString(decoded["recorded_at"]) == source.RecordedAt.UTC().Format(time.RFC3339Nano)
}

func rawJSONString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}

func unknownFieldsAccepted(envelope eventing.Envelope) bool {
	wire, err := envelope.MarshalJSON()
	if err != nil {
		return false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(wire, &object) != nil {
		return false
	}
	object["future_extension"] = json.RawMessage(`{"enabled":true}`)
	wire, err = json.Marshal(object)
	if err != nil {
		return false
	}
	var decoded eventing.Envelope
	return decoded.UnmarshalJSON(wire) == nil
}

func compareOrUpdate(destination string, data []byte, update bool) (bool, error) {
	temporary, err := os.CreateTemp(filepath.Dir(destination), filepath.Base(destination)+".tmp-")
	if err != nil {
		return false, err
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
		return false, err
	}
	if err := temporary.Close(); err != nil {
		return false, err
	}

	current, err := os.ReadFile(destination)
	if err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if !update {
		return true, nil
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return false, err
	}
	renamed = true
	return true, nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
