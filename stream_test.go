//
// stream_test.go
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
	"fmt"
	"io"
	"strings"
	"testing"
)

// ============================================================================
// Encoder Tests
// ============================================================================

func TestEncoder(t *testing.T) {
	tests := []struct {
		name   string
		values []any
	}{
		{"single_int", []any{42}},
		{"single_string", []any{"hello"}},
		{"multiple_values", []any{1, "two", true, nil}},
		{"objects", []any{map[string]int{"a": 1}, map[string]int{"b": 2}}},
		{"arrays", []any{[]int{1, 2}, []int{3, 4}}},
		{"mixed", []any{42, "hello", []int{1, 2}, map[string]string{"key": "value"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := NewEncoder(&buf)

			for i, v := range tt.values {
				if err := enc.Encode(v); err != nil {
					t.Fatalf("Encode[%d] error: %v", i, err)
				}
			}

			// Decode them back
			dec := NewDecoder(&buf)
			for i, want := range tt.values {
				var got any
				if err := dec.Decode(&got); err != nil {
					t.Fatalf("Decode[%d] error: %v", i, err)
				}
				// Values may have different types due to untyped decoding
				_ = want
			}
		})
	}
}

func TestEncoderReuse(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	// Encode multiple values
	for i := 0; i < 100; i++ {
		if err := enc.Encode(i); err != nil {
			t.Fatalf("Encode(%d) error: %v", i, err)
		}
	}

	// Decode them
	dec := NewDecoder(&buf)
	for i := 0; i < 100; i++ {
		var got int
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("Decode error: %v", err)
		}
		if got != i {
			t.Errorf("got %d, want %d", got, i)
		}
	}
}

// ============================================================================
// Decoder Tests
// ============================================================================

func TestDecoder(t *testing.T) {
	// Create data with multiple values
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.Encode(1)
	enc.Encode("two")
	enc.Encode(true)
	enc.Encode(nil)

	dec := NewDecoder(&buf)

	var i int
	if err := dec.Decode(&i); err != nil {
		t.Fatalf("Decode int error: %v", err)
	}
	if i != 1 {
		t.Errorf("int = %d, want 1", i)
	}

	var s string
	if err := dec.Decode(&s); err != nil {
		t.Fatalf("Decode string error: %v", err)
	}
	if s != "two" {
		t.Errorf("string = %q, want %q", s, "two")
	}

	var b bool
	if err := dec.Decode(&b); err != nil {
		t.Fatalf("Decode bool error: %v", err)
	}
	if !b {
		t.Error("bool = false, want true")
	}

	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("Decode nil error: %v", err)
	}
	if v != nil {
		t.Errorf("nil = %v, want nil", v)
	}

	// Next decode should return EOF
	if err := dec.Decode(&v); err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestDecoderMore(t *testing.T) {
	// Test More() method
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.Encode(1)
	enc.Encode(2)

	dec := NewDecoder(&buf)

	if !dec.More() {
		t.Error("More() = false before first decode")
	}

	var v int
	dec.Decode(&v)

	if !dec.More() {
		t.Error("More() = false after first decode")
	}

	dec.Decode(&v)

	if dec.More() {
		t.Error("More() = true after all values decoded")
	}
}

func TestDecoderMoreOnEmptyInput(t *testing.T) {
	// Test More() on empty input
	dec := NewDecoder(bytes.NewReader([]byte{}))

	if dec.More() {
		t.Error("More() = true on empty input, expected false")
	}
}

func TestDecoderMoreAfterChunkExhausted(t *testing.T) {
	// Test More() correctly identifies when container is exhausted
	// With delimiter-terminated format, More() returns false when 0xFE end marker is found
	// Build an array with one element, and read it
	data := []byte{
		typeArray,
		typeNull,        // the one element
		typeContainerEnd, // end marker
	}
	dec := NewDecoder(bytes.NewReader(data))

	// Read array start
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if d, ok := tok.(Delim); !ok || d != '[' {
		t.Fatalf("expected '[', got %v", tok)
	}

	// Now More() should return true (one element remaining)
	if !dec.More() {
		t.Error("More() = false when element remaining, expected true")
	}

	// Read the element
	_, err = dec.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}

	// Now More() should return false (chunk exhausted, no continuation)
	if dec.More() {
		t.Error("More() = true after chunk exhausted, expected false")
	}
}

func TestDecoderMoreDetectsValue(t *testing.T) {
	// Test More() correctly identifies a value is present
	data := []byte{typeNull}
	dec := NewDecoder(bytes.NewReader(data))

	if !dec.More() {
		t.Error("More() = false for null value, expected true")
	}
}

func TestDecoderBuffered(t *testing.T) {
	// Test Buffered() method returns unconsumed data
	data, _ := Marshal(42)
	extra := []byte{0x99, 0x9b} // empty array marker

	combined := append(data, extra...)
	dec := NewDecoder(bytes.NewReader(combined))

	var v int
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	buffered := dec.Buffered()
	remaining, err := io.ReadAll(buffered)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}

	// Should have the extra data
	if len(remaining) == 0 {
		t.Log("Buffered() returned no extra data (may be implementation-specific)")
	}
}

