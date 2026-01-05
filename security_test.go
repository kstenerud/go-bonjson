//
// security_test.go
//
// Copyright (c) 2026 Karl Stenerud. All rights reserved.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall remain in place
// in this source code.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.
//

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

func TestDuplicateKeyExact(t *testing.T) {
	// Test that exact duplicate keys are detected

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
		{"invalid_continuation", []byte{0x80}},          // Continuation byte without start
		{"incomplete_2byte", []byte{0xC0}},              // Incomplete 2-byte sequence
		{"incomplete_3byte", []byte{0xE0, 0x80}},        // Incomplete 3-byte sequence
		{"incomplete_4byte", []byte{0xF0, 0x80, 0x80}},  // Incomplete 4-byte sequence
		{"overlong_2byte", []byte{0xC0, 0x80}},          // Overlong encoding of NUL
		{"overlong_3byte", []byte{0xE0, 0x80, 0x80}},    // Overlong
		{"invalid_surrogate", []byte{0xED, 0xA0, 0x80}}, // UTF-16 surrogate
		{"above_max", []byte{0xF4, 0x90, 0x80, 0x80}},   // Above U+10FFFF
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

func TestChunkingLimitDefault(t *testing.T) {
	// Create a string with more than 100 chunks (default limit)
	var buf bytes.Buffer
	buf.WriteByte(typeLongString)

	// Create 101 chunks to exceed the default limit of 100
	for i := 0; i < 101; i++ {
		continuation := i < 100 // continuation bit for all but last
		if continuation {
			buf.WriteByte(0x03) // length 1, continuation bit set
		} else {
			buf.WriteByte(0x02) // length 1, no continuation
		}
		buf.WriteByte('x')
	}

	var v string
	err := Unmarshal(buf.Bytes(), &v)
	if err == nil {
		t.Error("expected error for string with too many chunks")
	}

	var chunkErr *TooManyChunksError
	if !errors.As(err, &chunkErr) {
		t.Logf("got error: %T: %v", err, err)
	}
}

func TestSetMaxChunksMethod(t *testing.T) {
	// Verify SetMaxChunks method exists and can be called
	dec := NewDecoder(bytes.NewReader([]byte{typeNull}))
	dec.SetMaxChunks(1000)

	var v any
	if err := dec.Decode(&v); err != nil {
		t.Errorf("Decode after SetMaxChunks error: %v", err)
	}
}

// ============================================================================
// NaN/Infinity Tests
// Security Rule: Reject NaN and Infinity values
// ============================================================================

func TestBigNumberSpecialValuesRejectionOnDecode(t *testing.T) {
	// Big number special values (NaN, Infinity) should be rejected
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
		-1, 0, 1, // Around zero
		99, 100, 101, // Around positive boundary
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

// ============================================================================
// Invalid UTF-8 Handling Mode Tests
// ============================================================================

func TestInvalidUTF8ModeReplace(t *testing.T) {
	// Create string with invalid UTF-8: "hello" + invalid byte + "world"
	var buf bytes.Buffer
	buf.WriteByte(typeShortStringBase + 11) // 11 bytes total
	buf.Write([]byte("hello"))
	buf.WriteByte(0xff) // invalid UTF-8 byte
	buf.Write([]byte("world"))

	dec := NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.SetInvalidUTF8Mode(UTF8Replace)

	var v string
	err := dec.Decode(&v)
	if err != nil {
		t.Fatalf("UTF8Replace should not error: %v", err)
	}

	// Should replace 0xff with U+FFFD (replacement character)
	expected := "hello\uFFFDworld"
	if v != expected {
		t.Errorf("got %q, expected %q", v, expected)
	}
}

func TestInvalidUTF8ModeDelete(t *testing.T) {
	// Create string with invalid UTF-8: "hello" + invalid byte + "world"
	var buf bytes.Buffer
	buf.WriteByte(typeShortStringBase + 11) // 11 bytes total
	buf.Write([]byte("hello"))
	buf.WriteByte(0xff) // invalid UTF-8 byte
	buf.Write([]byte("world"))

	dec := NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.SetInvalidUTF8Mode(UTF8Delete)

	var v string
	err := dec.Decode(&v)
	if err != nil {
		t.Fatalf("UTF8Delete should not error: %v", err)
	}

	// Should delete the invalid byte
	expected := "helloworld"
	if v != expected {
		t.Errorf("got %q, expected %q", v, expected)
	}
}

func TestInvalidUTF8ModeIgnore(t *testing.T) {
	// Create string with invalid UTF-8: "hello" + invalid byte + "world"
	var buf bytes.Buffer
	buf.WriteByte(typeShortStringBase + 11) // 11 bytes total
	buf.Write([]byte("hello"))
	buf.WriteByte(0xff) // invalid UTF-8 byte
	buf.Write([]byte("world"))

	dec := NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.SetInvalidUTF8Mode(UTF8Ignore)

	var v string
	err := dec.Decode(&v)
	if err != nil {
		t.Fatalf("UTF8Ignore should not error: %v", err)
	}

	// Should pass through the invalid byte unchanged
	expected := "hello\xffworld"
	if v != expected {
		t.Errorf("got %q, expected %q", v, expected)
	}
}

func TestInvalidUTF8ModeRejectDefault(t *testing.T) {
	// Create string with invalid UTF-8
	var buf bytes.Buffer
	buf.WriteByte(typeShortStringBase + 6)
	buf.Write([]byte("hello"))
	buf.WriteByte(0xff) // invalid UTF-8 byte

	var v string
	err := Unmarshal(buf.Bytes(), &v)
	if err == nil {
		t.Error("expected error for invalid UTF-8 with default mode")
	}

	var utf8Err *InvalidUTF8Error
	if !errors.As(err, &utf8Err) {
		t.Errorf("expected InvalidUTF8Error, got %T: %v", err, err)
	}
}

func TestInvalidUTF8ModeWithMultipleInvalidBytes(t *testing.T) {
	// Helper to create string with multiple invalid UTF-8 bytes
	// Content: a + 0x80 + b + 0xff + c + 0xfe + d = 7 bytes
	makeTestData := func() []byte {
		var buf bytes.Buffer
		buf.WriteByte(typeShortStringBase + 7) // 7 bytes
		buf.Write([]byte("a"))
		buf.WriteByte(0x80) // invalid
		buf.Write([]byte("b"))
		buf.WriteByte(0xff) // invalid
		buf.Write([]byte("c"))
		buf.WriteByte(0xfe) // invalid
		buf.Write([]byte("d"))
		return buf.Bytes()
	}

	t.Run("replace", func(t *testing.T) {
		dec := NewDecoder(bytes.NewReader(makeTestData()))
		dec.SetInvalidUTF8Mode(UTF8Replace)

		var v string
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("error: %v", err)
		}
		expected := "a\uFFFDb\uFFFDc\uFFFDd"
		if v != expected {
			t.Errorf("got %q, expected %q", v, expected)
		}
	})

	t.Run("delete", func(t *testing.T) {
		dec := NewDecoder(bytes.NewReader(makeTestData()))
		dec.SetInvalidUTF8Mode(UTF8Delete)

		var v string
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("error: %v", err)
		}
		expected := "abcd"
		if v != expected {
			t.Errorf("got %q, expected %q", v, expected)
		}
	})
}

