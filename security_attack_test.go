//
// security_attack_test.go
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
	"fmt"
	"strings"
	"testing"
)

// ============================================================================
// ATTACK VECTOR TESTS
// These tests verify the codec resists malicious input
// ============================================================================

// ============================================================================
// Integer Overflow / Large Allocation Attacks
// ============================================================================

func TestAttack_LengthFieldOverflow(t *testing.T) {
	// Long strings use 0xFF + data + 0xFF delimiter format.
	// Test malformed long strings.
	tests := []struct {
		name string
		data []byte
	}{
		{
			"long_string_no_terminator",
			// Long string with no terminating 0xFF
			[]byte{typeLongString, 'h', 'e', 'l', 'l', 'o'},
		},
		{
			"long_string_empty_no_terminator",
			// Long string start with no content and no terminator
			[]byte{typeLongString},
		},
		{
			"long_string_only_start",
			// Just the long string marker byte
			[]byte{typeLongString},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			err := Unmarshal(tt.data, &v)
			if err == nil {
				t.Error("expected error for malicious length field")
			}
		})
	}
}

func TestAttack_IntegerLengthMismatch(t *testing.T) {
	// Type code claims N bytes but data is shorter
	tests := []struct {
		name string
		data []byte
	}{
		{"uint8_missing", []byte{typeUintBase}},                       // Claims 1 byte, has 0
		{"uint16_short", []byte{typeUintBase + 1, 0x01}},              // Claims 2 bytes, has 1
		{"uint64_short", []byte{typeUintBase + 3, 0x01, 0x02}},        // Claims 8 bytes, has 2
		{"sint8_missing", []byte{typeSintBase}},                       // Claims 1 byte, has 0
		{"sint64_short", []byte{typeSintBase + 3, 0x01}},              // Claims 8 bytes, has 1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v int64
			err := Unmarshal(tt.data, &v)
			if err == nil {
				t.Error("expected error for truncated integer")
			}
			var truncErr *TruncatedDataError
			if !errors.As(err, &truncErr) {
				t.Errorf("expected TruncatedDataError, got %T: %v", err, err)
			}
		})
	}
}

// Reserved type code rejection is tested by universal spec tests (errors.json)

// ============================================================================
// Container Termination Attacks
// ============================================================================

func TestAttack_TruncatedContainer(t *testing.T) {
	// In delimiter-terminated format, test truncated containers (missing end markers or elements)
	tests := []struct {
		name string
		data []byte
	}{
		{"array_no_end", []byte{typeArray}},                                              // no end marker
		{"array_truncated_elements", []byte{typeArray, typeNull}},                      // element but no end marker
		{"object_no_end", []byte{typeObject}},                                          // no end marker
		{"object_truncated_pairs", []byte{typeObject, typeShortStringBase + 1, 'a'}},   // key but no value or end marker
		{"nested_array_truncated", []byte{typeArray, typeArray, typeContainerEnd}},      // inner array missing end marker
		{"nested_object_truncated", []byte{typeObject, typeShortStringBase + 1, 'a', typeObject}}, // nested object missing end
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			err := Unmarshal(tt.data, &v)
			if err == nil {
				t.Error("expected error for truncated container")
			}
		})
	}
}

func TestAttack_InvalidReservedTypeCodes(t *testing.T) {
	// Test that reserved type codes are rejected
	tests := []struct {
		name string
		data []byte
	}{
		{"reserved_0xc9", []byte{0xc9}},
		{"reserved_0xfa", []byte{0xfa}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			err := Unmarshal(tt.data, &v)
			if err == nil {
				t.Error("expected error for reserved type code")
			}
		})
	}
}

// ============================================================================
// Object Key Attacks
// ============================================================================