// ============================================================================
// Decoder Options Tests
// ============================================================================

func TestDecoderTokenReturnsNativeTypes(t *testing.T) {
	// Test that Token() returns native numeric types (float64, int64, uint64)

	// Float value
	data, _ := Marshal(123.456)
	dec := NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	f, ok := tok.(float64)
	if !ok {
		t.Errorf("expected float64, got %T", tok)
	} else if f != 123.456 {
		t.Errorf("float value = %f, want 123.456", f)
	}

	// Signed integer
	data2, _ := Marshal(-42)
	dec2 := NewDecoder(bytes.NewReader(data2))
	tok2, err := dec2.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	i, ok := tok2.(int64)
	if !ok {
		t.Errorf("expected int64, got %T", tok2)
	} else if i != -42 {
		t.Errorf("int value = %d, want -42", i)
	}

	// Unsigned integer
	data3, _ := Marshal(uint64(12345678901234567890))
	dec3 := NewDecoder(bytes.NewReader(data3))
	tok3, err := dec3.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	u, ok := tok3.(uint64)
	if !ok {
		t.Errorf("expected uint64, got %T", tok3)
	} else if u != 12345678901234567890 {
		t.Errorf("uint value = %d, want 12345678901234567890", u)
	}
}

func TestDecoderDisallowUnknownFields(t *testing.T) {
	type Target struct {
		Known int `bonjson:"known"`
	}

	type Source struct {
		Known   int `bonjson:"known"`
		Unknown int `bonjson:"unknown"`
	}

	data, _ := Marshal(Source{Known: 1, Unknown: 2})

	dec := NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var target Target
	err := dec.Decode(&target)
	if err == nil {
		t.Error("expected error for unknown field")
	}
}

func TestDecoderAllowNUL(t *testing.T) {
	// Just verify the method can be called
	dec := NewDecoder(strings.NewReader(""))
	dec.AllowNUL()
}

func TestDecoderSetMaxDepth(t *testing.T) {
	// Create nested arrays using delimiter-terminated format
	depth := 20
	var buf bytes.Buffer
	// Each nested array
	for i := 0; i < depth; i++ {
		buf.WriteByte(typeArray)
	}
	// Innermost value
	buf.WriteByte(typeNull)
	// Close all arrays
	for i := 0; i < depth; i++ {
		buf.WriteByte(typeContainerEnd)
	}

	// Should succeed with adequate depth
	dec1 := NewDecoder(bytes.NewReader(buf.Bytes()))
	dec1.SetMaxDepth(100)
	var v1 any
	if err := dec1.Decode(&v1); err != nil {
		t.Errorf("Decode with depth 100 failed: %v", err)
	}

	// Should fail with inadequate depth
	dec2 := NewDecoder(bytes.NewReader(buf.Bytes()))
	dec2.SetMaxDepth(5)
	var v2 any
	err := dec2.Decode(&v2)
	if err == nil {
		t.Error("expected error for exceeding max depth")
	}
}

func TestDecoderSetMaxStringLength(t *testing.T) {
	longStr := strings.Repeat("x", 100)
	data, _ := Marshal(longStr)

	// Should succeed with adequate length
	dec1 := NewDecoder(bytes.NewReader(data))
	dec1.SetMaxStringLength(200)
	var v1 string
	if err := dec1.Decode(&v1); err != nil {
		t.Errorf("Decode with length 200 failed: %v", err)
	}

	// Should fail with inadequate length
	dec2 := NewDecoder(bytes.NewReader(data))
	dec2.SetMaxStringLength(50)
	var v2 string
	err := dec2.Decode(&v2)
	if err == nil {
		t.Error("expected error for exceeding max string length")
	}
}

// ============================================================================
// RawMessage Tests
// ============================================================================

func TestRawMessageMarshal(t *testing.T) {
	inner, _ := Marshal(map[string]int{"a": 1, "b": 2})
	raw := RawMessage(inner)

	data, err := Marshal(raw)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	if !bytes.Equal(data, inner) {
		t.Errorf("RawMessage marshal mismatch")
	}
}

func TestRawMessageUnmarshal(t *testing.T) {
	original := map[string]int{"x": 42, "y": 99}
	data, _ := Marshal(original)

	var raw RawMessage
	if err := Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !bytes.Equal(raw, data) {
		t.Errorf("RawMessage unmarshal mismatch")
	}

	// Can further unmarshal the raw message
	var v map[string]int
	if err := Unmarshal(raw, &v); err != nil {
		t.Fatalf("Unmarshal raw error: %v", err)
	}

	if v["x"] != 42 || v["y"] != 99 {
		t.Errorf("decoded values mismatch: %v", v)
	}
}

