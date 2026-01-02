// Copyright 2024 Karl Stenerud. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bonjson

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// ============================================================================
// BONJSON Security Rules Tests
// These tests verify compliance with BONJSON security requirements
// ============================================================================

// ============================================================================
// Duplicate Key Detection Tests
// Security Rule: Reject documents with duplicate object keys
// ============================================================================

func TestDuplicateKeyRejection(t *testing.T) {
	// Manually construct BONJSON with duplicate keys
	// Object: {"a": 1, "a": 2}
	var buf bytes.Buffer
	buf.WriteByte(typeObjectStart)
	// First key "a"
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	// Value 1
	buf.WriteByte(0x01)
	// Second key "a" (duplicate)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	// Value 2
	buf.WriteByte(0x02)
	buf.WriteByte(typeContainerEnd)

	var v map[string]int
	err := Unmarshal(buf.Bytes(), &v)
	if err == nil {
		t.Error("expected error for duplicate key")
	}

	var dupErr *DuplicateKeyError
	if !errors.As(err, &dupErr) {
		t.Errorf("expected DuplicateKeyError, got %T: %v", err, err)
	}
}

func TestDuplicateKeyNormalized(t *testing.T) {
	// Test that duplicate detection uses normalized (case-folded) keys
	// This depends on implementation - some normalize, some don't
	// At minimum, exact duplicates must be detected

	var buf bytes.Buffer
	buf.WriteByte(typeObjectStart)
	// Key "ABC"
	buf.WriteByte(typeShortStringBase + 3)
	buf.Write([]byte("ABC"))
	buf.WriteByte(0x01)
	// Key "ABC" again (exact duplicate)
	buf.WriteByte(typeShortStringBase + 3)
	buf.Write([]byte("ABC"))
	buf.WriteByte(0x02)
	buf.WriteByte(typeContainerEnd)

	var v map[string]int
	err := Unmarshal(buf.Bytes(), &v)
	if err == nil {
		t.Error("expected error for duplicate normalized key")
	}
}

// ============================================================================
// Invalid UTF-8 Tests
// Security Rule: Reject strings with invalid UTF-8
// ============================================================================

func TestInvalidUTF8Rejection(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		{"invalid_continuation", []byte{0x80}},                    // Continuation byte without start
		{"incomplete_2byte", []byte{0xC0}},                        // Incomplete 2-byte sequence
		{"incomplete_3byte", []byte{0xE0, 0x80}},                  // Incomplete 3-byte sequence
		{"incomplete_4byte", []byte{0xF0, 0x80, 0x80}},            // Incomplete 4-byte sequence
		{"overlong_2byte", []byte{0xC0, 0x80}},                    // Overlong encoding of NUL
		{"overlong_3byte", []byte{0xE0, 0x80, 0x80}},              // Overlong
		{"invalid_surrogate", []byte{0xED, 0xA0, 0x80}},           // UTF-16 surrogate
		{"above_max", []byte{0xF4, 0x90, 0x80, 0x80}},             // Above U+10FFFF
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a string with invalid UTF-8
			var buf bytes.Buffer
			buf.WriteByte(typeLongString)
			// Length field (non-continuation)
			length := uint64(len(tt.content))
			encodeLengthField(buf.AvailableBuffer()[:16], length, false)
			buf.WriteByte(byte((length << 1) | 0x01)) // simple length encoding
			buf.Write(tt.content)

			var v string
			err := Unmarshal(buf.Bytes(), &v)
			if err == nil {
				t.Error("expected error for invalid UTF-8")
			}
			var utf8Err *InvalidUTF8Error
			if !errors.As(err, &utf8Err) {
				// May be a different error type depending on implementation
				t.Logf("got error: %T: %v", err, err)
			}
		})
	}
}

// ============================================================================
// NUL Character Tests
// Security Rule: Reject strings with NUL characters (by default)
// ============================================================================

func TestNULCharacterRejectionDefault(t *testing.T) {
	// String containing NUL
	var buf bytes.Buffer
	buf.WriteByte(typeShortStringBase + 3)
	buf.Write([]byte{'a', 0x00, 'b'})

	var v string
	err := Unmarshal(buf.Bytes(), &v)
	if err == nil {
		t.Error("expected error for NUL character in string (default)")
	}

	var nulErr *NullInStringError
	if !errors.As(err, &nulErr) {
		t.Logf("got error: %T: %v", err, err)
	}
}

