//
// wire_test.go
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

// ABOUTME: Tests for low-level wire format encoding/decoding.
// ABOUTME: Native-size integers, big numbers (signed length + LE magnitude),
// ABOUTME: FF-terminated long strings.

package bonjson

import (
	"math"
	"math/big"
	"testing"
)

// ============================================================================
// Integer Decoding Error Tests
// ============================================================================

func TestDecodeIntegerTruncated(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		typeCode byte
	}{
		{"uint truncated", []byte{0x01}, typeUintBase + 1}, // needs 2 bytes, only has 1
		{"sint truncated", []byte{}, typeSintBase + 2},     // needs 4 bytes, has 0
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := decodeInteger(tt.data, tt.typeCode)
			if err == nil {
				t.Error("expected error for truncated data")
			}
		})
	}
}

// ============================================================================
// Float Decoding Error Tests
// ============================================================================

func TestDecodeFloatTruncated(t *testing.T) {
	// Float64 needs 8 bytes of data (after type code)
	_, err := decodeFloat64([]byte{0x01, 0x02, 0x03})
	if err == nil {
		t.Error("expected error for truncated float64")
	}

	// Float32 needs 4 bytes of data (after type code)
	_, err = decodeFloat32([]byte{0x01})
	if err == nil {
		t.Error("expected error for truncated float32")
	}
}

// ============================================================================
// Integer Encoding Boundary Tests
// ============================================================================

func TestIntegerBoundaryValues(t *testing.T) {
	t.Run("small_int_boundaries", func(t *testing.T) {
		// Small integers: -100 to 100 encode as single byte (value + 100)
		dst := make([]byte, 16)

		// 0 encodes to 0x64 (0 + 100 = 100)
		n := encodeUnsignedInt(dst, 0)
		if n != 1 || dst[0] != 0x64 {
			t.Errorf("0 should encode as single byte 0x64 (value+100), got %d bytes: %v", n, dst[:n])
		}

		// 100 encodes to 0xc8 (100 + 100 = 200)
		n = encodeUnsignedInt(dst, 100)
		if n != 1 || dst[0] != 0xc8 {
			t.Errorf("100 should encode as single byte 0xc8 (value+100), got %d bytes: %v", n, dst[:n])
		}

		n = encodeUnsignedInt(dst, 101)
		if n != 2 {
			t.Errorf("101 should encode as 2 bytes (type + 1 data), got %d", n)
		}

		// -1 encodes to 0x63 (-1 + 100 = 99)
		n = encodeSignedInt(dst, -1)
		if n != 1 || dst[0] != 0x63 {
			t.Errorf("-1 should encode as single byte 0x63 (value+100), got %d bytes: %v", n, dst[:n])
		}

		// -100 encodes to 0x00 (-100 + 100 = 0)
		n = encodeSignedInt(dst, -100)
		if n != 1 || dst[0] != 0x00 {
			t.Errorf("-100 should encode as single byte 0x00 (value+100), got %d bytes: %v", n, dst[:n])
		}

		n = encodeSignedInt(dst, -101)
		if n != 2 {
			t.Errorf("-101 should encode as 2 bytes (type + 1 data), got %d", n)
		}
	})

	t.Run("unsigned_native_size_boundaries", func(t *testing.T) {
		// Phase 2: native sizes only (1, 2, 4, 8 bytes)
		tests := []struct {
			value     uint64
			wantBytes int // type code + data bytes
		}{
			{255, 2},                // fits in 1 byte: type + 1
			{256, 3},                // needs 2 bytes: type + 2
			{65535, 3},              // max 2-byte: type + 2
			{65536, 5},              // needs 4 bytes (rounds up from 3): type + 4
			{0xFFFFFFFF, 5},         // max 4-byte: type + 4
			{0x100000000, 9},        // needs 8 bytes (rounds up from 5): type + 8
			{0xFFFFFFFFFFFFFFFF, 9}, // max 8-byte: type + 8
		}

		for _, tt := range tests {
			dst := make([]byte, 16)
			n := encodeUnsignedInt(dst, tt.value)
			if n != tt.wantBytes {
				t.Errorf("encodeUnsignedInt(%d) = %d bytes, want %d", tt.value, n, tt.wantBytes)
			}

			// Verify roundtrip via decode
			_, decoded, _, err := decodeInteger(dst[1:], dst[0])
			if err != nil {
				t.Errorf("decodeInteger error for %d: %v", tt.value, err)
			}
			if decoded != tt.value {
				t.Errorf("roundtrip for %d got %d", tt.value, decoded)
			}
		}
	})

	t.Run("signed_native_size_boundaries", func(t *testing.T) {
		tests := []struct {
			value     int64
			wantBytes int
		}{
			{127, 2},    // fits in 1-byte signed: type + 1
			{128, 3},    // needs 2 bytes (sign bit): type + 2
			{-128, 2},   // min 1-byte signed: type + 1
			{-129, 3},   // needs 2 bytes: type + 2
			{32767, 3},  // max 2-byte signed: type + 2
			{32768, 5},  // needs 4 bytes (rounds up): type + 4
			{-32768, 3}, // min 2-byte signed: type + 2
			{-32769, 5}, // needs 4 bytes: type + 4
		}

		for _, tt := range tests {
			dst := make([]byte, 16)
			n := encodeSignedInt(dst, tt.value)
			if n != tt.wantBytes {
				t.Errorf("encodeSignedInt(%d) = %d bytes, want %d", tt.value, n, tt.wantBytes)
			}

			// Verify roundtrip
			decoded, _, _, err := decodeInteger(dst[1:], dst[0])
			if err != nil {
				t.Errorf("decodeInteger error for %d: %v", tt.value, err)
			}
			if decoded != tt.value {
				t.Errorf("roundtrip for %d got %d", tt.value, decoded)
			}
		}
	})
}

