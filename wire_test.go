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
