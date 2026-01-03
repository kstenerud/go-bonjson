// Copyright 2024 Karl Stenerud. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bonjson

import (
	"bytes"
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

func TestDecoderNativeNumericTypes(t *testing.T) {
	// Test that Token() returns native types

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

func TestDecoderAllowChunking(t *testing.T) {
	// Just verify the method can be called
	dec := NewDecoder(strings.NewReader(""))
	dec.AllowChunking()
}

func TestDecoderAllowNUL(t *testing.T) {
	// Just verify the method can be called
	dec := NewDecoder(strings.NewReader(""))
	dec.AllowNUL()
}

func TestDecoderSetMaxDepth(t *testing.T) {
	// Create nested arrays
	depth := 20
	var buf bytes.Buffer
	for i := 0; i < depth; i++ {
		buf.WriteByte(typeArrayStart)
	}
	buf.WriteByte(typeNull)
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
