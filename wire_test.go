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
//

package bonjson

import (
	"math"
	"testing"
)

// ============================================================================
// Length Field Tests
// ============================================================================

func TestEncodeLengthField(t *testing.T) {
	tests := []struct {
		name         string
		length       uint64
		continuation bool
		want         []byte
	}{
		{"zero no-cont", 0, false, []byte{0x01}},
		{"zero cont", 0, true, []byte{0x03}},
		{"1 no-cont", 1, false, []byte{0x05}},
		{"1 cont", 1, true, []byte{0x07}},
		{"63 no-cont", 63, false, []byte{0xfd}},
		{"64 no-cont", 64, false, []byte{0x02, 0x02}},
		{"127 no-cont", 127, false, []byte{0x02, 0x04}},
		{"128 no-cont", 128, false, []byte{0x02, 0x04}},
		{"large value", 0x123456, false, []byte{0x58, 0x68, 0x91, 0x04}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf [16]byte
			n := encodeLengthField(buf[:], tt.length, tt.continuation)
			got := buf[:n]

			// Verify roundtrip
			length, cont, _, err := decodeLengthField(got)
			if err != nil {
				t.Fatalf("decodeLengthField error: %v", err)
			}
			if length != tt.length {
				t.Errorf("roundtrip length = %d, want %d", length, tt.length)
			}
			if cont != tt.continuation {
				t.Errorf("roundtrip continuation = %v, want %v", cont, tt.continuation)
			}
		})
	}
}

func TestDecodeLengthFieldErrors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"truncated multi-byte", []byte{0x02}},
		{"truncated 9-byte", []byte{0x00, 0x01, 0x02}},
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
// Integer Encoding Tests
// ============================================================================

func TestEncodeSignedInt(t *testing.T) {
	tests := []struct {
		name  string
		value int64
	}{
		{"zero", 0},
		{"one", 1},
		{"small_max", 100},
		{"small_min", -100},
		{"byte_max", 127},
		{"byte_min", -128},
		{"beyond_small_pos", 101},
		{"beyond_small_neg", -101},
		{"int16_max", 32767},
		{"int16_min", -32768},
		{"int32_max", 2147483647},
		{"int32_min", -2147483648},
		{"int64_max", math.MaxInt64},
		{"int64_min", math.MinInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf [16]byte
			n := encodeSignedInt(buf[:], tt.value)

			// Decode and verify
			typeCode := buf[0]
			got, _, _, err := decodeInteger(buf[1:n], typeCode)
			if err != nil {
				t.Fatalf("decodeInteger error: %v", err)
			}
			if got != tt.value {
				t.Errorf("roundtrip = %d, want %d", got, tt.value)
			}
		})
	}
}

func TestEncodeUnsignedInt(t *testing.T) {
	tests := []struct {
		name  string
		value uint64
	}{
		{"zero", 0},
		{"one", 1},
		{"small_max", 100},
		{"beyond_small", 101},
		{"uint8_max", 255},
		{"uint16_max", 65535},
		{"uint32_max", 4294967295},
		{"uint64_max", math.MaxUint64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf [16]byte
			n := encodeUnsignedInt(buf[:], tt.value)

			// Decode and verify
			typeCode := buf[0]
			_, got, _, err := decodeInteger(buf[1:n], typeCode)
			if err != nil {
				t.Fatalf("decodeInteger error: %v", err)
			}
			if got != tt.value {
				t.Errorf("roundtrip = %d, want %d", got, tt.value)
			}
		})
	}
}

// ============================================================================
// Float Encoding Tests
// ============================================================================

func TestEncodeFloat64(t *testing.T) {
	tests := []struct {
		name  string
		value float64
	}{
		{"zero", 0.0},
		{"one", 1.0},
		{"negative", -1.0},
		{"pi", 3.14159265358979323846},
		{"small", 1e-10},
		{"large", 1e100},
		{"negative_large", -1e100},
		{"max_float64", math.MaxFloat64},
		{"min_float64", math.SmallestNonzeroFloat64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf [16]byte
			n := encodeFloat64(buf[:], tt.value)

			// Decode and verify (skip type code byte)
			got, err := decodeFloat64(buf[1:n])
			if err != nil {
				t.Fatalf("decodeFloat64 error: %v", err)
			}
			if got != tt.value {
				t.Errorf("roundtrip = %v, want %v", got, tt.value)
			}
		})
	}
}

func TestEncodeFloat32(t *testing.T) {
	tests := []struct {
		name  string
		value float32
	}{
		{"zero", 0.0},
		{"one", 1.0},
		{"negative", -1.0},
		{"pi", 3.14159},
		{"max_float32", math.MaxFloat32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf [16]byte
			n := encodeFloat32(buf[:], tt.value)

			// Decode and verify (skip type code byte)
			got, err := decodeFloat32(buf[1:n])
			if err != nil {
				t.Fatalf("decodeFloat32 error: %v", err)
			}
			if float32(got) != tt.value {
				t.Errorf("roundtrip = %v, want %v", got, tt.value)
			}
		})
	}
}