func TestAttack_NonStringObjectKey(t *testing.T) {
	// Object keys must be strings - try various other types
	// Delimiter-terminated: typeObject + key + value + typeContainerEnd
	tests := []struct {
		name string
		data []byte
	}{
		{"int_key", []byte{typeObject, 0x65, 0x66, typeContainerEnd}},                            // int key, int value
		{"null_key", []byte{typeObject, typeNull, 0x65, typeContainerEnd}},                        // null key
		{"bool_key", []byte{typeObject, typeTrue, 0x65, typeContainerEnd}},                        // bool key
		{"array_key", []byte{typeObject, typeArray, typeContainerEnd, 0x65, typeContainerEnd}},    // empty array key, int value
		{"float_key", []byte{typeObject, typeFloat32, 0x00, 0x00, 0x00, 0x00, 0x65, typeContainerEnd}}, // float key
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v map[string]any
			err := Unmarshal(tt.data, &v)
			if err == nil {
				t.Error("expected error for non-string object key")
			}
		})
	}
}

func TestAttack_DuplicateKeyVariations(t *testing.T) {
	// Various ways to try duplicate keys
	// Delimiter-terminated: typeObject + key-value pairs + typeContainerEnd
	tests := []struct {
		name string
		data []byte
	}{
		{
			"exact_duplicate",
			[]byte{
				typeObject,
				typeShortStringBase + 3, 'f', 'o', 'o', 0x65, // "foo": 1
				typeShortStringBase + 3, 'f', 'o', 'o', 0x66, // "foo": 2
				typeContainerEnd,
			},
		},
		{
			"empty_key_duplicate",
			[]byte{
				typeObject,
				typeShortStringBase, 0x65, // "": 1
				typeShortStringBase, 0x66, // "": 2
				typeContainerEnd,
			},
		},
		{
			"long_key_duplicate",
			func() []byte {
				key := strings.Repeat("x", 20)
				var buf bytes.Buffer
				buf.WriteByte(typeObject)
				// First key-value
				buf.WriteByte(typeLongString)
				buf.WriteString(key)
				buf.WriteByte(typeLongString)
				buf.WriteByte(0x65) // value 1
				// Duplicate key-value
				buf.WriteByte(typeLongString)
				buf.WriteString(key)
				buf.WriteByte(typeLongString)
				buf.WriteByte(0x66) // value 2
				buf.WriteByte(typeContainerEnd)
				return buf.Bytes()
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v map[string]any
			err := Unmarshal(tt.data, &v)
			if err == nil {
				t.Error("expected error for duplicate key")
			}
			var dupErr *DuplicateKeyError
			if !errors.As(err, &dupErr) {
				t.Logf("got error type: %T: %v", err, err)
			}
		})
	}
}

// Float NaN/Infinity rejection is tested by universal spec tests (floats.json, errors.json)

// ============================================================================
// Big Number Attack Tests
// ============================================================================

func TestAttack_BigNumberMalformed(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		// Exponent LEB128 truncated (high bit set, no continuation)
		{"exp_truncated", []byte{typeBigNumber, 0x80}},
		// Exponent valid but significand LEB128 truncated
		{"sig_truncated", []byte{typeBigNumber, 0x00, 0x80}}, // exp=0, sig starts with 0x80 (needs more bytes)
		// No data after type code
		{"no_data", []byte{typeBigNumber}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			err := Unmarshal(tt.data, &v)
			if err == nil {
				t.Error("expected error for malformed big number")
			}
		})
	}
}

// BigNumber NaN/Infinity rejection is tested by universal spec tests (errors.json)

// ============================================================================
// String Attack Tests
// ============================================================================