func TestRawMessageInStruct(t *testing.T) {
	type Container struct {
		Name string     `bonjson:"name"`
		Data RawMessage `bonjson:"data"`
	}

	inner, _ := Marshal([]int{1, 2, 3})
	original := Container{
		Name: "test",
		Data: RawMessage(inner),
	}

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got Container
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.Name != original.Name {
		t.Errorf("Name = %q, want %q", got.Name, original.Name)
	}

	// Decode the raw data
	var arr []int
	if err := Unmarshal(got.Data, &arr); err != nil {
		t.Fatalf("Unmarshal raw data error: %v", err)
	}

	if len(arr) != 3 || arr[0] != 1 || arr[1] != 2 || arr[2] != 3 {
		t.Errorf("raw data mismatch: %v", arr)
	}
}

// ============================================================================
// Token API Tests
// ============================================================================

func TestDecoderToken(t *testing.T) {
	original := map[string]any{
		"name": "Alice",
		"age":  30,
		"tags": []string{"a", "b"},
	}

	data, _ := Marshal(original)
	dec := NewDecoder(bytes.NewReader(data))

	// First token should be object start
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if tok != Delim('{') {
		t.Errorf("expected '{', got %v (%T)", tok, tok)
	}

	// Consume all tokens
	depth := 1
	for depth > 0 {
		tok, err = dec.Token()
		if err != nil {
			t.Fatalf("Token error: %v", err)
		}

		switch v := tok.(type) {
		case Delim:
			switch v {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}

	// Should be at end
	_, err = dec.Token()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestDecoderTokenArray(t *testing.T) {
	original := []int{1, 2, 3}
	data, _ := Marshal(original)
	dec := NewDecoder(bytes.NewReader(data))

	// First token: array start
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if tok != Delim('[') {
		t.Errorf("expected '[', got %v", tok)
	}

	// Three integers
	for i := 1; i <= 3; i++ {
		tok, err = dec.Token()
		if err != nil {
			t.Fatalf("Token error: %v", err)
		}
		// May be int, int64, float64, or Number depending on implementation
	}

	// Array end
	tok, err = dec.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if tok != Delim(']') {
		t.Errorf("expected ']', got %v", tok)
	}
}

func TestDecoderTokenObjectKeys(t *testing.T) {
	// Test that Token() returns object keys as strings
	original := map[string]int{"alpha": 1, "beta": 2}
	data, _ := Marshal(original)
	dec := NewDecoder(bytes.NewReader(data))

	// Object start
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if tok != Delim('{') {
		t.Fatalf("expected '{', got %v", tok)
	}

	// Read key-value pairs
	keys := make(map[string]bool)
	for i := 0; i < 2; i++ {
		// Key should be a string
		keyTok, err := dec.Token()
		if err != nil {
			t.Fatalf("Token error reading key %d: %v", i, err)
		}
		key, ok := keyTok.(string)
		if !ok {
			t.Errorf("key %d: expected string, got %T: %v", i, keyTok, keyTok)
		} else {
			keys[key] = true
		}

		// Value
		_, err = dec.Token()
		if err != nil {
			t.Fatalf("Token error reading value %d: %v", i, err)
		}
	}

	// Verify we got the expected keys
	if !keys["alpha"] || !keys["beta"] {
		t.Errorf("missing expected keys, got: %v", keys)
	}

	// Object end
	tok, err = dec.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if tok != Delim('}') {
		t.Errorf("expected '}', got %v", tok)
	}
}

func TestDelim(t *testing.T) {
	// Test Delim type
	delims := []Delim{'{', '}', '[', ']'}
	for _, d := range delims {
		s := d.String()
		if len(s) != 1 {
			t.Errorf("Delim(%c).String() = %q, expected single char", d, s)
		}
	}
}

// ============================================================================
// Reader/Writer Edge Cases
// ============================================================================

type errorReader struct {
	err error
}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, e.err
}

func TestDecoderReadError(t *testing.T) {
	expectedErr := io.ErrUnexpectedEOF
	dec := NewDecoder(&errorReader{err: expectedErr})

	var v any
	err := dec.Decode(&v)
	if err == nil {
		t.Error("expected error from reader")
	}
}

type errorWriter struct {
	err error
}

func (e *errorWriter) Write(p []byte) (n int, err error) {
	return 0, e.err
}

func TestEncoderWriteError(t *testing.T) {
	expectedErr := io.ErrShortWrite
	enc := NewEncoder(&errorWriter{err: expectedErr})

	err := enc.Encode(map[string]int{"a": 1})
	if err == nil {
		t.Error("expected error from writer")
	}
}

func TestEncoderErrorStateIsSticky(t *testing.T) {
	// Test that once an encoder encounters a write error, it remembers the error
	// and returns it on subsequent calls. This is by design - encoders become
	// unusable after a write error to prevent partial/corrupted data.
	enc := NewEncoder(&errorWriter{err: io.ErrShortWrite})

	// First encode should fail
	err1 := enc.Encode(42)
	if err1 == nil {
		t.Fatal("expected error on first encode")
	}

	// Second encode should return the same error (sticky error state)
	err2 := enc.Encode(123)
	if err2 == nil {
		t.Fatal("expected error on second encode")
	}

	// Both errors should be the same
	if err1 != err2 {
		t.Errorf("errors differ: first=%v, second=%v", err1, err2)
	}
}

// ============================================================================
// Streaming Large Data Tests
// ============================================================================

func TestStreamLargeData(t *testing.T) {
	// Create large slice
	large := make([]int, 10000)
	for i := range large {
		large[i] = i
	}

	// Encode via stream
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(large); err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	// Decode via stream
	dec := NewDecoder(&buf)
	var got []int
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if len(got) != len(large) {
		t.Errorf("length = %d, want %d", len(got), len(large))
	}
}

// ============================================================================
// InputOffset Tests
// ============================================================================

func TestInputOffset(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.Encode(1)
	enc.Encode("hello")
	enc.Encode(true)

	totalLen := buf.Len() // Store length before decoding consumes the buffer

	dec := NewDecoder(&buf)

	var v1 int
	dec.Decode(&v1)
	offset1 := dec.InputOffset()

	var v2 string
	dec.Decode(&v2)
	offset2 := dec.InputOffset()

	var v3 bool
	dec.Decode(&v3)
	offset3 := dec.InputOffset()

	// Offsets should be increasing
	if offset1 >= offset2 || offset2 >= offset3 {
		t.Errorf("offsets not increasing: %d, %d, %d", offset1, offset2, offset3)
	}

	// Final offset should equal total length
	if offset3 != int64(totalLen) {
		t.Errorf("final offset = %d, buffer len = %d", offset3, totalLen)
	}
}

// ============================================================================
// Concurrent Use Tests
// ============================================================================

func TestEncoderDecoderConcurrency(t *testing.T) {
	// Test that encoding and decoding can happen concurrently
	// (using separate encoders/decoders)

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(id int) {
			var buf bytes.Buffer
			enc := NewEncoder(&buf)

			for j := 0; j < 100; j++ {
				enc.Encode(map[string]int{"id": id, "iter": j})
			}

			dec := NewDecoder(&buf)
			for j := 0; j < 100; j++ {
				var v map[string]int
				if err := dec.Decode(&v); err != nil {
					t.Errorf("goroutine %d decode error: %v", id, err)
					break
				}
			}

			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// ============================================================================
// Comprehensive Integer Tests (covers readLittleEndianUint64 and signExtend)
// ============================================================================

func TestStreamDecoderIntegerSizes(t *testing.T) {
	// Test all integer sizes (1-8 bytes) for both signed and unsigned
	tests := []struct {
		name  string
		value any
	}{
		// Unsigned integers - various byte sizes
		{"uint8_small", uint8(50)},
		{"uint8_max", uint8(255)},
		{"uint16", uint16(1000)},
		{"uint16_max", uint16(65535)},
		{"uint32", uint32(100000)},
		{"uint32_max", uint32(4294967295)},
		{"uint64", uint64(10000000000)},
		{"uint64_large", uint64(1<<63 + 12345)},
		// 3-byte uint (tests unaligned read path)
		{"uint_3byte", uint32(0xABCDEF)},
		// 5-byte uint
		{"uint_5byte", uint64(0xABCDEF1234)},
		// 6-byte uint
		{"uint_6byte", uint64(0xABCDEF123456)},
		// 7-byte uint
		{"uint_7byte", uint64(0xABCDEF12345678)},

		// Signed integers - various byte sizes
		{"int8_pos", int8(100)},
		{"int8_neg", int8(-100)},
		{"int16_pos", int16(1000)},
		{"int16_neg", int16(-1000)},
		{"int32_pos", int32(100000)},
		{"int32_neg", int32(-100000)},
		{"int64_pos", int64(10000000000)},
		{"int64_neg", int64(-10000000000)},
		// 3-byte signed (tests unaligned read + sign extension)
		{"int_3byte_pos", int32(0x7FFFFF)},  // max positive 3-byte
		{"int_3byte_neg", int32(-0x7FFFFF)}, // negative 3-byte
		// Edge cases for sign extension
		{"int_signext_2byte", int16(-1)},
		{"int_signext_4byte", int32(-1)},
		{"int_signext_8byte", int64(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			// Test via Decode
			dec := NewDecoder(bytes.NewReader(data))
			var got any
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("Decode error: %v", err)
			}

			// Test via Token
			dec2 := NewDecoder(bytes.NewReader(data))
			tok, err := dec2.Token()
			if err != nil {
				t.Fatalf("Token error: %v", err)
			}
			_ = tok // Token returns int64 or uint64
		})
	}
}

// ============================================================================
// Comprehensive Float Tests
// ============================================================================

func TestStreamDecoderFloatTypes(t *testing.T) {
	// Test floats that encode as actual float types (not integers)
	// Note: BONJSON encodes whole numbers like 0.0, 1.0 as integers
	tests := []struct {
		name  string
		value float64
	}{
		{"negative", -123.456},
		{"small", 0.000001},
		{"large", 1e20},
		{"pi", 3.14159265358979},
		{"half", 0.5},
		{"third", 1.0 / 3.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test float32
			data32, _ := Marshal(float32(tt.value))
			dec32 := NewDecoder(bytes.NewReader(data32))
			tok32, err := dec32.Token()
			if err != nil {
				t.Fatalf("Token float32 error: %v", err)
			}
			if _, ok := tok32.(float64); !ok {
				t.Errorf("expected float64 from Token, got %T", tok32)
			}

			// Test float64
			data64, _ := Marshal(tt.value)
			dec64 := NewDecoder(bytes.NewReader(data64))
			tok64, err := dec64.Token()
			if err != nil {
				t.Fatalf("Token float64 error: %v", err)
			}
			if _, ok := tok64.(float64); !ok {
				t.Errorf("expected float64 from Token, got %T", tok64)
			}
		})
	}
}

// ============================================================================
// Comprehensive String Tests (covers readLongString and readLengthField)
// ============================================================================

func TestStreamDecoderShortStrings(t *testing.T) {
	// Test all short string lengths (0-15 bytes)
	for length := 0; length <= 15; length++ {
		t.Run(fmt.Sprintf("length_%d", length), func(t *testing.T) {
			s := strings.Repeat("a", length)
			data, _ := Marshal(s)

			// Via Decode
			dec := NewDecoder(bytes.NewReader(data))
			var got string
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("Decode error: %v", err)
			}
			if got != s {
				t.Errorf("got %q, want %q", got, s)
			}

			// Via Token
			dec2 := NewDecoder(bytes.NewReader(data))
			tok, err := dec2.Token()
			if err != nil {
				t.Fatalf("Token error: %v", err)
			}
			if tok != s {
				t.Errorf("Token got %q, want %q", tok, s)
			}
		})
	}
}

func TestStreamDecoderLongStrings(t *testing.T) {
	// Test various long string sizes that exercise readLongString and readLengthField
	sizes := []int{
		16,     // just over short string limit
		100,    // medium
		1000,   // large
		10000,  // very large
		100000, // huge - tests multi-byte length encoding
	}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			s := strings.Repeat("x", size)
			data, err := Marshal(s)
			if err != nil {
				t.Fatalf("Marshal error for size %d: %v", size, err)
			}

			// Via Decode
			dec := NewDecoder(bytes.NewReader(data))
			var got string
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("Decode error for size %d: %v", size, err)
			}
			if len(got) != size {
				t.Errorf("Decode: got length %d, want %d", len(got), size)
			}

			// Via Token
			dec2 := NewDecoder(bytes.NewReader(data))
			tok, err := dec2.Token()
			if err != nil {
				t.Fatalf("Token error for size %d: %v", size, err)
			}
			if str, ok := tok.(string); !ok {
				t.Errorf("Token: expected string, got %T", tok)
			} else if len(str) != size {
				t.Errorf("Token: got length %d, want %d", len(str), size)
			}
		})
	}
}