func TestSignExtendNative(t *testing.T) {
	tests := []struct {
		name string
		val  uint64
		n    int
		want int64
	}{
		{"1_byte_positive", 0x7F, 1, 127},
		{"1_byte_negative", 0x80, 1, -128},
		{"1_byte_minus_one", 0xFF, 1, -1},
		{"2_byte_positive", 0x7FFF, 2, 32767},
		{"2_byte_negative", 0x8000, 2, -32768},
		{"4_byte_positive", 0x7FFFFFFF, 4, 2147483647},
		{"4_byte_negative", 0x80000000, 4, -2147483648},
		{"8_byte_no_extension", 0x7FFFFFFFFFFFFFFF, 8, 9223372036854775807},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := signExtendNative(tt.val, tt.n)
			if got != tt.want {
				t.Errorf("signExtendNative(0x%X, %d) = %d, want %d", tt.val, tt.n, got, tt.want)
			}
		})
	}
}

func TestDecodeIntegerInvalidTypeCode(t *testing.T) {
	// Type codes that aren't integers
	invalidCodes := []byte{typeNull, typeFalse, typeTrue, typeFloat32, typeFloat64}

	for _, tc := range invalidCodes {
		_, _, _, err := decodeInteger([]byte{0x00}, tc)
		if err == nil {
			t.Errorf("decodeInteger should fail for type code 0x%02x", tc)
		}
		if _, ok := err.(*InvalidTypeCodeError); !ok {
			t.Errorf("expected InvalidTypeCodeError for 0x%02x, got %T", tc, err)
		}
	}
}

// ============================================================================
// BigNumber Tests (signed_length + LE magnitude)
// ============================================================================

func TestBigNumberWireFormat(t *testing.T) {
	// Verify specific byte sequences match the spec examples
	tests := []struct {
		name     string
		sig      int64
		exp      int64
		expected []byte
	}{
		{"zero", 0, 0, []byte{0xca, 0x00, 0x00}},
		{"positive_2", 2, 0, []byte{0xca, 0x00, 0x02, 0x02}},
		{"negative_1", -1, 0, []byte{0xca, 0x00, 0x01, 0x01}},
		{"decimal_1_5", 15, -1, []byte{0xca, 0x01, 0x02, 0x0f}},
		{"value_1000", 10, 2, []byte{0xca, 0x04, 0x02, 0x0a}},
		{"value_255", 255, 0, []byte{0xca, 0x00, 0x02, 0xff}},
		{"value_65535", 65535, 0, []byte{0xca, 0x00, 0x04, 0xff, 0xff}},
		{"value_65537", 65537, 0, []byte{0xca, 0x00, 0x06, 0x01, 0x00, 0x01}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bn := &BigNumber{
				Significand: big.NewInt(tt.sig),
				Exponent:    big.NewInt(tt.exp),
			}
			dst := make([]byte, 64)
			n := encodeBigNumber(dst, bn)
			got := dst[:n]
			if len(got) != len(tt.expected) {
				t.Errorf("length mismatch: got %d bytes %x, want %d bytes %x", len(got), got, len(tt.expected), tt.expected)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("byte %d: got 0x%02x, want 0x%02x (full: %x vs %x)", i, got[i], tt.expected[i], got, tt.expected)
					return
				}
			}
		})
	}
}