func TestAttack_StringInvalidUTF8Comprehensive(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		// Isolated continuation bytes
		{"lone_continuation_80", []byte{0x80}},
		{"lone_continuation_bf", []byte{0xbf}},

		// Truncated sequences
		{"truncated_c2", []byte{0xc2}},           // Start of 2-byte, nothing after
		{"truncated_e0", []byte{0xe0}},           // Start of 3-byte
		{"truncated_e0_80", []byte{0xe0, 0x80}},  // 3-byte with only 1 continuation
		{"truncated_f0", []byte{0xf0}},           // Start of 4-byte
		{"truncated_f0_90", []byte{0xf0, 0x90}},  // 4-byte with only 1 continuation
		{"truncated_f0_90_80", []byte{0xf0, 0x90, 0x80}}, // 4-byte with only 2 continuations

		// Overlong encodings
		{"overlong_2byte_nul", []byte{0xc0, 0x80}},           // NUL as 2-byte
		{"overlong_2byte_slash", []byte{0xc0, 0xaf}},         // '/' as 2-byte
		{"overlong_3byte_nul", []byte{0xe0, 0x80, 0x80}},     // NUL as 3-byte
		{"overlong_4byte_nul", []byte{0xf0, 0x80, 0x80, 0x80}}, // NUL as 4-byte

		// Invalid first bytes
		{"invalid_fe", []byte{0xfe}},
		{"invalid_ff", []byte{0xff}},

		// UTF-16 surrogates (U+D800 - U+DFFF)
		{"surrogate_high_start", []byte{0xed, 0xa0, 0x80}}, // U+D800
		{"surrogate_high_end", []byte{0xed, 0xaf, 0xbf}},   // U+DBFF
		{"surrogate_low_start", []byte{0xed, 0xb0, 0x80}},  // U+DC00
		{"surrogate_low_end", []byte{0xed, 0xbf, 0xbf}},    // U+DFFF

		// Beyond Unicode range (> U+10FFFF)
		{"beyond_unicode", []byte{0xf4, 0x90, 0x80, 0x80}},      // U+110000
		{"way_beyond", []byte{0xf7, 0xbf, 0xbf, 0xbf}},          // U+1FFFFF (if valid)

		// Invalid continuation bytes
		{"missing_continuation", []byte{0xc2, 0x00}},
		{"invalid_continuation", []byte{0xc2, 0xc0}},

		// Mixed valid/invalid
		{"valid_then_invalid", []byte{'a', 'b', 0x80, 'c'}},
		{"invalid_in_middle", []byte{'h', 'e', 0xfe, 'l', 'o'}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create short string
			if len(tt.content) <= 15 {
				data := append([]byte{typeShortStringBase + byte(len(tt.content))}, tt.content...)
				var v string
				err := Unmarshal(data, &v)
				if err == nil {
					t.Error("expected error for invalid UTF-8 (short string)")
				}
			}

			// Create long string (0xFF + data + 0xFF)
			var buf bytes.Buffer
			buf.WriteByte(typeLongString)
			buf.Write(tt.content)
			buf.WriteByte(typeLongString)

			var v string
			err := Unmarshal(buf.Bytes(), &v)
			if err == nil {
				t.Error("expected error for invalid UTF-8 (long string)")
			}
		})
	}
}

func TestAttack_StringNULByte(t *testing.T) {
	// NUL in various positions
	tests := []struct {
		name    string
		content []byte
	}{
		{"nul_start", []byte{0x00, 'a', 'b'}},
		{"nul_middle", []byte{'a', 0x00, 'b'}},
		{"nul_end", []byte{'a', 'b', 0x00}},
		{"only_nul", []byte{0x00}},
		{"multiple_nul", []byte{0x00, 0x00, 0x00}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := append([]byte{typeShortStringBase + byte(len(tt.content))}, tt.content...)
			var v string
			err := Unmarshal(data, &v)
			if err == nil {
				t.Error("expected error for NUL in string (default)")
			}
		})
	}
}

// ============================================================================
// Deep Nesting Attack Tests
// ============================================================================