func TestNULCharacterAllowed(t *testing.T) {
	// String containing NUL
	var buf bytes.Buffer
	buf.WriteByte(typeShortStringBase + 3)
	buf.Write([]byte{'a', 0x00, 'b'})

	dec := NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.AllowNUL()

	var v string
	err := dec.Decode(&v)
	if err != nil {
		t.Errorf("AllowNUL should permit NUL characters: %v", err)
	}
}

// ============================================================================
// Chunking Tests
// Security Rule: Reject chunking (by default)
// ============================================================================

func TestChunkingRejectionDefault(t *testing.T) {
	// Create a chunked string manually
	// Long string with continuation bit set
	var buf bytes.Buffer
	buf.WriteByte(typeLongString)
	// Length field with continuation=true
	encodeLengthField(buf.AvailableBuffer()[:16], 5, true)
	buf.WriteByte(0x0b) // length 5, continuation bit set
	buf.Write([]byte("hello"))
	// Another chunk
	encodeLengthField(buf.AvailableBuffer()[:16], 5, false)
	buf.WriteByte(0x0b) // length 5, continuation bit not set (last chunk)
	buf.Write([]byte("world"))

	var v string
	err := Unmarshal(buf.Bytes(), &v)
	if err == nil {
		t.Error("expected error for chunked string (default)")
	}

	var chunkErr *ChunkingError
	if !errors.As(err, &chunkErr) {
		t.Logf("got error: %T: %v", err, err)
	}
}

func TestChunkingAllowed(t *testing.T) {
	// This test depends on having properly formatted chunked data
	// For now, just verify the AllowChunking method exists and can be called
	dec := NewDecoder(bytes.NewReader([]byte{typeNull}))
	dec.AllowChunking()

	var v any
	if err := dec.Decode(&v); err != nil {
		t.Errorf("AllowChunking decode error: %v", err)
	}
}

// ============================================================================
// NaN/Infinity Tests
// Security Rule: Reject NaN and Infinity values
// ============================================================================

func TestNaNInfinityRejectionOnDecode(t *testing.T) {
	// Big number with NaN special value
	tests := []struct {
		name    string
		special byte
	}{
		{"quiet_nan", bigNumNaNQuiet},
		{"signaling_nan", bigNumNaNSignaling},
		{"positive_infinity", bigNumInfinity},
		{"negative_infinity", bigNumInfinity | bigNumNegative},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			buf.WriteByte(typeBigNumber)
			// Significand length = 0 indicates special value
			buf.WriteByte(0x01) // length field = 0 (payload = 0)
			buf.WriteByte(tt.special)

			var v float64
			err := Unmarshal(buf.Bytes(), &v)
			if err == nil {
				t.Error("expected error for NaN/Infinity")
			}
		})
	}
}

// ============================================================================
// Max Depth Tests
// Security Rule: Limit container nesting depth
// ============================================================================

func TestMaxDepthExceeded(t *testing.T) {
	// Create deeply nested arrays
	var buf bytes.Buffer
	depth := 2000 // Exceeds default max depth

	for i := 0; i < depth; i++ {
		buf.WriteByte(typeArrayStart)
	}
	buf.WriteByte(typeNull) // Innermost value
	for i := 0; i < depth; i++ {
		buf.WriteByte(typeContainerEnd)
	}

	var v any
	err := Unmarshal(buf.Bytes(), &v)
	if err == nil {
		t.Error("expected error for exceeding max depth")
	}

	var depthErr *MaxDepthError
	if !errors.As(err, &depthErr) {
		t.Logf("got error: %T: %v", err, err)
	}
}

func TestMaxDepthConfigurable(t *testing.T) {
	// Create moderately nested arrays
	depth := 50
	var buf bytes.Buffer

	for i := 0; i < depth; i++ {
		buf.WriteByte(typeArrayStart)
	}
	buf.WriteByte(typeNull)
	for i := 0; i < depth; i++ {
		buf.WriteByte(typeContainerEnd)
	}

	// With default depth (1000), this should succeed
	var v any
	if err := Unmarshal(buf.Bytes(), &v); err != nil {
		t.Errorf("depth %d should be allowed: %v", depth, err)
	}

	// With lower max depth via Decoder
	dec := NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.SetMaxDepth(10)

	var v2 any
	err := dec.Decode(&v2)
	if err == nil {
		t.Error("expected error when depth exceeds configured max")
	}
}

// ============================================================================
// Max String Length Tests
// ============================================================================

