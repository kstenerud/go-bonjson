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
// ATTACK VECTOR TESTS
// These tests verify the codec resists malicious input
// ============================================================================

// ============================================================================
// Integer Overflow / Large Allocation Attacks
// ============================================================================

func TestAttack_LengthFieldOverflow(t *testing.T) {
	// Try to create a length field claiming extremely large length
	tests := []struct {
		name string
		data []byte
	}{
		{
			"9byte_max_length",
			// Long string with 9-byte length field claiming max uint64
			append([]byte{typeLongString, 0x00}, bytes.Repeat([]byte{0xff}, 8)...),
		},
		{
			"length_larger_than_data",
			// Long string claiming 1000 bytes but only providing 5
			[]byte{typeLongString, 0x02, 0x0f, 0xd0, 'h', 'e', 'l', 'l', 'o'},
		},
		{
			"length_field_truncated",
			// 9-byte length field but truncated
			[]byte{typeLongString, 0x00, 0xff, 0xff},
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
		{"uint8_missing", []byte{typeUintBase}},                   // Claims 1 byte, has 0
		{"uint16_short", []byte{typeUintBase + 1, 0x01}},          // Claims 2 bytes, has 1
		{"uint64_short", []byte{typeUintBase + 7, 0x01, 0x02}},    // Claims 8 bytes, has 2
		{"sint8_missing", []byte{typeSintBase}},                   // Claims 1 byte, has 0
		{"sint64_short", []byte{typeSintBase + 7, 0x01}},          // Claims 8 bytes, has 1
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

// ============================================================================
// Reserved Type Code Attacks
// ============================================================================

func TestAttack_ReservedTypeCodes(t *testing.T) {
	// All reserved type codes should be rejected
	reserved := []byte{
		0x65, 0x66, 0x67, // Reserved between small int and string types
		0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, // Reserved before containers
	}

	for _, tc := range reserved {
		t.Run("reserved_0x"+string("0123456789abcdef"[tc>>4])+string("0123456789abcdef"[tc&0xf]), func(t *testing.T) {
			var v any
			err := Unmarshal([]byte{tc}, &v)
			if err == nil {
				t.Errorf("expected error for reserved type code 0x%02x", tc)
			}
			var tcErr *InvalidTypeCodeError
			if !errors.As(err, &tcErr) {
				t.Errorf("expected InvalidTypeCodeError, got %T: %v", err, err)
			}
		})
	}
}

// ============================================================================
// Container Termination Attacks
// ============================================================================

func TestAttack_MissingContainerEnd(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"array_no_end", []byte{typeArrayStart, 0x01}},
		{"object_no_end", []byte{typeObjectStart, typeShortStringBase + 1, 'a', 0x01}},
		{"nested_array_no_end", []byte{typeArrayStart, typeArrayStart, 0x01, typeContainerEnd}},
		{"nested_object_no_end", []byte{typeObjectStart, typeShortStringBase + 1, 'a', typeObjectStart, typeShortStringBase + 1, 'b', 0x01, typeContainerEnd}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			err := Unmarshal(tt.data, &v)
			if err == nil {
				t.Error("expected error for missing container end")
			}
		})
	}
}

func TestAttack_ExtraContainerEnd(t *testing.T) {
	// Extra container end markers should cause an error
	tests := []struct {
		name string
		data []byte
	}{
		{"standalone_end", []byte{typeContainerEnd}},
		{"double_end", []byte{typeArrayStart, typeContainerEnd, typeContainerEnd}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			err := Unmarshal(tt.data, &v)
			// First case should be invalid type code, second should be trailing data
			if err == nil {
				t.Error("expected error for extra container end")
			}
		})
	}
}

// ============================================================================
// Object Key Attacks
// ============================================================================

