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

// ABOUTME: Tests for Go-specific security configuration options and modes.
// ABOUTME: Default security behavior is covered by universal spec tests.

package bonjson

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

// ============================================================================
// NUL Character Configuration Tests
// ============================================================================

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
// Max Depth Configuration Tests
// ============================================================================

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
// Max String Length Configuration Tests
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
// Max Chunks Configuration Tests
// ============================================================================

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
	dec.SetDuplicateKeyMode(DupKeyKeepLast)

	var v map[string]int
	err := dec.Decode(&v)
	if err != nil {
		t.Fatalf("DupKeyReplace should not error: %v", err)
	}

	if v["a"] != 2 {
		t.Errorf("expected a=2 (last value), got a=%d", v["a"])
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
		dec.SetDuplicateKeyMode(DupKeyKeepLast)

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
		dec.SetDuplicateKeyMode(DupKeyKeepLast)

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
// NaN/Infinity Handling Mode Tests
// ============================================================================

func TestNaNInfinityAllow(t *testing.T) {
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
			dec.SetNaNInfinityMode(NaNInfAllow)

			var v float64
			err := dec.Decode(&v)
			if err != nil {
				t.Fatalf("NaNInfAllow should not error: %v", err)
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

func TestNaNInfinityStringifyDecode(t *testing.T) {
	tests := []struct {
		name     string
		special  byte
		expected string
	}{
		{"quiet_nan", bigNumNaNQuiet, "NaN"},
		{"signaling_nan", bigNumNaNSignaling, "NaN"},
		{"positive_infinity", bigNumInfinity, "Infinity"},
		{"negative_infinity", bigNumInfinity | bigNumNegative, "-Infinity"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			buf.WriteByte(typeBigNumber)
			buf.WriteByte(tt.special)

			dec := NewDecoder(bytes.NewReader(buf.Bytes()))
			dec.SetNaNInfinityMode(NaNInfStringify)

			var v any
			err := dec.Decode(&v)
			if err != nil {
				t.Fatalf("NaNInfStringify should not error: %v", err)
			}

			s, ok := v.(string)
			if !ok {
				t.Fatalf("expected string, got %T: %v", v, v)
			}
			if s != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, s)
			}
		})
	}
}

func TestNaNInfinityStringifyDecodeFloat16(t *testing.T) {
	// Create a bfloat16 NaN value
	// bfloat16 NaN: sign=0, exp=0xFF, mantissa!=0
	// Using 0x7FC0 which is a common quiet NaN
	var buf bytes.Buffer
	buf.WriteByte(typeFloat16)
	buf.WriteByte(0xC0) // low byte
	buf.WriteByte(0x7F) // high byte (0x7FC0 = quiet NaN)

	dec := NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.SetNaNInfinityMode(NaNInfStringify)

	var v any
	err := dec.Decode(&v)
	if err != nil {
		t.Fatalf("NaNInfStringify should not error: %v", err)
	}

	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string, got %T: %v", v, v)
	}
	if s != "NaN" {
		t.Errorf("expected %q, got %q", "NaN", s)
	}
}

func TestNaNInfinityStringifyDecodeFloat32(t *testing.T) {
	// Create a float32 positive infinity
	// float32 +Inf: 0x7F800000
	var buf bytes.Buffer
	buf.WriteByte(typeFloat32)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x80)
	buf.WriteByte(0x7F)

	dec := NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.SetNaNInfinityMode(NaNInfStringify)

	var v any
	err := dec.Decode(&v)
	if err != nil {
		t.Fatalf("NaNInfStringify should not error: %v", err)
	}

	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string, got %T: %v", v, v)
	}
	if s != "Infinity" {
		t.Errorf("expected %q, got %q", "Infinity", s)
	}
}

func TestNaNInfinityStringifyDecodeFloat64(t *testing.T) {
	// Create a float64 negative infinity
	// float64 -Inf: 0xFFF0000000000000
	var buf bytes.Buffer
	buf.WriteByte(typeFloat64)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0xF0)
	buf.WriteByte(0xFF)

	dec := NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.SetNaNInfinityMode(NaNInfStringify)

	var v any
	err := dec.Decode(&v)
	if err != nil {
		t.Fatalf("NaNInfStringify should not error: %v", err)
	}

	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string, got %T: %v", v, v)
	}
	if s != "-Infinity" {
		t.Errorf("expected %q, got %q", "-Infinity", s)
	}
}

func TestNaNInfinityStringifyEncode(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		expected string
	}{
		{"nan", math.NaN(), "NaN"},
		{"positive_infinity", math.Inf(1), "Infinity"},
		{"negative_infinity", math.Inf(-1), "-Infinity"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := NewEncoder(&buf)
			enc.SetNaNInfinityMode(NaNInfStringify)

			err := enc.Encode(tt.value)
			if err != nil {
				t.Fatalf("NaNInfStringify encode should not error: %v", err)
			}

			// Decode the result
			var v string
			err = Unmarshal(buf.Bytes(), &v)
			if err != nil {
				t.Fatalf("failed to decode stringified value: %v", err)
			}

			if v != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, v)
			}
		})
	}
}