func TestBigNumberZero(t *testing.T) {
	bn := &BigNumber{
		Significand: big.NewInt(0),
		Exponent:    big.NewInt(0),
	}
	dst := make([]byte, 32)
	n := encodeBigNumber(dst, bn)

	decoded, special, _, err := decodeBigNumber(dst[1:n])
	if err != nil {
		t.Errorf("decode error: %v", err)
	}
	if special != BigNumNormal {
		t.Errorf("zero should be BigNumNormal, got %v", special)
	}
	if decoded.Significand.Sign() != 0 {
		t.Errorf("zero significand should be 0, got %v", decoded.Significand)
	}
}

func TestBigNumberRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		sig  int64
		exp  int64
	}{
		{"positive_no_exp", 12345, 0},
		{"negative_no_exp", -12345, 0},
		{"positive_with_exp", 12345, 10},
		{"negative_with_neg_exp", -12345, -10},
		{"large_exp", 1, 1000},
		{"large_neg_exp", 1, -1000},
		{"one", 1, 0},
		{"minus_one", -1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bn := &BigNumber{
				Significand: big.NewInt(tt.sig),
				Exponent:    big.NewInt(tt.exp),
			}
			dst := make([]byte, 64)
			n := encodeBigNumber(dst, bn)

			decoded, _, _, err := decodeBigNumber(dst[1:n])
			if err != nil {
				t.Errorf("decode error: %v", err)
				return
			}
			if decoded.Significand.Cmp(big.NewInt(tt.sig)) != 0 {
				t.Errorf("significand = %v, want %d", decoded.Significand, tt.sig)
			}
			if decoded.Exponent.Cmp(big.NewInt(tt.exp)) != 0 {
				t.Errorf("exponent = %v, want %d", decoded.Exponent, tt.exp)
			}
		})
	}
}

func TestBigNumberLargeValues(t *testing.T) {
	// Test with values that don't fit in int64
	sig := new(big.Int)
	sig.SetString("123456789012345678901234567890", 10)

	bn := &BigNumber{
		Significand: sig,
		Exponent:    big.NewInt(100),
	}
	dst := make([]byte, 128)
	n := encodeBigNumber(dst, bn)

	decoded, _, _, err := decodeBigNumber(dst[1:n])
	if err != nil {
		t.Errorf("decode error: %v", err)
		return
	}
	if decoded.Significand.Cmp(sig) != 0 {
		t.Errorf("significand mismatch: got %v, want %v", decoded.Significand, sig)
	}
	if decoded.Exponent.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("exponent mismatch: got %v, want 100", decoded.Exponent)
	}
}

func TestBigNumberTruncatedData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"truncated_exponent", []byte{0x80}},                          // LEB128 continuation but no more bytes
		{"truncated_signed_length", []byte{0x00, 0x80}},               // exponent done, signed_length truncated
		{"truncated_magnitude", []byte{0x00, 0x04, 0xff}},             // signed_length=+2 but only 1 magnitude byte
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := decodeBigNumber(tt.data)
			if err == nil {
				t.Error("expected error for truncated data")
			}
		})
	}
}

func TestBigNumberNonNormalizedMagnitude(t *testing.T) {
	// signed_length=+2, magnitude=0x01 0x00 (trailing zero = non-normalized)
	data := []byte{0x00, 0x04, 0x01, 0x00}
	_, _, _, err := decodeBigNumber(data)
	if err == nil {
		t.Error("expected error for non-normalized magnitude")
	}
	if _, ok := err.(*InvalidValueError); !ok {
		t.Errorf("expected InvalidValueError, got %T: %v", err, err)
	}
}

// ============================================================================
// Zigzag LEB128 Tests
// ============================================================================

func TestZigzagRoundtrip(t *testing.T) {
	tests := []int64{0, 1, -1, 2, -2, 127, -128, 32767, -32768, math.MaxInt64, math.MinInt64}
	for _, v := range tests {
		encoded := zigzagEncode(v)
		decoded := zigzagDecode(encoded)
		if decoded != v {
			t.Errorf("zigzag roundtrip %d: encoded=%d, decoded=%d", v, encoded, decoded)
		}
	}
}