func TestAttack_DeepNestingArrays(t *testing.T) {
	// Create arrays nested to various depths
	// Default max depth per BONJSON spec is 500
	// Delimiter-terminated: typeArray + children + typeContainerEnd
	depths := []int{100, 499, 500, 501, 1000}

	for _, depth := range depths {
		t.Run(fmt.Sprintf("depth_%04d", depth), func(t *testing.T) {
			var buf bytes.Buffer
			for i := 0; i < depth; i++ {
				buf.WriteByte(typeArray)
			}
			buf.WriteByte(typeNull)
			for i := 0; i < depth; i++ {
				buf.WriteByte(typeContainerEnd)
			}

			var v any
			err := Unmarshal(buf.Bytes(), &v)

			if depth > 500 {
				// Should fail for depths > default max (500 per BONJSON spec)
				if err == nil {
					t.Errorf("expected error for depth %d", depth)
				}
			} else {
				// Should succeed for depths <= 500
				if err != nil {
					t.Errorf("unexpected error for depth %d: %v", depth, err)
				}
			}
		})
	}
}

func TestAttack_DeepNestingObjects(t *testing.T) {
	// Create objects nested to excessive depth
	// Delimiter-terminated: typeObject + key + value + typeContainerEnd
	depth := 2000
	var buf bytes.Buffer

	for i := 0; i < depth; i++ {
		buf.WriteByte(typeObject)
		buf.WriteByte(typeShortStringBase + 1)
		buf.WriteByte('k')
	}
	buf.WriteByte(typeNull)
	for i := 0; i < depth; i++ {
		buf.WriteByte(typeContainerEnd)
	}

	var v any
	err := Unmarshal(buf.Bytes(), &v)
	if err == nil {
		t.Error("expected error for excessive object nesting")
	}
}

func TestAttack_WideContainer(t *testing.T) {
	// Very wide array (many elements)
	// Delimiter-terminated: typeArray + elements + typeContainerEnd
	var buf bytes.Buffer
	count := 10000
	buf.WriteByte(typeArray)
	for i := 0; i < count; i++ {
		// Small int encoding: value + 100 (values 0-100 map to type codes 100-200)
		buf.WriteByte(byte((i % 101) + 100))
	}
	buf.WriteByte(typeContainerEnd)

	var v []int
	err := Unmarshal(buf.Bytes(), &v)
	if err != nil {
		t.Errorf("wide array should succeed: %v", err)
	}
	if len(v) != count {
		t.Errorf("expected %d elements, got %d", count, len(v))
	}
}

// ============================================================================
// Truncation Attack Tests
// ============================================================================

func TestAttack_TruncatedData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"just_array_start", []byte{typeArray}},
		{"just_object_start", []byte{typeObject}},
		{"float32_truncated", []byte{typeFloat32, 0x00, 0x00}},
		{"float64_truncated", []byte{typeFloat64, 0x00, 0x00, 0x00, 0x00}},
		{"string_header_only", []byte{typeLongString}},
		{"bignumber_header_only", []byte{typeBigNumber}},
		// Array with truncated value: typeArray + truncated float64
		{"array_with_truncated_value", []byte{typeArray, typeFloat64, 0x00}},
		// Object with truncated key: typeObject + truncated string
		{"object_with_truncated_key", []byte{typeObject, typeShortStringBase + 5, 'h', 'e'}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			err := Unmarshal(tt.data, &v)
			if err == nil {
				t.Error("expected error for truncated data")
			}
		})
	}
}

// ============================================================================
// Trailing Data Attack Tests
// ============================================================================

func TestAttack_TrailingGarbage(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"int_with_trailing", []byte{0x65, 0x02}}, // small int 1 (0x64+1) + trailing
		{"null_with_trailing", []byte{typeNull, 0x00}},
		{"bool_with_trailing", []byte{typeTrue, 0xff}},
		{"string_with_trailing", []byte{typeShortStringBase + 1, 'a', 0x00}},
		// Empty array + trailing null
		{"array_with_trailing", []byte{typeArray, typeContainerEnd, typeNull}},
		// Empty object + trailing byte
		{"object_with_trailing", []byte{typeObject, typeContainerEnd, 0x01}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			err := Unmarshal(tt.data, &v)
			if err == nil {
				t.Error("expected error for trailing data")
			}
			var synErr *SyntaxError
			if !errors.As(err, &synErr) {
				t.Logf("got error type: %T: %v", err, err)
			}
		})
	}
}