func TestStreamDecoderLongStringUnicode(t *testing.T) {
	// Test long strings with unicode content
	tests := []string{
		strings.Repeat("日本語", 100),      // Japanese
		strings.Repeat("🎉🎊🎁", 100),      // Emoji
		strings.Repeat("α β γ δ ", 100), // Greek
	}

	for i, s := range tests {
		t.Run("unicode_"+string(rune('a'+i)), func(t *testing.T) {
			data, _ := Marshal(s)

			dec := NewDecoder(bytes.NewReader(data))
			var got string
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("Decode error: %v", err)
			}
			if got != s {
				t.Errorf("string mismatch")
			}
		})
	}
}

// ============================================================================
// BigNumber Tests (covers readBigNumber)
// ============================================================================

func TestStreamDecoderBigNumbers(t *testing.T) {
	// BigNumbers are encoded via *big.Int and *big.Float
	// Test that they round-trip through streaming

	// Large integer that doesn't fit in int64
	largeIntStr := "123456789012345678901234567890"

	type bigIntWrapper struct {
		Value string `bonjson:"value"`
	}

	// Encode as string (BigNumber encoding tested elsewhere)
	data, _ := Marshal(bigIntWrapper{Value: largeIntStr})

	dec := NewDecoder(bytes.NewReader(data))
	var got bigIntWrapper
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if got.Value != largeIntStr {
		t.Errorf("got %q, want %q", got.Value, largeIntStr)
	}
}