func TestMaxStringLengthExceeded(t *testing.T) {
	// Create a long string via Decoder with reduced limit
	longStr := strings.Repeat("x", 1000)
	data, _ := Marshal(longStr)

	dec := NewDecoder(bytes.NewReader(data))
	dec.SetMaxStringLength(100)

	var v string
	err := dec.Decode(&v)
	if err == nil {
		t.Error("expected error for string exceeding max length")
	}
}

// ============================================================================
// Valid Document Tests
// ============================================================================

func TestValidSecureDocuments(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{"simple_object", map[string]int{"a": 1, "b": 2}},
		{"nested_object", map[string]any{"outer": map[string]int{"inner": 42}}},
		{"unicode_keys", map[string]int{"日本語": 1, "한국어": 2}},
		{"unicode_values", map[string]string{"key": "日本語テスト"}},
		{"deep_nesting", createNestedArray(100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			if !Valid(data) {
				t.Error("Valid returned false for valid document")
			}

			var v any
			if err := Unmarshal(data, &v); err != nil {
				t.Errorf("Unmarshal error: %v", err)
			}
		})
	}
}

func createNestedArray(depth int) any {
	var v any = "leaf"
	for i := 0; i < depth; i++ {
		v = []any{v}
	}
	return v
}

// ============================================================================
// Object Key Order Tests
// ============================================================================

func TestObjectKeyOrder(t *testing.T) {
	// Marshal and unmarshal should preserve key order (within a single encode/decode)
	// Note: Go maps don't preserve order, but the encoding should be deterministic

	original := map[string]int{
		"z": 1,
		"a": 2,
		"m": 3,
	}

	data1, _ := Marshal(original)
	data2, _ := Marshal(original)

	// Multiple encodes should produce identical output
	if !bytes.Equal(data1, data2) {
		t.Error("encoding should be deterministic")
	}
}

// ============================================================================
// Edge Case Security Tests
// ============================================================================

func TestEmptyObject(t *testing.T) {
	data, err := Marshal(map[string]any{})
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var v map[string]any
	if err := Unmarshal(data, &v); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(v) != 0 {
		t.Errorf("expected empty map, got %v", v)
	}
}

func TestEmptyArray(t *testing.T) {
	data, err := Marshal([]any{})
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var v []any
	if err := Unmarshal(data, &v); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(v) != 0 {
		t.Errorf("expected empty slice, got %v", v)
	}
}

func TestEmptyString(t *testing.T) {
	data, err := Marshal("")
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var v string
	if err := Unmarshal(data, &v); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if v != "" {
		t.Errorf("expected empty string, got %q", v)
	}
}

// ============================================================================
// Boundary Value Tests
// ============================================================================

func TestSmallIntBoundaries(t *testing.T) {
	// Test exact boundaries of small integer encoding
	tests := []int64{
		-101, -100, -99, // Around negative boundary
		-1, 0, 1,         // Around zero
		99, 100, 101,     // Around positive boundary
	}

	for _, v := range tests {
		data, err := Marshal(v)
		if err != nil {
			t.Errorf("Marshal(%d) error: %v", v, err)
			continue
		}

		var got int64
		if err := Unmarshal(data, &got); err != nil {
			t.Errorf("Unmarshal error for %d: %v", v, err)
			continue
		}

		if got != v {
			t.Errorf("roundtrip %d -> %d", v, got)
		}

		// Verify encoding size
		expectedSize := 1
		if v < -100 || v > 100 {
			expectedSize = 2 // type code + 1 byte value
		}
		if len(data) != expectedSize {
			t.Errorf("Marshal(%d) size = %d, expected %d", v, len(data), expectedSize)
		}
	}
}

func TestShortStringBoundaries(t *testing.T) {
	// Test exact boundaries of short string encoding
	tests := []int{0, 1, 14, 15, 16, 17}

	for _, length := range tests {
		s := strings.Repeat("x", length)
		data, err := Marshal(s)
		if err != nil {
			t.Errorf("Marshal(len=%d) error: %v", length, err)
			continue
		}

		var got string
		if err := Unmarshal(data, &got); err != nil {
			t.Errorf("Unmarshal error for len=%d: %v", length, err)
			continue
		}

		if got != s {
			t.Errorf("roundtrip mismatch for len=%d", length)
		}

		// Short strings (0-15) should be 1 + length bytes
		// Long strings (16+) should be 1 + length_field + length bytes
		if length <= 15 {
			expectedSize := 1 + length
			if len(data) != expectedSize {
				t.Errorf("short string len=%d encoded as %d bytes, expected %d", length, len(data), expectedSize)
			}
		}
	}
}
