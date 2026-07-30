package eventing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type envelopeWire struct {
	SpecVersion   string          `json:"specversion"`
	ID            EventID         `json:"id"`
	Source        string          `json:"source"`
	Type          EventType       `json:"type"`
	Version       EventVersion    `json:"version"`
	Scope         string          `json:"scope"`
	Subject       string          `json:"subject"`
	PartitionKey  string          `json:"partition_key"`
	OccurredAt    string          `json:"occurred_at"`
	RecordedAt    string          `json:"recorded_at"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	CausationID   string          `json:"causation_id,omitempty"`
	TraceParent   string          `json:"traceparent,omitempty"`
	TraceState    string          `json:"tracestate,omitempty"`
	ContentType   string          `json:"content_type"`
	Payload       json.RawMessage `json:"payload"`
}

// Validate 根据信封自身的 SpecVersion 校验完整结构。
func (e Envelope) Validate() error {
	if e.SpecVersion != SpecVersionV1 {
		return errors.Join(
			ErrInvalidEnvelope,
			fmt.Errorf("%w: %q", ErrUnsupportedSpecVersion, e.SpecVersion),
		)
	}
	checks := []struct {
		name  string
		value string
		max   int
	}{
		{"id", string(e.ID), MaxEventIDLength},
		{"source", e.Source, MaxSourceLength},
		{"type", string(e.Type), MaxEventTypeLength},
		{"scope", e.Scope, MaxScopeLength},
		{"subject", e.Subject, MaxSubjectLength},
		{"partition_key", e.PartitionKey, MaxPartitionKeyLength},
		{"content_type", e.ContentType, len(JSONContentType)},
	}
	for _, check := range checks {
		if err := validateString(check.name, check.value, check.max, true); err != nil {
			return err
		}
	}
	optional := []struct {
		name  string
		value string
		max   int
	}{
		{"correlation_id", e.CorrelationID, MaxCorrelationIDLength},
		{"causation_id", e.CausationID, MaxCausationIDLength},
		{"traceparent", e.TraceParent, MaxTraceParentLength},
		{"tracestate", e.TraceState, MaxTraceStateLength},
	}
	for _, check := range optional {
		if err := validateString(check.name, check.value, check.max, false); err != nil {
			return err
		}
	}
	if e.ContentType != JSONContentType {
		return invalidEnvelope("content_type must be %q", JSONContentType)
	}
	if e.Version == 0 {
		return invalidEnvelope("version must be positive")
	}
	if e.OccurredAt.IsZero() {
		return invalidEnvelope("occurred_at must be non-zero")
	}
	if e.RecordedAt.IsZero() {
		return invalidEnvelope("recorded_at must be non-zero")
	}
	if err := validatePayload(e.Payload); err != nil {
		return err
	}
	return nil
}

// Clone 返回 Payload 已深拷贝的信封。
func (e Envelope) Clone() Envelope {
	clone := e
	clone.Payload = append(json.RawMessage(nil), e.Payload...)
	return clone
}

// MarshalJSON 使用固定字段名和 UTC RFC3339Nano 时间编码有效信封。
func (e Envelope) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(envelopeWire{
		SpecVersion:   e.SpecVersion,
		ID:            e.ID,
		Source:        e.Source,
		Type:          e.Type,
		Version:       e.Version,
		Scope:         e.Scope,
		Subject:       e.Subject,
		PartitionKey:  e.PartitionKey,
		OccurredAt:    e.OccurredAt.UTC().Format(time.RFC3339Nano),
		RecordedAt:    e.RecordedAt.UTC().Format(time.RFC3339Nano),
		CorrelationID: e.CorrelationID,
		CausationID:   e.CausationID,
		TraceParent:   e.TraceParent,
		TraceState:    e.TraceState,
		ContentType:   e.ContentType,
		Payload:       e.Payload,
	})
}

// UnmarshalJSON 接受未知附加字段，并校验已声明的受支持版本。
func (e *Envelope) UnmarshalJSON(data []byte) error {
	if e == nil {
		return invalidEnvelope("cannot unmarshal into nil envelope")
	}
	var wire envelopeWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return invalidEnvelope("decode JSON: %v", err)
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, wire.OccurredAt)
	if err != nil {
		return invalidEnvelope("decode occurred_at: %v", err)
	}
	recordedAt, err := time.Parse(time.RFC3339Nano, wire.RecordedAt)
	if err != nil {
		return invalidEnvelope("decode recorded_at: %v", err)
	}
	decoded := Envelope{
		SpecVersion:   wire.SpecVersion,
		ID:            wire.ID,
		Source:        wire.Source,
		Type:          wire.Type,
		Version:       wire.Version,
		Scope:         wire.Scope,
		Subject:       wire.Subject,
		PartitionKey:  wire.PartitionKey,
		OccurredAt:    occurredAt.UTC(),
		RecordedAt:    recordedAt.UTC(),
		CorrelationID: wire.CorrelationID,
		CausationID:   wire.CausationID,
		TraceParent:   wire.TraceParent,
		TraceState:    wire.TraceState,
		ContentType:   wire.ContentType,
		Payload:       append(json.RawMessage(nil), wire.Payload...),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*e = decoded
	return nil
}

func validateString(name, value string, max int, required bool) error {
	if value == "" && !required {
		return nil
	}
	if !utf8.ValidString(value) {
		return invalidEnvelope("%s must be valid UTF-8", name)
	}
	if strings.TrimSpace(value) == "" {
		return invalidEnvelope("%s must be non-empty", name)
	}
	if len(value) > max {
		return invalidEnvelope("%s exceeds %d bytes", name, max)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return invalidEnvelope("%s contains a control character", name)
		}
	}
	return nil
}

func validatePayload(payload json.RawMessage) error {
	if len(payload) == 0 {
		return invalidEnvelope("payload must be non-empty")
	}
	if len(payload) > MaxPayloadBytes {
		return invalidEnvelope("payload exceeds %d bytes", MaxPayloadBytes)
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' || !json.Valid(trimmed) {
		return invalidEnvelope("payload must be a valid JSON object")
	}
	return nil
}

func invalidEnvelope(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidEnvelope, fmt.Sprintf(format, args...))
}