// ============================================================================
// Duplicate Key Handling Mode Tests
// ============================================================================

func TestDuplicateKeyModeKeepFirst(t *testing.T) {
	// Object: {"a": 1, "a": 2}
	var buf bytes.Buffer
	buf.WriteByte(typeObjectStart)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x01) // value 1
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x02) // value 2
	buf.WriteByte(typeContainerEnd)

	dec := NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.SetDuplicateKeyMode(DupKeyKeepFirst)

	var v map[string]int
	err := dec.Decode(&v)
	if err != nil {
		t.Fatalf("DupKeyKeepFirst should not error: %v", err)
	}

	if v["a"] != 1 {
		t.Errorf("expected a=1 (first value), got a=%d", v["a"])
	}
}

func TestDuplicateKeyModeReplace(t *testing.T) {
	// Object: {"a": 1, "a": 2}
	var buf bytes.Buffer
	buf.WriteByte(typeObjectStart)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x01) // value 1
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x02) // value 2
	buf.WriteByte(typeContainerEnd)

	dec := NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.SetDuplicateKeyMode(DupKeyReplace)

	var v map[string]int
	err := dec.Decode(&v)
	if err != nil {
		t.Fatalf("DupKeyReplace should not error: %v", err)
	}

	if v["a"] != 2 {
		t.Errorf("expected a=2 (last value), got a=%d", v["a"])
	}
}