func TestNaNInfinityEncodeAllow(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		checkNaN bool
		checkInf int // 0 = not inf, 1 = +inf, -1 = -inf
	}{
		{"nan", math.NaN(), true, 0},
		{"positive_infinity", math.Inf(1), false, 1},
		{"negative_infinity", math.Inf(-1), false, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := NewEncoder(&buf)
			enc.SetNaNInfinityMode(NaNInfAllow)

			err := enc.Encode(tt.value)
			if err != nil {
				t.Fatalf("NaNInfAllow encode should not error: %v", err)
			}

			// Decode with allow mode to verify the value
			dec := NewDecoder(bytes.NewReader(buf.Bytes()))
			dec.SetNaNInfinityMode(NaNInfAllow)

			var v float64
			err = dec.Decode(&v)
			if err != nil {
				t.Fatalf("failed to decode: %v", err)
			}

			if tt.checkNaN {
				if !math.IsNaN(v) {
					t.Errorf("expected NaN, got %v", v)
				}
			} else if tt.checkInf != 0 {
				if !math.IsInf(v, tt.checkInf) {
					t.Errorf("expected Inf(%d), got %v", tt.checkInf, v)
				}
			}
		})
	}
}

func TestNaNInfinityDecodeFloat16Allow(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		checkNaN bool
		checkInf int
	}{
		// bfloat16 NaN: 0x7FC0
		{"nan", []byte{typeFloat16, 0xC0, 0x7F}, true, 0},
		// bfloat16 +Inf: 0x7F80
		{"positive_infinity", []byte{typeFloat16, 0x80, 0x7F}, false, 1},
		// bfloat16 -Inf: 0xFF80
		{"negative_infinity", []byte{typeFloat16, 0x80, 0xFF}, false, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec := NewDecoder(bytes.NewReader(tt.data))
			dec.SetNaNInfinityMode(NaNInfAllow)

			var v float64
			err := dec.Decode(&v)
			if err != nil {
				t.Fatalf("NaNInfAllow should not error: %v", err)
			}

			if tt.checkNaN {
				if !math.IsNaN(v) {
					t.Errorf("expected NaN, got %v", v)
				}
			} else if tt.checkInf != 0 {
				if !math.IsInf(v, tt.checkInf) {
					t.Errorf("expected Inf(%d), got %v", tt.checkInf, v)
				}
			}
		})
	}
}

func TestNaNInfinityDecodeFloat32Allow(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		checkNaN bool
		checkInf int
	}{
		// float32 NaN: 0x7FC00000
		{"nan", []byte{typeFloat32, 0x00, 0x00, 0xC0, 0x7F}, true, 0},
		// float32 +Inf: 0x7F800000
		{"positive_infinity", []byte{typeFloat32, 0x00, 0x00, 0x80, 0x7F}, false, 1},
		// float32 -Inf: 0xFF800000
		{"negative_infinity", []byte{typeFloat32, 0x00, 0x00, 0x80, 0xFF}, false, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec := NewDecoder(bytes.NewReader(tt.data))
			dec.SetNaNInfinityMode(NaNInfAllow)

			var v float64
			err := dec.Decode(&v)
			if err != nil {
				t.Fatalf("NaNInfAllow should not error: %v", err)
			}

			if tt.checkNaN {
				if !math.IsNaN(v) {
					t.Errorf("expected NaN, got %v", v)
				}
			} else if tt.checkInf != 0 {
				if !math.IsInf(v, tt.checkInf) {
					t.Errorf("expected Inf(%d), got %v", tt.checkInf, v)
				}
			}
		})
	}
}

func TestNaNInfinityDecodeFloat64Allow(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		checkNaN bool
		checkInf int
	}{
		// float64 NaN: 0x7FF8000000000000
		{"nan", []byte{typeFloat64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF8, 0x7F}, true, 0},
		// float64 +Inf: 0x7FF0000000000000
		{"positive_infinity", []byte{typeFloat64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x7F}, false, 1},
		// float64 -Inf: 0xFFF0000000000000
		{"negative_infinity", []byte{typeFloat64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0xFF}, false, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec := NewDecoder(bytes.NewReader(tt.data))
			dec.SetNaNInfinityMode(NaNInfAllow)

			var v float64
			err := dec.Decode(&v)
			if err != nil {
				t.Fatalf("NaNInfAllow should not error: %v", err)
			}

			if tt.checkNaN {
				if !math.IsNaN(v) {
					t.Errorf("expected NaN, got %v", v)
				}
			} else if tt.checkInf != 0 {
				if !math.IsInf(v, tt.checkInf) {
					t.Errorf("expected Inf(%d), got %v", tt.checkInf, v)
				}
			}
		})
	}
}
