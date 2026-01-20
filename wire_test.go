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

// ABOUTME: Tests for low-level wire format decoding error handling.
// ABOUTME: Basic encoding/decoding is covered by universal spec tests.

package bonjson

import (
	"math"
	"testing"
)

// ============================================================================
// Length Field Error Tests
// ============================================================================

func TestDecodeLengthFieldErrors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"truncated multi-byte", []byte{0x01}},
		{"truncated 9-byte", []byte{0xff, 0x01, 0x02}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := decodeLengthField(tt.data)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

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
		{"sint truncated", []byte{}, typeSintBase + 3},     // needs 4 bytes, has 0
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

	// Float16 needs 2 bytes of data (after type code)
	_, err = decodeFloat16([]byte{})
	if err == nil {
		t.Error("expected error for truncated float16")
	}
}

// ============================================================================
// Length Field Edge Case Tests
// ============================================================================

func TestLengthFieldBoundaryValues(t *testing.T) {
	tests := []struct {
		name         string
		payload      uint64
		continuation bool
		wantBytes    int
	}{
		// 1-byte encoding: payload fits in 7 bits (0-127)
		{"zero_no_continuation", 0, false, 1},
		{"zero_with_continuation", 0, true, 1},
		{"max_1_byte_payload", 63, false, 1},         // 63 << 1 = 126 (fits in 7 bits)
		{"max_1_byte_with_cont", 63, true, 1},        // 63 << 1 | 1 = 127
		{"boundary_to_2_byte", 64, false, 2},         // 64 << 1 = 128 (needs 8 bits)
		{"max_2_byte_payload", 8191, false, 2},       // 8191 << 1 = 16382 (fits in 14 bits)
		{"boundary_to_3_byte", 8192, false, 3},       // needs 15 bits
		{"max_3_byte_payload", 1048575, false, 3},    // fits in 21 bits
		{"boundary_to_4_byte", 1048576, false, 4},    // needs 22 bits
		{"max_7_byte", (1<<48)-1, false, 7},          // max 7-byte payload
		{"max_8_byte", (1<<55)-1, false, 8},          // max 8-byte payload
		{"requires_9_byte", uint64(1) << 56, false, 9}, // needs 9-byte encoding
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			dst := make([]byte, 16)
			n := encodeLengthField(dst, tt.payload, tt.continuation)
			if n != tt.wantBytes {
				t.Errorf("encodeLengthField() wrote %d bytes, want %d", n, tt.wantBytes)
			}

			// Decode and verify roundtrip
			length, cont, consumed, err := decodeLengthField(dst[:n])
			if err != nil {
				t.Errorf("decodeLengthField() error = %v", err)
				return
			}
			if consumed != n {
				t.Errorf("decodeLengthField() consumed %d bytes, want %d", consumed, n)
			}
			if length != tt.payload {
				t.Errorf("decodeLengthField() length = %d, want %d", length, tt.payload)
			}
			if cont != tt.continuation {
				t.Errorf("decodeLengthField() continuation = %v, want %v", cont, tt.continuation)
			}
		})
	}
}

func TestNonCanonicalLengthAcceptance(t *testing.T) {
	// Per BONJSON spec: "Decoders MUST accept non-canonical (oversized) length encodings"
	tests := []struct {
		name         string
		data         []byte
		wantLength   uint64
		wantCont     bool
		wantConsumed int
	}{
		// 2-byte encoding for value that fits in 1 byte
		// Payload 0 encoded as 2 bytes: [0x01, 0x00]
		{"zero_in_2_bytes", []byte{0x01, 0x00}, 0, false, 2},
		// 3-byte encoding for small value
		// Payload 0 encoded as 3 bytes: [0x03, 0x00, 0x00]
		{"zero_in_3_bytes", []byte{0x03, 0x00, 0x00}, 0, false, 3},
		// 9-byte encoding for value that fits in fewer bytes
		// Payload 0 encoded as 9 bytes: [0xff, 0x00, ...]
		{"zero_in_9_bytes", []byte{0xff, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, 0, false, 9},
		// 2-byte encoding for payload 2 (length=1, cont=false)
		// Canonical would be 0x04; non-canonical 2-byte: [0x09, 0x00]
		{"payload2_in_2_bytes", []byte{0x09, 0x00}, 1, false, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			length, cont, consumed, err := decodeLengthField(tt.data)
			if err != nil {
				t.Errorf("decodeLengthField() unexpected error = %v", err)
				return
			}
			if length != tt.wantLength {
				t.Errorf("length = %d, want %d", length, tt.wantLength)
			}
			if cont != tt.wantCont {
				t.Errorf("continuation = %v, want %v", cont, tt.wantCont)
			}
			if consumed != tt.wantConsumed {
				t.Errorf("consumed = %d, want %d", consumed, tt.wantConsumed)
			}
		})
	}
}