func TestZigzagBigIntRoundtrip(t *testing.T) {
	tests := []*big.Int{
		big.NewInt(0),
		big.NewInt(1),
		big.NewInt(-1),
		big.NewInt(math.MaxInt64),
		big.NewInt(math.MinInt64),
	}
	// Add a value larger than int64
	large := new(big.Int)
	large.SetString("123456789012345678901234567890", 10)
	tests = append(tests, large)
	negLarge := new(big.Int).Neg(large)
	tests = append(tests, negLarge)

	for _, v := range tests {
		encoded := zigzagEncodeBigInt(v)
		decoded := zigzagDecodeBigInt(encoded)
		if decoded.Cmp(v) != 0 {
			t.Errorf("zigzag big roundtrip %v: decoded=%v", v, decoded)
		}
	}
}

func TestLEB128Roundtrip(t *testing.T) {
	tests := []uint64{0, 1, 127, 128, 16383, 16384, math.MaxUint64 >> 1}
	for _, v := range tests {
		dst := make([]byte, 16)
		n := encodeLEB128(dst, v)
		decoded, consumed, err := decodeLEB128(dst[:n])
		if err != nil {
			t.Errorf("LEB128 roundtrip %d: error %v", v, err)
			continue
		}
		if consumed != n {
			t.Errorf("LEB128 roundtrip %d: consumed %d, wrote %d", v, consumed, n)
		}
		if decoded != v {
			t.Errorf("LEB128 roundtrip %d: decoded=%d", v, decoded)
		}
	}
}

func TestLEB128BigIntRoundtrip(t *testing.T) {
	tests := []*big.Int{
		big.NewInt(0),
		big.NewInt(1),
		big.NewInt(127),
		big.NewInt(128),
	}
	large := new(big.Int)
	large.SetString("123456789012345678901234567890", 10)
	tests = append(tests, large)

	for _, v := range tests {
		dst := make([]byte, 64)
		n := encodeLEB128BigInt(dst, v)
		decoded, consumed, err := decodeLEB128BigInt(dst[:n])
		if err != nil {
			t.Errorf("LEB128 big roundtrip %v: error %v", v, err)
			continue
		}
		if consumed != n {
			t.Errorf("LEB128 big roundtrip %v: consumed %d, wrote %d", v, consumed, n)
		}
		if decoded.Cmp(v) != 0 {
			t.Errorf("LEB128 big roundtrip %v: decoded=%v", v, decoded)
		}
	}
}

// ============================================================================
// Float Optimization Tests
// ============================================================================

func TestCanUseFloat32(t *testing.T) {
	tests := []struct {
		value float64
		can   bool
	}{
		{0, true},
		{1, true},
		{-1, true},
		{0.1, false},                // not exactly representable in float32
		{0.5, true},
		{1.0000001192092896, true},  // smallest increment above 1 in float32
		{1.0000000000000002, false}, // smallest increment above 1 in float64
		{16777216, true},            // 2^24, exact in float32
		{16777217, false},           // 2^24 + 1, loses precision in float32
	}

	for _, tt := range tests {
		got := canUseFloat32(tt.value)
		if got != tt.can {
			t.Errorf("canUseFloat32(%v) = %v, want %v", tt.value, got, tt.can)
		}
	}
}

func TestCanUseInteger(t *testing.T) {
	tests := []struct {
		value   float64
		wantOK  bool
		wantInt int64
	}{
		{0, true, 0},
		{1, true, 1},
		{-1, true, -1},
		{100, true, 100},
		{-100, true, -100},
		{9007199254740992, true, 9007199254740992}, // 2^53, max safe integer
		{0.5, false, 0},
		{1.1, false, 0},
	}

	for _, tt := range tests {
		gotInt, gotOK := canUseInteger(tt.value)
		if gotOK != tt.wantOK {
			t.Errorf("canUseInteger(%v) ok = %v, want %v", tt.value, gotOK, tt.wantOK)
		}
		if gotOK && gotInt != tt.wantInt {
			t.Errorf("canUseInteger(%v) = %d, want %d", tt.value, gotInt, tt.wantInt)
		}
	}
}