func TestEncodeFloat16(t *testing.T) {
	tests := []struct {
		name  string
		value float32
		want  float32 // bfloat16 has limited precision
	}{
		{"zero", 0.0, 0.0},
		{"one", 1.0, 1.0},
		{"negative", -1.0, -1.0},
		{"two", 2.0, 2.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf [16]byte
			n := encodeFloat16(buf[:], tt.value)

			// Decode and verify (skip type code byte)
			got, err := decodeFloat16(buf[1:n])
			if err != nil {
				t.Fatalf("decodeFloat16 error: %v", err)
			}
			if float32(got) != tt.want {
				t.Errorf("roundtrip = %v, want %v", got, tt.want)
			}
		})
	}
}

// ============================================================================
// String Encoding Tests
// ============================================================================

func TestEncodeString(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"short_1", "a"},
		{"short_15", "123456789012345"},
		{"long_16", "1234567890123456"},
		{"unicode", "Hello, 世界!"},
		{"emoji", "😀🎉"},
		{"long", string(make([]byte, 1000))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, len(tt.value)+20) // enough space
			n := encodeString(buf, tt.value)

			// Verify we can decode it (basic check)
			data := buf[:n]
			if len(data) > 0 {
				typeCode := data[0]
				if typeCode >= typeShortStringBase && typeCode <= typeShortStringBase+15 {
					// Short string
					strLen := int(typeCode & 0x0f)
					if strLen != len(tt.value) && len(tt.value) <= 15 {
						t.Errorf("short string length mismatch: got %d, want %d", strLen, len(tt.value))
					}
				} else if typeCode == typeLongString {
					// Long string - more complex to verify
				}
			}
		})
	}
}

// ============================================================================
// Type Code Tests
// ============================================================================

func TestTypeCodeRanges(t *testing.T) {
	// Verify type code constants are correct
	if typeSmallIntMin != 0x00 {
		t.Errorf("typeSmallIntMin = 0x%02x, want 0x00", typeSmallIntMin)
	}
	if typeSmallIntMax != 0x64 {
		t.Errorf("typeSmallIntMax = 0x%02x, want 0x64", typeSmallIntMax)
	}
	if typeLongString != 0x68 {
		t.Errorf("typeLongString = 0x%02x, want 0x68", typeLongString)
	}
	if typeNull != 0x6d {
		t.Errorf("typeNull = 0x%02x, want 0x6d", typeNull)
	}
	if typeFalse != 0x6e {
		t.Errorf("typeFalse = 0x%02x, want 0x6e", typeFalse)
	}
	if typeTrue != 0x6f {
		t.Errorf("typeTrue = 0x%02x, want 0x6f", typeTrue)
	}
	if typeUintBase != 0x70 {
		t.Errorf("typeUintBase = 0x%02x, want 0x70", typeUintBase)
	}
	if typeSintBase != 0x78 {
		t.Errorf("typeSintBase = 0x%02x, want 0x78", typeSintBase)
	}
	if typeShortStringBase != 0x80 {
		t.Errorf("typeShortStringBase = 0x%02x, want 0x80", typeShortStringBase)
	}
	if typeArrayStart != 0x99 {
		t.Errorf("typeArrayStart = 0x%02x, want 0x99", typeArrayStart)
	}
	if typeObjectStart != 0x9a {
		t.Errorf("typeObjectStart = 0x%02x, want 0x9a", typeObjectStart)
	}
	if typeContainerEnd != 0x9b {
		t.Errorf("typeContainerEnd = 0x%02x, want 0x9b", typeContainerEnd)
	}
	if typeSmallNegIntMin != 0x9c {
		t.Errorf("typeSmallNegIntMin = 0x%02x, want 0x9c", typeSmallNegIntMin)
	}
	if typeSmallNegIntMax != 0xff {
		t.Errorf("typeSmallNegIntMax = 0x%02x, want 0xff", typeSmallNegIntMax)
	}
}

// Test small int encoding values
func TestSmallIntTypeCodes(t *testing.T) {
	// Positive small ints 0-100 should encode as type codes 0x00-0x64
	for i := int64(0); i <= 100; i++ {
		var buf [16]byte
		n := encodeSignedInt(buf[:], i)
		if n != 1 {
			t.Errorf("encodeSignedInt(%d) wrote %d bytes, want 1", i, n)
		}
		if buf[0] != byte(i) {
			t.Errorf("encodeSignedInt(%d) = 0x%02x, want 0x%02x", i, buf[0], byte(i))
		}
	}

	// Negative small ints -1 to -100 should encode as type codes 0xff to 0x9c
	for i := int64(-1); i >= -100; i-- {
		var buf [16]byte
		n := encodeSignedInt(buf[:], i)
		if n != 1 {
			t.Errorf("encodeSignedInt(%d) wrote %d bytes, want 1", i, n)
		}
		expected := byte(int8(i))
		if buf[0] != expected {
			t.Errorf("encodeSignedInt(%d) = 0x%02x, want 0x%02x", i, buf[0], expected)
		}
	}
}

// ============================================================================
// Edge Case Tests
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