func TestLengthField9ByteEncoding(t *testing.T) {
	// Test the 9-byte encoding path (header 0xff + 8 bytes payload)
	largePayload := uint64(1) << 57 // Requires 9-byte encoding

	dst := make([]byte, 16)
	n := encodeLengthField(dst, largePayload, false)
	if n != 9 {
		t.Errorf("large payload should use 9 bytes, got %d", n)
	}
	if dst[0] != 0xff {
		t.Errorf("9-byte encoding should start with 0xff, got 0x%02x", dst[0])
	}

	// Decode and verify
	length, cont, consumed, err := decodeLengthField(dst[:n])
	if err != nil {
		t.Errorf("decodeLengthField() error = %v", err)
		return
	}
	if length != largePayload {
		t.Errorf("decoded length = %d, want %d", length, largePayload)
	}
	if cont != false {
		t.Error("continuation should be false")
	}
	if consumed != 9 {
		t.Errorf("should consume 9 bytes, got %d", consumed)
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

	t.Run("unsigned_byte_boundaries", func(t *testing.T) {
		tests := []struct {
			value     uint64
			wantBytes int
		}{
			{255, 2},                      // max 1-byte value: type + 1 byte
			{256, 3},                      // needs 2 bytes: type + 2 bytes
			{65535, 3},                    // max 2-byte value
			{65536, 4},                    // needs 3 bytes
			{0xFFFFFF, 4},                 // max 3-byte value
			{0x1000000, 5},                // needs 4 bytes
			{0xFFFFFFFF, 5},               // max 4-byte value
			{0x100000000, 6},              // needs 5 bytes
			{0xFFFFFFFFFF, 6},             // max 5-byte value
			{0x10000000000, 7},            // needs 6 bytes
			{0xFFFFFFFFFFFF, 7},           // max 6-byte value
			{0x1000000000000, 8},          // needs 7 bytes
			{0xFFFFFFFFFFFFFF, 8},         // max 7-byte value
			{0x100000000000000, 9},        // needs 8 bytes
			{0xFFFFFFFFFFFFFFFF, 9},       // max 8-byte value
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

	t.Run("signed_byte_boundaries", func(t *testing.T) {
		tests := []struct {
			value     int64
			wantBytes int
		}{
			{127, 2},                       // max positive 1-byte signed
			{128, 3},                       // needs 2 bytes (sign bit)
			{-128, 2},                      // min negative 1-byte signed
			{-129, 3},                      // needs 2 bytes
			{32767, 3},                     // max positive 2-byte signed
			{32768, 4},                     // needs 3 bytes
			{-32768, 3},                    // min negative 2-byte signed
			{-32769, 4},                    // needs 3 bytes
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

func TestSignExtension(t *testing.T) {
	tests := []struct {
		name    string
		val     uint64
		n       int
		want    int64
	}{
		{"1_byte_positive", 0x7F, 1, 127},
		{"1_byte_negative", 0x80, 1, -128},
		{"1_byte_minus_one", 0xFF, 1, -1},
		{"2_byte_positive", 0x7FFF, 2, 32767},
		{"2_byte_negative", 0x8000, 2, -32768},
		{"3_byte_positive", 0x7FFFFF, 3, 8388607},
		{"3_byte_negative", 0x800000, 3, -8388608},
		{"4_byte_positive", 0x7FFFFFFF, 4, 2147483647},
		{"4_byte_negative", 0x80000000, 4, -2147483648},
		{"8_byte_no_extension", 0x7FFFFFFFFFFFFFFF, 8, 9223372036854775807},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := signExtend(tt.val, tt.n)
			if got != tt.want {
				t.Errorf("signExtend(0x%X, %d) = %d, want %d", tt.val, tt.n, got, tt.want)
			}
		})
	}
}

func TestDecodeIntegerInvalidTypeCode(t *testing.T) {
	// Type codes that aren't integers
	invalidCodes := []byte{typeNull, typeFalse, typeTrue, typeFloat16, typeFloat32, typeFloat64}

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
// BigNumber Edge Case Tests
// ============================================================================

func TestBigNumberSpecialValues(t *testing.T) {
	t.Run("zero", func(t *testing.T) {
		// Zero: sigLen=0, expLen=0
		bn := &BigNumber{Significand: []byte{}, Exponent: 0, Negative: false}
		dst := make([]byte, 16)
		n := encodeBigNumber(dst, bn)

		decoded, special, _, err := decodeBigNumber(dst[1:n])
		if err != nil {
			t.Errorf("decode error: %v", err)
		}
		if special != BigNumNormal {
			t.Errorf("zero should be BigNumNormal, got %v", special)
		}
		if len(decoded.Significand) != 0 {
			t.Errorf("zero significand should be empty, got %v", decoded.Significand)
		}
	})

	t.Run("negative_zero", func(t *testing.T) {
		bn := &BigNumber{Significand: []byte{}, Exponent: 0, Negative: true}
		dst := make([]byte, 16)
		n := encodeBigNumber(dst, bn)

		decoded, special, _, err := decodeBigNumber(dst[1:n])
		if err != nil {
			t.Errorf("decode error: %v", err)
		}
		if special != BigNumNormal {
			t.Errorf("-zero should be BigNumNormal, got %v", special)
		}
		if !decoded.Negative {
			t.Error("-zero should preserve negative sign")
		}
	})

	t.Run("infinity", func(t *testing.T) {
		// Infinity: sigLen=0, expLen=1
		data := []byte{typeBigNumber, 0x02} // header: sigLen=0, expLen=1, negative=0
		_, special, _, err := decodeBigNumber(data[1:])
		if err != nil {
			t.Errorf("decode error: %v", err)
		}
		if special != BigNumInf {
			t.Errorf("expected BigNumInf, got %v", special)
		}
	})

	t.Run("negative_infinity", func(t *testing.T) {
		// -Infinity: sigLen=0, expLen=1, negative=1
		data := []byte{typeBigNumber, 0x03}
		decoded, special, _, err := decodeBigNumber(data[1:])
		if err != nil {
			t.Errorf("decode error: %v", err)
		}
		if special != BigNumInf {
			t.Errorf("expected BigNumInf, got %v", special)
		}
		if !decoded.Negative {
			t.Error("-infinity should be negative")
		}
	})

	t.Run("quiet_nan", func(t *testing.T) {
		// QNaN: sigLen=0, expLen=2
		data := []byte{typeBigNumber, 0x04}
		_, special, _, err := decodeBigNumber(data[1:])
		if err != nil {
			t.Errorf("decode error: %v", err)
		}
		if special != BigNumQNaN {
			t.Errorf("expected BigNumQNaN, got %v", special)
		}
	})

	t.Run("signaling_nan", func(t *testing.T) {
		// SNaN: sigLen=0, expLen=3
		data := []byte{typeBigNumber, 0x06}
		_, special, _, err := decodeBigNumber(data[1:])
		if err != nil {
			t.Errorf("decode error: %v", err)
		}
		if special != BigNumSNaN {
			t.Errorf("expected BigNumSNaN, got %v", special)
		}
	})
}

func TestBigNumberExponentLengths(t *testing.T) {
	tests := []struct {
		name    string
		exp     int32
		expLen  int
	}{
		{"zero_exponent", 0, 0},
		{"small_positive", 10, 1},
		{"small_negative", -10, 1},
		{"max_1_byte", 127, 1},
		{"min_1_byte", -128, 1},
		{"needs_2_bytes_positive", 128, 2},
		{"needs_2_bytes_negative", -129, 2},
		{"max_2_bytes", 32767, 2},
		{"min_2_bytes", -32768, 2},
		{"needs_4_bytes_positive", 32768, 3},
		{"needs_4_bytes_negative", -32769, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bn := &BigNumber{
				Significand: []byte{0x01}, // minimal non-zero significand
				Exponent:    tt.exp,
				Negative:    false,
			}

			dst := make([]byte, 32)
			n := encodeBigNumber(dst, bn)

			// Decode and verify exponent
			decoded, _, _, err := decodeBigNumber(dst[1:n])
			if err != nil {
				t.Errorf("decode error: %v", err)
				return
			}
			if decoded.Exponent != tt.exp {
				t.Errorf("exponent = %d, want %d", decoded.Exponent, tt.exp)
			}
		})
	}
}

func TestBigNumberMaxSignificand(t *testing.T) {
	// Max significand is 31 bytes (5 bits in header)
	maxSig := make([]byte, 31)
	for i := range maxSig {
		maxSig[i] = 0xFF
	}

	bn := &BigNumber{
		Significand: maxSig,
		Exponent:    0,
		Negative:    true,
	}

	dst := make([]byte, 64)
	n := encodeBigNumber(dst, bn)

	decoded, _, consumed, err := decodeBigNumber(dst[1:n])
	if err != nil {
		t.Errorf("decode error: %v", err)
		return
	}
	if consumed != n-1 {
		t.Errorf("consumed %d bytes, expected %d", consumed, n-1)
	}
	if len(decoded.Significand) != 31 {
		t.Errorf("significand length = %d, want 31", len(decoded.Significand))
	}
	if !decoded.Negative {
		t.Error("negative flag not preserved")
	}
	for i, b := range decoded.Significand {
		if b != 0xFF {
			t.Errorf("significand[%d] = 0x%02x, want 0xFF", i, b)
		}
	}
}

func TestBigNumberTruncatedData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"header_only_with_exp", []byte{0x0A}},      // needs 1 exp byte
		{"header_only_with_sig", []byte{0x08}},      // needs 1 sig byte
		{"partial_exponent", []byte{0x0C, 0x01}},    // 2-byte exp, only 1 provided
		{"partial_significand", []byte{0x10, 0x01}}, // 2-byte sig, only 1 provided
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

// ============================================================================
// Float Optimization Tests
// ============================================================================

func TestCanUseBFloat16(t *testing.T) {
	tests := []struct {
		value float64
		can   bool
	}{
		{0, true},
		{1, true},
		{-1, true},
		{0.5, true},
		{1.5, true},
		{256, true},
		{0.00390625, true},            // 2^-8, representable in bfloat16
		{1.0078125, true},             // 1 + 2^-7, mantissa bit 16 set, lower 16 bits are 0
		{1.0000152587890625, false},   // 1 + 2^-16, requires lower mantissa bits
		{0.1, false},                  // not exactly representable
		{3.141592653589793, false},    // pi needs full precision
	}

	for _, tt := range tests {
		got := canUseBFloat16(tt.value)
		if got != tt.can {
			t.Errorf("canUseBFloat16(%v) = %v, want %v", tt.value, got, tt.can)
		}
	}
}

func TestCanUseFloat32(t *testing.T) {
	tests := []struct {
		value float64
		can   bool
	}{
		{0, true},
		{1, true},
		{-1, true},
		{0.1, false},                      // not exactly representable in float32
		{0.5, true},
		{1.0000001192092896, true},        // smallest increment above 1 in float32
		{1.0000000000000002, false},       // smallest increment above 1 in float64
		{16777216, true},                  // 2^24, exact in float32
		{16777217, false},                 // 2^24 + 1, loses precision in float32
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
		{9007199254740992, true, 9007199254740992},   // 2^53, max safe integer
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
		{"small_int_zero", 0, 0x64, 1},      // 0 + 100 = 0x64
		{"small_int_100", 100, 0xc8, 1},     // 100 + 100 = 200 = 0xc8
		{"sint_101", 101, typeSintBase, 2},  // Prefer signed per spec when same size
		{"small_neg_minus1", -1, 0x63, 1},   // -1 + 100 = 99 = 0x63
		{"small_neg_minus100", -100, 0x00, 1}, // -100 + 100 = 0
		{"sint_minus101", -101, typeSintBase, 2},
		{"bfloat16", 0.5, typeFloat16, 3},
		{"float32", 1.0000001192092896, typeFloat32, 5},
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
		positiveNaN(),
		negativeNaN(),
		positiveInf(),
		negativeInf(),
	}

	for _, v := range tests {
		_, err := encodeNumber(dst, v)
		if err == nil {
			t.Errorf("encodeNumber(%v) should return error", v)
		}
	}
}

// Helper functions for NaN/Inf
func positiveNaN() float64 {
	return math.NaN()
}

func negativeNaN() float64 {
	return math.Copysign(math.NaN(), -1)
}

func positiveInf() float64 {
	return math.Inf(1)
}

func negativeInf() float64 {
	return math.Inf(-1)
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
		// Long string: 1 type byte + 1 length byte (16<<1 = 32) + 16 data bytes = 18
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

func TestDecodeLongStringChunked(t *testing.T) {
	// Manually construct a chunked string: "hello" + "world"
	// First chunk: length=5, continuation=1: (5 << 1) | 1 = 11 -> byte 0x16
	// Then "hello"
	// Second chunk: length=5, continuation=0: 5 << 1 = 10 -> byte 0x14
	// Then "world"
	data := []byte{
		0x16, 'h', 'e', 'l', 'l', 'o', // chunk 1: 5 bytes with continuation
		0x14, 'w', 'o', 'r', 'l', 'd', // chunk 2: 5 bytes, no continuation
	}

	result, consumed, err := decodeLongString(data, 100, 1000)
	if err != nil {
		t.Errorf("decodeLongString error: %v", err)
		return
	}
	if string(result) != "helloworld" {
		t.Errorf("decoded = %q, want %q", string(result), "helloworld")
	}
	if consumed != len(data) {
		t.Errorf("consumed = %d, want %d", consumed, len(data))
	}
}

func TestDecodeLongStringEmptyChunkContinuation(t *testing.T) {
	// 0x02 = length 0 with continuation bit set - should be rejected
	data := []byte{0x02}
	_, _, err := decodeLongString(data, 100, 1000)
	if err == nil {
		t.Error("expected error for empty chunk with continuation")
	}
	if _, ok := err.(*EmptyChunkContinuationError); !ok {
		t.Errorf("expected EmptyChunkContinuationError, got %T: %v", err, err)
	}
}
