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
	// Create moderately nested arrays using chunked format
	depth := 50
	var buf bytes.Buffer

	// Each nested array except the innermost contains one element (another array)
	for i := 0; i < depth-1; i++ {
		buf.WriteByte(typeArray)
	}
	// Innermost array contains just null
	buf.WriteByte(typeArray)
	buf.WriteByte(typeNull)
	buf.WriteByte(typeContainerEnd)
	for i := 0; i < depth-1; i++ {
		buf.WriteByte(typeContainerEnd)
	}

	// With default depth (500), this should succeed
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
	// Object: {"a": 1, "a": 2} - 2 pairs with duplicate key
	var buf bytes.Buffer
	buf.WriteByte(typeObject)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x65) // value 1 (small int: 0x64+1)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x66) // value 2 (small int: 0x64+2)
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
	// Object: {"a": 1, "a": 2} - 2 pairs with duplicate key
	var buf bytes.Buffer
	buf.WriteByte(typeObject)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x65) // value 1 (small int: 0x64+1)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x66) // value 2 (small int: 0x64+2)
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

	// Object: {"a": 1, "a": 2} - 2 pairs with duplicate key
	var buf bytes.Buffer
	buf.WriteByte(typeObject)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x65) // value 1 (small int: 0x64+1)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x66) // value 2 (small int: 0x64+2)
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
	// Object: {"a": 1, "a": 2} - 2 pairs with duplicate key
	var buf bytes.Buffer
	buf.WriteByte(typeObject)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x65) // value 1 (small int: 0x64+1)
	buf.WriteByte(typeShortStringBase + 1)
	buf.WriteByte('a')
	buf.WriteByte(0x66) // value 2 (small int: 0x64+2)
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
			// Encode with allow mode
			var buf bytes.Buffer
			enc := NewEncoder(&buf)
			enc.SetNaNInfinityMode(NaNInfAllow)
			if err := enc.Encode(tt.value); err != nil {
				t.Fatalf("NaNInfAllow encode should not error: %v", err)
			}

			dec := NewDecoder(bytes.NewReader(buf.Bytes()))
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