func TestEncodeNumberOptimalFormat(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		wantType  byte
		wantBytes int
	}{
		// Small integers encode as value + 100
		{"small_int_zero", 0, 0x64, 1},        // 0 + 100 = 0x64
		{"small_int_100", 100, 0xc8, 1},       // 100 + 100 = 200 = 0xc8
		{"sint_101", 101, typeSintBase, 2},     // Prefer signed per spec when same size
		{"small_neg_minus1", -1, 0x63, 1},      // -1 + 100 = 99 = 0x63
		{"small_neg_minus100", -100, 0x00, 1},  // -100 + 100 = 0
		{"sint_minus101", -101, typeSintBase, 2},
		{"float32", 0.5, typeFloat32, 5},
		{"float64", 0.1, typeFloat64, 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := make([]byte, 16)
			n, err := encodeNumber(dst, tt.value)
			if err != nil {
				t.Errorf("encodeNumber(%v) error: %v", tt.value, err)
				return
			}
			if n != tt.wantBytes {
				t.Errorf("encodeNumber(%v) = %d bytes, want %d", tt.value, n, tt.wantBytes)
			}
			if dst[0] != tt.wantType {
				t.Errorf("encodeNumber(%v) type = 0x%02x, want 0x%02x", tt.value, dst[0], tt.wantType)
			}
		})
	}
}

func TestEncodeNumberRejectsNaNInf(t *testing.T) {
	dst := make([]byte, 16)

	tests := []float64{
		math.NaN(),
		math.Copysign(math.NaN(), -1),
		math.Inf(1),
		math.Inf(-1),
	}

	for _, v := range tests {
		_, err := encodeNumber(dst, v)
		if err == nil {
			t.Errorf("encodeNumber(%v) should return error", v)
		}
	}
}

// ============================================================================
// String Encoding Tests
// ============================================================================

func TestStringEncodingBoundary(t *testing.T) {
	t.Run("short_string_max", func(t *testing.T) {
		s := "123456789012345" // 15 bytes - max short string
		dst := make([]byte, 32)
		n := encodeString(dst, s)
		if n != 16 { // 1 type byte + 15 data bytes
			t.Errorf("15-byte string should encode to 16 bytes, got %d", n)
		}
		if dst[0] != typeShortStringBase+15 {
			t.Errorf("type byte should be 0x%02x, got 0x%02x", typeShortStringBase+15, dst[0])
		}
	})

	t.Run("long_string_min", func(t *testing.T) {
		s := "1234567890123456" // 16 bytes - min long string
		dst := make([]byte, 32)
		n := encodeString(dst, s)
		// Long string: 0xFF + 16 data bytes + 0xFF = 18
		if dst[0] != typeLongString {
			t.Errorf("type byte should be 0x%02x, got 0x%02x", typeLongString, dst[0])
		}
		if n != 18 {
			t.Errorf("16-byte string should encode to 18 bytes, got %d", n)
		}
	})

	t.Run("empty_string", func(t *testing.T) {
		dst := make([]byte, 8)
		n := encodeString(dst, "")
		if n != 1 {
			t.Errorf("empty string should encode to 1 byte, got %d", n)
		}
		if dst[0] != typeShortStringBase {
			t.Errorf("empty string type should be 0x%02x, got 0x%02x", typeShortStringBase, dst[0])
		}
	})
}

func TestDecodeLongStringFF(t *testing.T) {
	// Phase 2: long string is data bytes terminated by 0xFF
	data := []byte{'h', 'e', 'l', 'l', 'o', typeLongString}

	result, consumed, err := decodeLongString(data, 1000)
	if err != nil {
		t.Errorf("decodeLongString error: %v", err)
		return
	}
	if string(result) != "hello" {
		t.Errorf("decoded = %q, want %q", string(result), "hello")
	}
	if consumed != len(data) {
		t.Errorf("consumed = %d, want %d", consumed, len(data))
	}
}

func TestDecodeLongStringNoTerminator(t *testing.T) {
	// No terminating 0xFF - should error
	data := []byte{'h', 'e', 'l', 'l', 'o'}
	_, _, err := decodeLongString(data, 1000)
	if err == nil {
		t.Error("expected error for unterminated long string")
	}
}

func TestDecodeLongStringMaxLength(t *testing.T) {
	// String exceeds max length
	data := []byte{'a', 'b', 'c', 'd', 'e', typeLongString}
	_, _, err := decodeLongString(data, 3) // max 3 bytes
	if err == nil {
		t.Error("expected error for string exceeding max length")
	}
	if _, ok := err.(*MaxStringLengthError); !ok {
		t.Errorf("expected MaxStringLengthError, got %T: %v", err, err)
	}
}