func TestAttack_NonStringObjectKey(t *testing.T) {
	// Object keys must be strings - try various other types
	tests := []struct {
		name string
		data []byte
	}{
		{"int_key", []byte{typeObjectStart, 0x01, 0x02, typeContainerEnd}},
		{"null_key", []byte{typeObjectStart, typeNull, 0x01, typeContainerEnd}},
		{"bool_key", []byte{typeObjectStart, typeTrue, 0x01, typeContainerEnd}},
		{"array_key", []byte{typeObjectStart, typeArrayStart, typeContainerEnd, 0x01, typeContainerEnd}},
		{"float_key", []byte{typeObjectStart, typeFloat16, 0x00, 0x00, 0x01, typeContainerEnd}},
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
	tests := []struct {
		name string
		data []byte
	}{
		{
			"exact_duplicate",
			[]byte{
				typeObjectStart,
				typeShortStringBase + 3, 'f', 'o', 'o', 0x01,
				typeShortStringBase + 3, 'f', 'o', 'o', 0x02,
				typeContainerEnd,
			},
		},
		{
			"empty_key_duplicate",
			[]byte{
				typeObjectStart,
				typeShortStringBase, 0x01, // empty string key
				typeShortStringBase, 0x02, // empty string key again
				typeContainerEnd,
			},
		},
		{
			"long_key_duplicate",
			func() []byte {
				key := strings.Repeat("x", 20)
				var buf bytes.Buffer
				buf.WriteByte(typeObjectStart)
				// First key-value
				buf.WriteByte(typeLongString)
				buf.WriteByte(byte((len(key) << 1) | 0x01))
				buf.WriteString(key)
				buf.WriteByte(0x01)
				// Duplicate key-value
				buf.WriteByte(typeLongString)
				buf.WriteByte(byte((len(key) << 1) | 0x01))
				buf.WriteString(key)
				buf.WriteByte(0x02)
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

// ============================================================================
// Float Attack Tests
// ============================================================================

func TestAttack_FloatSpecialValues(t *testing.T) {
	// Test that NaN and Inf in float16/32/64 are rejected
	tests := []struct {
		name string
		data []byte
	}{
		// bfloat16 NaN (0x7fc0 = quiet NaN)
		{"bfloat16_nan", []byte{typeFloat16, 0xc0, 0x7f}},
		// bfloat16 +Inf (0x7f80)
		{"bfloat16_inf", []byte{typeFloat16, 0x80, 0x7f}},
		// bfloat16 -Inf (0xff80)
		{"bfloat16_neg_inf", []byte{typeFloat16, 0x80, 0xff}},
		// float32 NaN
		{"float32_nan", []byte{typeFloat32, 0x00, 0x00, 0xc0, 0x7f}},
		// float32 +Inf
		{"float32_inf", []byte{typeFloat32, 0x00, 0x00, 0x80, 0x7f}},
		// float32 -Inf
		{"float32_neg_inf", []byte{typeFloat32, 0x00, 0x00, 0x80, 0xff}},
		// float64 NaN
		{"float64_nan", []byte{typeFloat64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf8, 0x7f}},
		// float64 +Inf
		{"float64_inf", []byte{typeFloat64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x7f}},
		// float64 -Inf
		{"float64_neg_inf", []byte{typeFloat64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0xff}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v float64
			err := Unmarshal(tt.data, &v)
			if err == nil {
				t.Errorf("expected error for special float value, got %v", v)
			}
		})
	}
}

// ============================================================================
// Big Number Attack Tests
// ============================================================================

func TestAttack_BigNumberMalformed(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		// Header claims significand but not enough data
		{"sig_truncated", []byte{typeBigNumber, 0x10, 0x01}}, // sig_len=2, exp_len=0, but only 1 byte
		// Header claims exponent but not enough data
		{"exp_truncated", []byte{typeBigNumber, 0x0a}}, // sig_len=1, exp_len=1, but no data
		// Maximum significand length (31 bytes) but no data
		{"max_sig_truncated", []byte{typeBigNumber, 0xf8}}, // sig_len=31, exp_len=0
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

func TestAttack_BigNumberSpecialInvalid(t *testing.T) {
	// When sig_len=0, exp_len encodes special values
	// Only exp_len=0 (zero) is valid, others (NaN, Inf) are invalid
	tests := []struct {
		name   string
		header byte
	}{
		{"infinity_positive", 0x02},       // sig_len=0, exp_len=1, neg=0
		{"infinity_negative", 0x03},       // sig_len=0, exp_len=1, neg=1
		{"nan_quiet_positive", 0x04},      // sig_len=0, exp_len=2, neg=0
		{"nan_quiet_negative", 0x05},      // sig_len=0, exp_len=2, neg=1
		{"nan_signaling_positive", 0x06},  // sig_len=0, exp_len=3, neg=0
		{"nan_signaling_negative", 0x07},  // sig_len=0, exp_len=3, neg=1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte{typeBigNumber, tt.header}
			var v any
			err := Unmarshal(data, &v)
			if err == nil {
				t.Errorf("expected error for invalid big number special value")
			}
		})
	}
}

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

			// Create long string
			var buf bytes.Buffer
			buf.WriteByte(typeLongString)
			encodeLengthField(buf.AvailableBuffer()[:16], uint64(len(tt.content)), false)
			buf.WriteByte(byte((len(tt.content) << 1) | 0x01))
			buf.Write(tt.content)

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

func TestAttack_StringChunkingBomb(t *testing.T) {
	// Create a string with many tiny chunks to waste resources
	var buf bytes.Buffer
	buf.WriteByte(typeLongString)

	// 100 chunks of 1 byte each (should be rejected by default)
	for i := 0; i < 100; i++ {
		continuation := i < 99
		var lenByte byte = 0x03 // length=1, continuation=1
		if !continuation {
			lenByte = 0x03 // length=1, continuation=0
		}
		buf.WriteByte(lenByte)
		buf.WriteByte('x')
	}

	var v string
	err := Unmarshal(buf.Bytes(), &v)
	// Default should reject chunking
	if err == nil {
		t.Error("expected error for chunked string (default)")
	}
}

// ============================================================================
// Deep Nesting Attack Tests
// ============================================================================

func TestAttack_DeepNestingArrays(t *testing.T) {
	// Create arrays nested to various depths
	depths := []int{100, 500, 1000, 1001, 2000}

	for _, depth := range depths {
		t.Run("depth_"+string(rune('0'+depth/1000))+string(rune('0'+(depth%1000)/100))+string(rune('0'+(depth%100)/10))+string(rune('0'+depth%10)), func(t *testing.T) {
			var buf bytes.Buffer
			for i := 0; i < depth; i++ {
				buf.WriteByte(typeArrayStart)
			}
			buf.WriteByte(typeNull)
			for i := 0; i < depth; i++ {
				buf.WriteByte(typeContainerEnd)
			}

			var v any
			err := Unmarshal(buf.Bytes(), &v)

			if depth > 1000 {
				// Should fail for depths > default max (1000)
				if err == nil {
					t.Errorf("expected error for depth %d", depth)
				}
			} else {
				// Should succeed for depths <= 1000
				if err != nil {
					t.Errorf("unexpected error for depth %d: %v", depth, err)
				}
			}
		})
	}
}

func TestAttack_DeepNestingObjects(t *testing.T) {
	// Create objects nested to excessive depth
	depth := 2000
	var buf bytes.Buffer

	for i := 0; i < depth; i++ {
		buf.WriteByte(typeObjectStart)
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
	var buf bytes.Buffer
	buf.WriteByte(typeArrayStart)
	for i := 0; i < 10000; i++ {
		buf.WriteByte(byte(i % 101)) // Small ints
	}
	buf.WriteByte(typeContainerEnd)

	var v []int
	err := Unmarshal(buf.Bytes(), &v)
	if err != nil {
		t.Errorf("wide array should succeed: %v", err)
	}
	if len(v) != 10000 {
		t.Errorf("expected 10000 elements, got %d", len(v))
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
		{"just_array_start", []byte{typeArrayStart}},
		{"just_object_start", []byte{typeObjectStart}},
		{"float16_truncated", []byte{typeFloat16, 0x00}},
		{"float32_truncated", []byte{typeFloat32, 0x00, 0x00}},
		{"float64_truncated", []byte{typeFloat64, 0x00, 0x00, 0x00, 0x00}},
		{"string_header_only", []byte{typeLongString}},
		{"bignumber_header_only", []byte{typeBigNumber}},
		{"array_with_truncated_value", []byte{typeArrayStart, typeFloat64, 0x00}},
		{"object_with_truncated_key", []byte{typeObjectStart, typeShortStringBase + 5, 'h', 'e'}},
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
		{"int_with_trailing", []byte{0x01, 0x02}},
		{"null_with_trailing", []byte{typeNull, 0x00}},
		{"bool_with_trailing", []byte{typeTrue, 0xff}},
		{"string_with_trailing", []byte{typeShortStringBase + 1, 'a', 0x00}},
		{"array_with_trailing", []byte{typeArrayStart, typeContainerEnd, typeNull}},
		{"object_with_trailing", []byte{typeObjectStart, typeContainerEnd, 0x01}},
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
	// Try to allocate a huge string via length field
	// Using 9-byte length encoding for maximum value
	data := []byte{
		typeLongString,
		0x00, // 9-byte length marker
		0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, // 2GB length
	}

	var v string
	err := Unmarshal(data, &v)
	if err == nil {
		t.Error("expected error for huge string length")
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
		{"int_into_string", []byte{0x2a}, new(string)},
		{"string_into_int", []byte{typeShortStringBase + 3, 'f', 'o', 'o'}, new(int)},
		{"array_into_string", []byte{typeArrayStart, typeContainerEnd}, new(string)},
		{"object_into_slice", []byte{typeObjectStart, typeContainerEnd}, new([]int)},
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

// ============================================================================
// Boundary Value Tests
// ============================================================================

func TestAttack_IntegerBoundaries(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want int64
	}{
		// Small int boundaries
		{"small_int_max", []byte{0x64}, 100},
		{"small_int_min", []byte{0x9c}, -100},

		// Just outside small int range
		{"small_int_max_plus_1", []byte{typeUintBase, 0x65}, 101},
		{"small_int_min_minus_1", []byte{typeSintBase, 0x9b}, -101},

		// Max/min for various sizes
		{"int8_max", []byte{typeSintBase, 0x7f}, 127},
		{"int8_min", []byte{typeSintBase, 0x80}, -128},
		{"uint8_max", []byte{typeUintBase, 0xff}, 255},

		{"int16_max", []byte{typeSintBase + 1, 0xff, 0x7f}, 32767},
		{"int16_min", []byte{typeSintBase + 1, 0x00, 0x80}, -32768},
		{"uint16_max", []byte{typeUintBase + 1, 0xff, 0xff}, 65535},

		{"int32_max", []byte{typeSintBase + 3, 0xff, 0xff, 0xff, 0x7f}, 2147483647},
		{"int32_min", []byte{typeSintBase + 3, 0x00, 0x00, 0x00, 0x80}, -2147483648},

		{"int64_max", []byte{typeSintBase + 7, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}, 9223372036854775807},
		{"int64_min", []byte{typeSintBase + 7, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80}, -9223372036854775808},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v int64
			err := Unmarshal(tt.data, &v)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if v != tt.want {
				t.Errorf("got %d, want %d", v, tt.want)
			}
		})
	}
}

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
		{"valid_int", []byte{0x01}, true},
		{"valid_string", []byte{typeShortStringBase + 2, 'h', 'i'}, true},
		{"truncated_float", []byte{typeFloat64, 0x00}, false},
		{"reserved_type", []byte{0x65}, false},
		{"missing_container_end", []byte{typeArrayStart, 0x01}, false},
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