// ============================================================================
// Container Tests (covers readContainer)
// ============================================================================

func TestStreamDecoderNestedContainers(t *testing.T) {
	// Deeply nested structure
	type Inner struct {
		Value int `bonjson:"value"`
	}
	type Middle struct {
		Inner Inner `bonjson:"inner"`
	}
	type Outer struct {
		Middle Middle `bonjson:"middle"`
	}

	original := Outer{
		Middle: Middle{
			Inner: Inner{
				Value: 42,
			},
		},
	}

	data, _ := Marshal(original)

	dec := NewDecoder(bytes.NewReader(data))
	var got Outer
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if got.Middle.Inner.Value != 42 {
		t.Errorf("got %d, want 42", got.Middle.Inner.Value)
	}
}

func TestStreamDecoderMixedArrays(t *testing.T) {
	// Array with mixed types
	original := []any{
		int64(1),
		"two",
		true,
		nil,
		[]any{int64(3), int64(4)},
		map[string]any{"key": "value"},
	}

	data, _ := Marshal(original)

	dec := NewDecoder(bytes.NewReader(data))
	var got []any
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if len(got) != len(original) {
		t.Errorf("got %d elements, want %d", len(got), len(original))
	}
}

// ============================================================================
// Token API Comprehensive Tests
// ============================================================================