func TestDuplicateKeyModeRejectDefault(t *testing.T) {
	// Object: {"a": 1, "a": 2}
	var buf bytes.Buffer
	buf.WriteByte(typeObjectStart)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x01)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x02)
	buf.WriteByte(typeContainerEnd)

	var v map[string]int
	err := Unmarshal(buf.Bytes(), &v)
	if err == nil {
		t.Error("expected error for duplicate key with default mode")
	}

	var dupErr *DuplicateKeyError
	if !errors.As(err, &dupErr) {
		t.Errorf("expected DuplicateKeyError, got %T: %v", err, err)
	}
}

func TestDuplicateKeyModeWithStruct(t *testing.T) {
	type TestStruct struct {
		A int `json:"a"`
	}

	// Object: {"a": 1, "a": 2}
	var buf bytes.Buffer
	buf.WriteByte(typeObjectStart)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x01)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x02)
	buf.WriteByte(typeContainerEnd)

	t.Run("keep_first", func(t *testing.T) {
		dec := NewDecoder(bytes.NewReader(buf.Bytes()))
		dec.SetDuplicateKeyMode(DupKeyKeepFirst)

		var v TestStruct
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("error: %v", err)
		}
		if v.A != 1 {
			t.Errorf("expected A=1, got A=%d", v.A)
		}
	})

	t.Run("replace", func(t *testing.T) {
		dec := NewDecoder(bytes.NewReader(buf.Bytes()))
		dec.SetDuplicateKeyMode(DupKeyReplace)

		var v TestStruct
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("error: %v", err)
		}
		if v.A != 2 {
			t.Errorf("expected A=2, got A=%d", v.A)
		}
	})
}

func TestDuplicateKeyModeWithInterface(t *testing.T) {
	// Object: {"a": 1, "a": 2}
	var buf bytes.Buffer
	buf.WriteByte(typeObjectStart)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x01)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x02)
	buf.WriteByte(typeContainerEnd)

	t.Run("keep_first", func(t *testing.T) {
		dec := NewDecoder(bytes.NewReader(buf.Bytes()))
		dec.SetDuplicateKeyMode(DupKeyKeepFirst)

		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("error: %v", err)
		}
		m := v.(map[string]any)
		if m["a"].(int64) != 1 {
			t.Errorf("expected a=1, got a=%v", m["a"])
		}
	})

	t.Run("replace", func(t *testing.T) {
		dec := NewDecoder(bytes.NewReader(buf.Bytes()))
		dec.SetDuplicateKeyMode(DupKeyReplace)

		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("error: %v", err)
		}
		m := v.(map[string]any)
		if m["a"].(int64) != 2 {
			t.Errorf("expected a=2, got a=%v", m["a"])
		}
	})
}

// ============================================================================
// NaN/Infinity Allow Tests
// ============================================================================

func TestAllowNaNInfinity(t *testing.T) {
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
			buf.WriteByte(tt.special)

			dec := NewDecoder(bytes.NewReader(buf.Bytes()))
			dec.AllowNaNInfinity()

			var v float64
			err := dec.Decode(&v)
			if err != nil {
				t.Fatalf("AllowNaNInfinity should not error: %v", err)
			}

			// Verify the value is correct
			switch tt.special & 0x06 { // mask out negative bit
			case bigNumNaNQuiet, bigNumNaNSignaling:
				if v == v { // NaN != NaN
					t.Errorf("expected NaN, got %v", v)
				}
			case bigNumInfinity:
				if tt.special&bigNumNegative != 0 {
					if v >= 0 {
						t.Errorf("expected negative infinity, got %v", v)
					}
				} else {
					if v <= 0 {
						t.Errorf("expected positive infinity, got %v", v)
					}
				}
			}
		})
	}
}

func TestNaNInfinityRejectedByDefault(t *testing.T) {
	tests := []struct {
		name    string
		special byte
	}{
		{"quiet_nan", bigNumNaNQuiet},
		{"positive_infinity", bigNumInfinity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			buf.WriteByte(typeBigNumber)
			buf.WriteByte(tt.special)

			var v float64
			err := Unmarshal(buf.Bytes(), &v)
			if err == nil {
				t.Error("expected error for NaN/Infinity with default mode")
			}

			var valErr *InvalidValueError
			if !errors.As(err, &valErr) {
				t.Errorf("expected InvalidValueError, got %T: %v", err, err)
			}
		})
	}
}