func TestNaNInfinityStringifyDecode(t *testing.T) {
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
			// Encode with allow mode to get raw float bytes
			var buf bytes.Buffer
			enc := NewEncoder(&buf)
			enc.SetNaNInfinityMode(NaNInfAllow)
			if err := enc.Encode(tt.value); err != nil {
				t.Fatalf("encode error: %v", err)
			}

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

// ============================================================================
// Configuration Interaction Tests
// ============================================================================

// Test multiple decoder configurations combined
func TestCombinedDecoderConfigurations(t *testing.T) {
	t.Run("nul_and_utf8_together", func(t *testing.T) {
		// String with NUL and invalid UTF-8
		// 0x80 alone is invalid UTF-8, then NUL
		invalidData := []byte{typeShortStringBase + 3, 0x80, 0x00, 'a'}

		// With AllowNUL and UTF8Replace
		dec := NewDecoder(bytes.NewReader(invalidData))
		dec.AllowNUL()
		dec.SetInvalidUTF8Mode(UTF8Replace)

		var s string
		err := dec.Decode(&s)
		if err != nil {
			t.Errorf("AllowNUL + UTF8Replace should succeed: %v", err)
		}
	})

	t.Run("depth_and_string_limits", func(t *testing.T) {
		// Create nested structure with strings
		data, _ := Marshal(map[string]any{
			"nested": map[string]string{
				"key": "short",
			},
		})

		dec := NewDecoder(bytes.NewReader(data))
		dec.SetMaxDepth(10)
		dec.SetMaxStringLength(1000)

		var v any
		if err := dec.Decode(&v); err != nil {
			t.Errorf("should decode within limits: %v", err)
		}
	})

	t.Run("all_relaxed_modes", func(t *testing.T) {
		// Test all relaxed modes together
		data, _ := Marshal(map[string]any{"a": 1, "b": 2})

		dec := NewDecoder(bytes.NewReader(data))
		dec.AllowNUL()
		dec.SetInvalidUTF8Mode(UTF8Ignore)
		dec.SetDuplicateKeyMode(DupKeyKeepLast)
		dec.SetNaNInfinityMode(NaNInfAllow)

		var v map[string]any
		if err := dec.Decode(&v); err != nil {
			t.Errorf("all relaxed modes should work: %v", err)
		}
	})
}

// Test limit edge values
func TestLimitEdgeValues(t *testing.T) {
	t.Run("depth_at_limit", func(t *testing.T) {
		// Create structure exactly at depth limit using delimiter-terminated format
		const depth = 5
		var buf bytes.Buffer
		// Each nested array (except innermost) contains one element
		for i := 0; i < depth-1; i++ {
			buf.WriteByte(typeArray)
		}
		// Innermost array contains one value
		buf.WriteByte(typeArray)
		buf.WriteByte(0x65) // value 1 (small int: 0x64+1)
		// Close all arrays
		for i := 0; i < depth; i++ {
			buf.WriteByte(typeContainerEnd)
		}

		dec := NewDecoder(&buf)
		dec.SetMaxDepth(depth)

		var v any
		err := dec.Decode(&v)
		if err != nil {
			t.Errorf("depth %d should succeed with limit %d: %v", depth, depth, err)
		}
	})

	t.Run("depth_one_over_limit", func(t *testing.T) {
		// Create structure one over depth limit using delimiter-terminated format
		const limit = 5
		const depth = limit + 1
		var buf bytes.Buffer
		// Each nested array (except innermost) contains one element
		for i := 0; i < depth-1; i++ {
			buf.WriteByte(typeArray)
		}
		// Innermost array contains one value
		buf.WriteByte(typeArray)
		buf.WriteByte(0x65) // value 1 (small int: 0x64+1)
		// Close all arrays
		for i := 0; i < depth; i++ {
			buf.WriteByte(typeContainerEnd)
		}

		dec := NewDecoder(bytes.NewReader(buf.Bytes()))
		dec.SetMaxDepth(limit)

		var v any
		err := dec.Decode(&v)
		if err == nil {
			t.Error("depth over limit should fail")
		}
	})

	t.Run("string_at_limit", func(t *testing.T) {
		// Create string exactly at limit
		const limit = 10
		s := strings.Repeat("x", limit)
		data, _ := Marshal(s)

		dec := NewDecoder(bytes.NewReader(data))
		dec.SetMaxStringLength(limit)

		var decoded string
		err := dec.Decode(&decoded)
		if err != nil {
			t.Errorf("string at limit should succeed: %v", err)
		}
	})

	t.Run("string_one_over_limit", func(t *testing.T) {
		// Create string one over limit - must be >15 chars to use long string encoding
		// (short strings ≤15 chars bypass length limit check)
		const limit = 20
		s := strings.Repeat("x", limit+1)
		data, _ := Marshal(s)

		dec := NewDecoder(bytes.NewReader(data))
		dec.SetMaxStringLength(limit)

		var decoded string
		err := dec.Decode(&decoded)
		if err == nil {
			t.Error("string over limit should fail")
		}
	})

	t.Run("zero_string_length_unlimited", func(t *testing.T) {
		// Zero string length limit means unlimited
		longStr := strings.Repeat("x", 1000)
		data, _ := Marshal(longStr)

		dec := NewDecoder(bytes.NewReader(data))
		dec.SetMaxStringLength(0) // Unlimited

		var v string
		err := dec.Decode(&v)
		if err != nil {
			t.Errorf("zero string length limit should be unlimited: %v", err)
		}
	})

}

// Test encoder configuration
func TestEncoderConfigurationModes(t *testing.T) {
	t.Run("nan_stringify_mode", func(t *testing.T) {
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		enc.SetNaNInfinityMode(NaNInfStringify)

		err := enc.Encode(math.NaN())
		if err != nil {
			t.Fatalf("NaNInfStringify encode error: %v", err)
		}

		// Should be encoded as string "NaN"
		dec := NewDecoder(&buf)
		var v any
		dec.Decode(&v)
		if s, ok := v.(string); !ok || s != "NaN" {
			t.Errorf("expected string \"NaN\", got %v (%T)", v, v)
		}
	})

	t.Run("infinity_stringify_mode", func(t *testing.T) {
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		enc.SetNaNInfinityMode(NaNInfStringify)

		enc.Encode(math.Inf(1))

		dec := NewDecoder(&buf)
		var v any
		dec.Decode(&v)
		if s, ok := v.(string); !ok || s != "Infinity" {
			t.Errorf("expected string \"Infinity\", got %v (%T)", v, v)
		}
	})

	t.Run("negative_infinity_stringify", func(t *testing.T) {
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		enc.SetNaNInfinityMode(NaNInfStringify)

		enc.Encode(math.Inf(-1))

		dec := NewDecoder(&buf)
		var v any
		dec.Decode(&v)
		if s, ok := v.(string); !ok || s != "-Infinity" {
			t.Errorf("expected string \"-Infinity\", got %v (%T)", v, v)
		}
	})

	t.Run("nan_allow_mode_roundtrip", func(t *testing.T) {
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		enc.SetNaNInfinityMode(NaNInfAllow)

		err := enc.Encode(math.NaN())
		if err != nil {
			t.Fatalf("NaNInfAllow encode error: %v", err)
		}

		dec := NewDecoder(&buf)
		dec.SetNaNInfinityMode(NaNInfAllow)
		var v float64
		dec.Decode(&v)
		if !math.IsNaN(v) {
			t.Errorf("expected NaN, got %v", v)
		}
	})
}

// Test that configurations are independent across decoders
func TestConfigurationIndependence(t *testing.T) {
	data, _ := Marshal("test")

	// Create two decoders with different configurations
	dec1 := NewDecoder(bytes.NewReader(data))
	dec1.AllowNUL()
	dec1.SetMaxDepth(5)

	dec2 := NewDecoder(bytes.NewReader(data))
	dec2.SetMaxDepth(100)

	// Configurations should be independent
	var v1, v2 string
	if err := dec1.Decode(&v1); err != nil {
		t.Errorf("dec1 decode error: %v", err)
	}
	if err := dec2.Decode(&v2); err != nil {
		t.Errorf("dec2 decode error: %v", err)
	}
}

// Test encoder and decoder mode consistency
func TestEncoderDecoderModeConsistency(t *testing.T) {
	t.Run("both_allow_nan", func(t *testing.T) {
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		enc.SetNaNInfinityMode(NaNInfAllow)
		enc.Encode(math.NaN())
		enc.Encode(math.Inf(1))
		enc.Encode(math.Inf(-1))

		dec := NewDecoder(&buf)
		dec.SetNaNInfinityMode(NaNInfAllow)

		var nan, posInf, negInf float64
		dec.Decode(&nan)
		dec.Decode(&posInf)
		dec.Decode(&negInf)

		if !math.IsNaN(nan) {
			t.Errorf("expected NaN, got %v", nan)
		}
		if !math.IsInf(posInf, 1) {
			t.Errorf("expected +Inf, got %v", posInf)
		}
		if !math.IsInf(negInf, -1) {
			t.Errorf("expected -Inf, got %v", negInf)
		}
	})

	t.Run("encoder_stringify_decoder_relaxed", func(t *testing.T) {
		// Encoder stringifies, decoder should get strings
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		enc.SetNaNInfinityMode(NaNInfStringify)
		enc.Encode(math.NaN())

		dec := NewDecoder(&buf)
		var v any
		dec.Decode(&v)

		// Should be a string even without special decoder config
		if s, ok := v.(string); !ok || s != "NaN" {
			t.Errorf("expected string \"NaN\", got %v (%T)", v, v)
		}
	})
}

// Test DisallowUnknownFields combined with other options
func TestDisallowUnknownFieldsCombined(t *testing.T) {
	type KnownFields struct {
		Name  string `bonjson:"name"`
		Value int    `bonjson:"value"`
	}

	t.Run("with_duplicate_key_keepfirst", func(t *testing.T) {
		// Object with unknown field and duplicate
		data, _ := Marshal(map[string]any{
			"name":    "test",
			"value":   42,
			"unknown": "should fail",
		})

		dec := NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		dec.SetDuplicateKeyMode(DupKeyKeepFirst)

		var v KnownFields
		err := dec.Decode(&v)
		if err == nil {
			t.Error("DisallowUnknownFields should still fail with unknown field")
		}
	})

	t.Run("known_fields_only", func(t *testing.T) {
		data, _ := Marshal(map[string]any{
			"name":  "test",
			"value": 42,
		})

		dec := NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		dec.SetDuplicateKeyMode(DupKeyKeepFirst)
		dec.SetInvalidUTF8Mode(UTF8Ignore)

		var v KnownFields
		err := dec.Decode(&v)
		if err != nil {
			t.Errorf("should succeed with only known fields: %v", err)
		}
		if v.Name != "test" || v.Value != 42 {
			t.Errorf("unexpected values: %+v", v)
		}
	})
}

// Test Valid() with security-relevant data
func TestValidWithSecurityData(t *testing.T) {
	t.Run("nan_invalid_by_default", func(t *testing.T) {
		// NaN in BigNumber format: 0x69 (BigNumber) + 0x02 (sigLen=0, expLen=0, special=NaN)
		// Actually, NaN is sigLen=0, expLen=0, negative=0, with header indicating NaN
		// Header byte: [sig_len:5][exp_len:2][negative:1]
		// For NaN: sigLen=0, but need to check special value encoding
		// Let's use float64 NaN: 0x6c + 8 bytes of NaN
		nanBytes := []byte{typeFloat64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf8, 0x7f}
		if Valid(nanBytes) {
			t.Error("Valid should return false for NaN by default")
		}
	})

	t.Run("infinity_invalid_by_default", func(t *testing.T) {
		// Positive infinity: 0x6c + 8 bytes
		infBytes := []byte{typeFloat64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x7f}
		if Valid(infBytes) {
			t.Error("Valid should return false for Infinity by default")
		}
	})

	t.Run("duplicate_keys_invalid_by_default", func(t *testing.T) {
		// Object with duplicate keys: {"a": 1, "a": 2} using delimiter-terminated format
		dupKeyData := []byte{
			typeObject,
			typeShortStringBase + 1, 'a', 0x65, // "a": 1 (small int: 0x64+1)
			typeShortStringBase + 1, 'a', 0x66, // "a": 2 (small int: 0x64+2)
			typeContainerEnd,
		}
		if Valid(dupKeyData) {
			t.Error("Valid should return false for duplicate keys by default")
		}
	})

	t.Run("deeply_nested_valid", func(t *testing.T) {
		// Deep nesting but within limits using delimiter-terminated format
		var buf bytes.Buffer
		// Each nested array (except innermost) contains one element
		for i := 0; i < 99; i++ {
			buf.WriteByte(typeArray)
		}
		// Innermost array contains null
		buf.WriteByte(typeArray)
		buf.WriteByte(typeNull)
		// Close all 100 arrays
		for i := 0; i < 100; i++ {
			buf.WriteByte(typeContainerEnd)
		}
		if !Valid(buf.Bytes()) {
			t.Error("Valid should return true for deeply nested but valid data")
		}
	})

	t.Run("nul_invalid_by_default", func(t *testing.T) {
		// String containing NUL
		nulData := []byte{typeShortStringBase + 3, 'a', 0x00, 'b'}
		if Valid(nulData) {
			t.Error("Valid should return false for NUL in string by default")
		}
	})
}

// Test Token() with security configuration modes
func TestTokenWithSecurityModes(t *testing.T) {
	t.Run("token_with_nan_allow", func(t *testing.T) {
		// Encode NaN with allow mode
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		enc.SetNaNInfinityMode(NaNInfAllow)
		enc.Encode(math.NaN())

		dec := NewDecoder(&buf)
		dec.SetNaNInfinityMode(NaNInfAllow)

		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("Token error: %v", err)
		}
		if f, ok := tok.(float64); !ok || !math.IsNaN(f) {
			t.Errorf("expected NaN float64, got %v (%T)", tok, tok)
		}
	})

	t.Run("token_with_nan_stringify", func(t *testing.T) {
		// Encode NaN with stringify mode - should come back as string
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		enc.SetNaNInfinityMode(NaNInfStringify)
		enc.Encode(math.NaN())

		dec := NewDecoder(&buf)
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("Token error: %v", err)
		}
		if s, ok := tok.(string); !ok || s != "NaN" {
			t.Errorf("expected string \"NaN\", got %v (%T)", tok, tok)
		}
	})

	t.Run("token_returns_raw_strings", func(t *testing.T) {
		// Token() is a lower-level API that returns raw strings
		// without UTF-8 processing. This test verifies that behavior.
		data := []byte{
			typeObject,
			typeShortStringBase + 2, 0x80, 'a', // Invalid UTF-8 key
			0x65, // value 1 (small int: 0x64+1)
			typeContainerEnd,
		}

		dec := NewDecoder(bytes.NewReader(data))
		dec.SetInvalidUTF8Mode(UTF8Replace) // This won't affect Token()

		// Read object start
		tok, _ := dec.Token()
		if _, ok := tok.(Delim); !ok {
			t.Fatalf("expected Delim, got %T", tok)
		}

		// Read key - Token() returns raw bytes as string without UTF-8 processing
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("Token error for key: %v", err)
		}
		if s, ok := tok.(string); !ok {
			t.Errorf("expected string key, got %v (%T)", tok, tok)
		} else if s != "\x80a" {
			// Token() returns raw string, not replaced
			t.Errorf("expected raw string \"\\x80a\", got %q", s)
		}
	})

	t.Run("token_in_array_with_all_modes", func(t *testing.T) {
		// Create array with valid data
		data, _ := Marshal([]any{1, "hello", true})

		dec := NewDecoder(bytes.NewReader(data))
		dec.AllowNUL()
		dec.SetInvalidUTF8Mode(UTF8Ignore)
		dec.SetDuplicateKeyMode(DupKeyKeepLast)
		dec.SetNaNInfinityMode(NaNInfAllow)

		// Should still work normally
		tok, _ := dec.Token() // [
		if d, ok := tok.(Delim); !ok || d != '[' {
			t.Errorf("expected '[', got %v", tok)
		}

		tok, _ = dec.Token() // 1
		if v, ok := tok.(int64); !ok || v != 1 {
			t.Errorf("expected 1, got %v (%T)", tok, tok)
		}

		tok, _ = dec.Token() // "hello"
		if s, ok := tok.(string); !ok || s != "hello" {
			t.Errorf("expected \"hello\", got %v", tok)
		}

		tok, _ = dec.Token() // true
		if b, ok := tok.(bool); !ok || !b {
			t.Errorf("expected true, got %v", tok)
		}

		tok, _ = dec.Token() // ]
		if d, ok := tok.(Delim); !ok || d != ']' {
			t.Errorf("expected ']', got %v", tok)
		}
	})
}