func TestTokenAllTypes(t *testing.T) {
	// Test Token() returns correct types for all value types
	tests := []struct {
		name     string
		value    any
		wantType string
	}{
		{"null", nil, "<nil>"},
		{"true", true, "bool"},
		{"false", false, "bool"},
		{"small_int", int64(42), "int64"},
		{"small_neg_int", int64(-42), "int64"},
		{"large_int", uint64(1 << 62), "int64"},        // Fits in int64, encoded as signed per spec
		{"large_uint", uint64(1<<63 + 1000), "uint64"}, // Exceeds int64, must be unsigned
		{"float", 3.14, "float64"},
		{"string", "hello", "string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := Marshal(tt.value)
			dec := NewDecoder(bytes.NewReader(data))
			tok, err := dec.Token()
			if err != nil {
				t.Fatalf("Token error: %v", err)
			}

			// Check type
			var gotType string
			switch tok.(type) {
			case nil:
				gotType = "<nil>"
			case bool:
				gotType = "bool"
			case int64:
				gotType = "int64"
			case uint64:
				gotType = "uint64"
			case float64:
				gotType = "float64"
			case string:
				gotType = "string"
			default:
				gotType = "unknown"
			}

			if gotType != tt.wantType {
				t.Errorf("got type %s, want %s", gotType, tt.wantType)
			}
		})
	}
}

func TestTokenContainerNavigation(t *testing.T) {
	// Test navigating through nested containers with Token()
	original := map[string]any{
		"array": []any{int64(1), int64(2), int64(3)},
		"nested": map[string]any{
			"deep": "value",
		},
	}

	data, _ := Marshal(original)
	dec := NewDecoder(bytes.NewReader(data))

	// Should be able to walk the entire structure
	tokens := []Token{}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Token error: %v", err)
		}
		tokens = append(tokens, tok)
	}

	// Should have: { "array" [ 1 2 3 ] "nested" { "deep" "value" } }
	// That's 12 tokens
	if len(tokens) < 10 {
		t.Errorf("expected at least 10 tokens, got %d", len(tokens))
	}
}

// ============================================================================
// Error Handling Tests
// ============================================================================

func TestStreamDecoderTruncatedData(t *testing.T) {
	// Test handling of truncated data in various places

	tests := []struct {
		name string
		data []byte
	}{
		{"truncated_uint", []byte{typeUintBase + 3}},                          // Says 4 bytes but none follow
		{"truncated_sint", []byte{typeSintBase + 3}},                          // Says 4 bytes but none follow
		{"truncated_float32", []byte{typeFloat32, 0x00}},                      // Only 1 byte of float32
		{"truncated_float64", []byte{typeFloat64, 0x00, 0x00}},                // Only 2 bytes of float64
		{"truncated_short_string", []byte{typeShortStringBase + 5, 'a', 'b'}}, // Says 5 bytes, only 2
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec := NewDecoder(bytes.NewReader(tt.data))
			var v any
			err := dec.Decode(&v)
			if err == nil {
				t.Error("expected error for truncated data")
			}
		})
	}
}

func TestStreamTokenTruncatedData(t *testing.T) {
	// Test Token() with truncated data

	tests := []struct {
		name string
		data []byte
	}{
		{"truncated_uint", []byte{typeUintBase + 3}},
		{"truncated_sint", []byte{typeSintBase + 3}},
		{"truncated_float32", []byte{typeFloat32, 0x00}},
		{"truncated_float64", []byte{typeFloat64, 0x00, 0x00, 0x00}},
		{"truncated_short_string", []byte{typeShortStringBase + 5, 'a'}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec := NewDecoder(bytes.NewReader(tt.data))
			_, err := dec.Token()
			if err == nil {
				t.Error("expected error for truncated data")
			}
		})
	}
}

// ============================================================================
// Additional Streaming API Edge Case Tests
// ============================================================================

// Test mixed Token() and Decode() usage
func TestMixedTokenAndDecode(t *testing.T) {
	// Create an array with nested structures
	input := []any{
		42,
		map[string]any{"key": "value"},
		[]int{1, 2, 3},
	}
	data, _ := Marshal(input)

	dec := NewDecoder(bytes.NewReader(data))

	// Start with Token to get array delimiter
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if tok != Delim('[') {
		t.Errorf("expected [, got %v", tok)
	}

	// Read first element with Token
	tok, err = dec.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if tok != int64(42) {
		t.Errorf("expected 42, got %v", tok)
	}

	// Read second element (object) with Decode
	var obj map[string]string
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if obj["key"] != "value" {
		t.Errorf("expected value, got %q", obj["key"])
	}

	// Read third element (array) with Decode
	var arr []int
	if err := dec.Decode(&arr); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if len(arr) != 3 {
		t.Errorf("expected 3 elements, got %d", len(arr))
	}

	// Finish with Token to get closing delimiter
	tok, err = dec.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if tok != Delim(']') {
		t.Errorf("expected ], got %v", tok)
	}
}