// ============================================================================
// Memory Allocation Attack Tests
// ============================================================================

func TestAttack_LargeStringLength(t *testing.T) {
	// Long strings use 0xFF + data + 0xFF format.
	// A very large string without a terminator should fail (truncated).
	// Also test that max string length is enforced via streaming decoder.
	data := []byte{typeLongString} // no terminator = truncated

	var v string
	err := Unmarshal(data, &v)
	if err == nil {
		t.Error("expected error for unterminated long string")
	}
}

func TestAttack_MaxStringLengthEnforced(t *testing.T) {
	// Create a valid but large string
	largeString := strings.Repeat("x", 10000)
	data, _ := Marshal(largeString)

	// With a restrictive max length
	dec := NewDecoder(bytes.NewReader(data))
	dec.SetMaxStringLength(100)

	var v string
	err := dec.Decode(&v)
	if err == nil {
		t.Error("expected error when string exceeds max length")
	}
}

// ============================================================================
// Type Confusion Attack Tests
// ============================================================================

func TestAttack_TypeConfusion(t *testing.T) {
	// Try to unmarshal wrong types
	tests := []struct {
		name   string
		data   []byte
		target any
	}{
		{"int_into_string", []byte{0x6a}, new(string)}, // small int 6 (0x64+6)
		{"string_into_int", []byte{typeShortStringBase + 3, 'f', 'o', 'o'}, new(int)},
		// Empty array
		{"array_into_string", []byte{typeArray, typeContainerEnd}, new(string)},
		// Empty object
		{"object_into_slice", []byte{typeObject, typeContainerEnd}, new([]int)},
		{"bool_into_int", []byte{typeTrue}, new(int)},
		{"null_into_int", []byte{typeNull}, new(int)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Unmarshal(tt.data, tt.target)
			// These should either error or handle gracefully (save error)
			// The important thing is they shouldn't panic
			_ = err
		})
	}
}

// Integer boundary tests are covered by universal spec tests (integers.json)

// ============================================================================
// Concurrent Decoding Safety Tests
// ============================================================================

func TestAttack_ConcurrentDecode(t *testing.T) {
	// Ensure concurrent decoding of different data doesn't cause issues
	data1, _ := Marshal(map[string]int{"a": 1, "b": 2})
	data2, _ := Marshal([]int{1, 2, 3, 4, 5})
	data3, _ := Marshal("hello world")

	done := make(chan bool, 30)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				var v map[string]int
				Unmarshal(data1, &v)
			}
			done <- true
		}()
		go func() {
			for j := 0; j < 100; j++ {
				var v []int
				Unmarshal(data2, &v)
			}
			done <- true
		}()
		go func() {
			for j := 0; j < 100; j++ {
				var v string
				Unmarshal(data3, &v)
			}
			done <- true
		}()
	}

	for i := 0; i < 30; i++ {
		<-done
	}
}

// ============================================================================
// Valid() Function Attack Tests
// ============================================================================

func TestAttack_ValidWithMalicious(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"empty", []byte{}, false},
		{"valid_null", []byte{typeNull}, true},
		{"valid_int", []byte{0x65}, true}, // small int 1 (0x64+1)
		{"valid_string", []byte{typeShortStringBase + 2, 'h', 'i'}, true},
		{"truncated_float", []byte{typeFloat64, 0x00}, false},
		{"reserved_type", []byte{0xc9}, false}, // reserved range 0xc9-0xcf
		// Truncated array: typeArray with no end marker
		{"truncated_container", []byte{typeArray}, false},
		{"trailing_data", []byte{typeNull, 0x00}, false},
		{"invalid_utf8", []byte{typeShortStringBase + 1, 0x80}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Valid(tt.data)
			if got != tt.valid {
				t.Errorf("Valid() = %v, want %v", got, tt.valid)
			}
		})
	}
}