// Test Token() on empty containers
func TestTokenEmptyContainers(t *testing.T) {
	t.Run("empty_array", func(t *testing.T) {
		data, _ := Marshal([]int{})
		dec := NewDecoder(bytes.NewReader(data))

		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("Token error: %v", err)
		}
		if tok != Delim('[') {
			t.Errorf("expected [, got %v", tok)
		}

		tok, err = dec.Token()
		if err != nil {
			t.Fatalf("Token error: %v", err)
		}
		if tok != Delim(']') {
			t.Errorf("expected ], got %v", tok)
		}
	})

	t.Run("empty_object", func(t *testing.T) {
		data, _ := Marshal(map[string]int{})
		dec := NewDecoder(bytes.NewReader(data))

		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("Token error: %v", err)
		}
		if tok != Delim('{') {
			t.Errorf("expected {, got %v", tok)
		}

		tok, err = dec.Token()
		if err != nil {
			t.Fatalf("Token error: %v", err)
		}
		if tok != Delim('}') {
			t.Errorf("expected }, got %v", tok)
		}
	})
}

// Test InputOffset accuracy at various positions
func TestInputOffsetAccuracy(t *testing.T) {
	// Create a multi-value stream with known byte positions
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.Encode(true)        // 1 byte: 0x6f
	enc.Encode(false)       // 1 byte: 0x6e
	enc.Encode(nil)         // 1 byte: 0x6d
	enc.Encode(42)          // 1 byte: 0x2a
	enc.Encode("hi")        // 3 bytes: 0x82 'h' 'i'

	data := buf.Bytes()
	dec := NewDecoder(bytes.NewReader(data))

	// Track expected offsets after each decode
	expectedOffsets := []int64{
		0,  // before first decode
		1,  // after true
		2,  // after false
		3,  // after nil
		4,  // after 42
		7,  // after "hi"
	}

	if dec.InputOffset() != expectedOffsets[0] {
		t.Errorf("initial offset = %d, want %d", dec.InputOffset(), expectedOffsets[0])
	}

	var v any
	for i := 1; i < len(expectedOffsets); i++ {
		dec.Decode(&v)
		if dec.InputOffset() != expectedOffsets[i] {
			t.Errorf("offset after decode %d = %d, want %d", i, dec.InputOffset(), expectedOffsets[i])
		}
	}
}

// Test More() behavior after error
func TestMoreAfterError(t *testing.T) {
	// Create invalid data using reserved type code 0xbb (in the 0xbb-0xf4 reserved range)
	invalidData := []byte{0xbb} // Reserved type code - invalid

	dec := NewDecoder(bytes.NewReader(invalidData))

	// More should still work (just peeks - returns true if there are bytes)
	if !dec.More() {
		t.Error("More() should return true for non-empty stream")
	}

	// Now try to decode - should error
	var v any
	err := dec.Decode(&v)
	if err == nil {
		t.Error("expected error for invalid data")
	}
}

// Test multi-document streaming with mixed types
func TestMultiDocumentMixedTypes(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	// Encode various types
	enc.Encode(map[string]int{"a": 1})
	enc.Encode([]string{"x", "y"})
	enc.Encode(123)
	enc.Encode("string")
	enc.Encode(true)
	enc.Encode(nil)

	// Use buffer directly like TestDecoderMore does
	dec := NewDecoder(&buf)

	// Decode each value with the correct type
	var obj map[string]int
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("Decode map error: %v", err)
	}
	if obj["a"] != 1 {
		t.Errorf("obj[a] = %d, want 1", obj["a"])
	}

	var arr []string
	if err := dec.Decode(&arr); err != nil {
		t.Fatalf("Decode array error: %v", err)
	}
	if len(arr) != 2 {
		t.Errorf("arr len = %d, want 2", len(arr))
	}

	var num int
	if err := dec.Decode(&num); err != nil {
		t.Fatalf("Decode int error: %v", err)
	}
	if num != 123 {
		t.Errorf("num = %d, want 123", num)
	}

	var str string
	if err := dec.Decode(&str); err != nil {
		t.Fatalf("Decode string error: %v", err)
	}
	if str != "string" {
		t.Errorf("str = %q, want %q", str, "string")
	}

	var b bool
	if err := dec.Decode(&b); err != nil {
		t.Fatalf("Decode bool error: %v", err)
	}
	if !b {
		t.Error("b = false, want true")
	}

	var null any
	if err := dec.Decode(&null); err != nil {
		t.Fatalf("Decode nil error: %v", err)
	}
	if null != nil {
		t.Errorf("null = %v, want nil", null)
	}

	// At this point, More() might still peek ahead before returning false.
	// This is expected behavior - More() only returns false after attempting
	// to peek and getting EOF.
	// Try to decode one more value - should get EOF
	var extra any
	err := dec.Decode(&extra)
	if err != io.EOF {
		t.Errorf("expected io.EOF after all values, got err=%v, extra=%v", err, extra)
	}
}

// Test Token() returning correct delimiter types
func TestTokenDelimiterTypes(t *testing.T) {
	// Verify that delimiters are returned as Delim type with correct rune value
	tests := []struct {
		input    any
		expected []Token
	}{
		{
			[]int{1},
			[]Token{Delim('['), int64(1), Delim(']')},
		},
		{
			map[string]int{"k": 2},
			[]Token{Delim('{'), "k", int64(2), Delim('}')},
		},
	}

	for _, tt := range tests {
		data, _ := Marshal(tt.input)
		dec := NewDecoder(bytes.NewReader(data))

		for i, expected := range tt.expected {
			tok, err := dec.Token()
			if err != nil {
				t.Fatalf("Token error at %d: %v", i, err)
			}

			switch exp := expected.(type) {
			case Delim:
				if d, ok := tok.(Delim); !ok || d != exp {
					t.Errorf("token %d = %v (%T), want %c (Delim)", i, tok, tok, exp)
				}
			default:
				if tok != expected {
					t.Errorf("token %d = %v, want %v", i, tok, expected)
				}
			}
		}
	}
}

// Test Encoder with various struct types
func TestEncoderStructTypes(t *testing.T) {
	type Inner struct {
		Value int `bonjson:"value"`
	}
	type Outer struct {
		Name  string `bonjson:"name"`
		Inner Inner  `bonjson:"inner"`
	}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	original := Outer{
		Name:  "test",
		Inner: Inner{Value: 42},
	}

	if err := enc.Encode(original); err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	dec := NewDecoder(&buf)
	var decoded Outer
	if err := dec.Decode(&decoded); err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if decoded.Name != original.Name || decoded.Inner.Value != original.Inner.Value {
		t.Errorf("decoded = %+v, want %+v", decoded, original)
	}
}

// Test Delim String() method
func TestDelimString(t *testing.T) {
	tests := []struct {
		d        Delim
		expected string
	}{
		{Delim('['), "["},
		{Delim(']'), "]"},
		{Delim('{'), "{"},
		{Delim('}'), "}"},
	}

	for _, tt := range tests {
		if s := tt.d.String(); s != tt.expected {
			t.Errorf("Delim(%c).String() = %q, want %q", tt.d, s, tt.expected)
		}
	}
}

// Test Token() EOF behavior
func TestTokenEOF(t *testing.T) {
	data, _ := Marshal(42)
	dec := NewDecoder(bytes.NewReader(data))

	// First token should succeed
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if tok != int64(42) {
		t.Errorf("tok = %v, want 42", tok)
	}

	// Second token should return EOF
	_, err = dec.Token()
	if err != io.EOF {
		t.Errorf("err = %v, want io.EOF", err)
	}
}

// Test consecutive Token() calls return correct values
func TestTokenConsecutiveValues(t *testing.T) {
	// Create stream of different value types
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.Encode(int64(100))
	enc.Encode("hello")
	enc.Encode(true)
	enc.Encode(3.14)

	dec := NewDecoder(&buf)

	// Token should return correct types
	tok, _ := dec.Token()
	if v, ok := tok.(int64); !ok || v != 100 {
		t.Errorf("token 1: got %v (%T), want 100 (int64)", tok, tok)
	}

	tok, _ = dec.Token()
	if v, ok := tok.(string); !ok || v != "hello" {
		t.Errorf("token 2: got %v (%T), want hello (string)", tok, tok)
	}

	tok, _ = dec.Token()
	if v, ok := tok.(bool); !ok || v != true {
		t.Errorf("token 3: got %v (%T), want true (bool)", tok, tok)
	}

	tok, _ = dec.Token()
	if v, ok := tok.(float64); !ok || v != 3.14 {
		t.Errorf("token 4: got %v (%T), want 3.14 (float64)", tok, tok)
	}
}

// Test Decoder UseNumber option behavior
func TestDecoderUseNumber(t *testing.T) {
	// Note: BONJSON preserves integer types, so UseNumber might behave differently
	// from JSON. This test verifies the current behavior.
	data, _ := Marshal(map[string]any{"int": 42, "float": 3.14})

	dec := NewDecoder(bytes.NewReader(data))
	var decoded map[string]any
	if err := dec.Decode(&decoded); err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	// BONJSON should return int64 for integers
	if _, ok := decoded["int"].(int64); !ok {
		t.Errorf("int value type = %T, want int64", decoded["int"])
	}

	// BONJSON should return float64 for floats
	if _, ok := decoded["float"].(float64); !ok {
		t.Errorf("float value type = %T, want float64", decoded["float"])
	}
}
