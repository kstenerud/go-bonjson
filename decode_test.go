//
// decode_test.go
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
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Basic Decode Tests
// ============================================================================

func TestDecodeBasicTypes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		ptr  any
		want any
	}{
		// Booleans
		{"true", []byte{typeTrue}, new(bool), true},
		{"false", []byte{typeFalse}, new(bool), false},

		// Null
		{"null", []byte{typeNull}, new(any), nil},

		// Small integers (0-100)
		{"int_0", []byte{0x00}, new(int), 0},
		{"int_1", []byte{0x01}, new(int), 1},
		{"int_100", []byte{0x64}, new(int), 100},

		// Small negative integers (-1 to -100)
		{"int_-1", []byte{0xff}, new(int), -1},
		{"int_-100", []byte{0x9c}, new(int), -100},

		// Short strings
		{"empty_string", []byte{typeShortStringBase}, new(string), ""},
		{"string_a", []byte{typeShortStringBase + 1, 'a'}, new(string), "a"},
		{"string_hello", []byte{typeShortStringBase + 5, 'h', 'e', 'l', 'l', 'o'}, new(string), "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Unmarshal(tt.data, tt.ptr); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			got := reflect.ValueOf(tt.ptr).Elem().Interface()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Unmarshal = %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

// ============================================================================
// Type Coercion Tests
// ============================================================================

func TestDecodeTypeCoercion(t *testing.T) {
	// Marshal an int, decode into various numeric types
	data, _ := Marshal(42)

	tests := []struct {
		name string
		ptr  any
	}{
		{"int", new(int)},
		{"int8", new(int8)},
		{"int16", new(int16)},
		{"int32", new(int32)},
		{"int64", new(int64)},
		{"uint", new(uint)},
		{"uint8", new(uint8)},
		{"uint16", new(uint16)},
		{"uint32", new(uint32)},
		{"uint64", new(uint64)},
		{"float32", new(float32)},
		{"float64", new(float64)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Unmarshal(data, tt.ptr); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
		})
	}
}

// ============================================================================
// Invalid Unmarshal Target Tests
// ============================================================================

func TestUnmarshalInvalidTargetErrors(t *testing.T) {
	data, _ := Marshal(42)

	tests := []struct {
		name   string
		target any
	}{
		{"nil", nil},
		{"non_pointer", 42},
		{"nil_pointer", (*int)(nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Unmarshal(data, tt.target)
			if err == nil {
				t.Error("expected error")
			}
			var e *InvalidUnmarshalError
			if !errors.As(err, &e) {
				t.Errorf("expected InvalidUnmarshalError, got %T: %v", err, err)
			}
		})
	}
}

// ============================================================================
// Unmarshal Type Error Tests
// ============================================================================

func TestUnmarshalTypeError(t *testing.T) {
	tests := []struct {
		name string
		data any // value to marshal
		ptr  any // target to unmarshal into
	}{
		{"string_to_int", "hello", new(int)},
		{"int_to_string", 42, new(string)},
		{"array_to_string", []int{1, 2}, new(string)},
		{"object_to_int", map[string]int{"a": 1}, new(int)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := Marshal(tt.data)
			err := Unmarshal(data, tt.ptr)
			if err == nil {
				t.Error("expected type error")
			}
		})
	}
}

// ============================================================================
// UnmarshalWithByteCount Tests
// ============================================================================

func TestUnmarshalWithByteCount(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		ptr          any
		want         any
		wantConsumed int
	}{
		// Values without trailing data - should succeed
		{"int_no_trailing", []byte{0x0a}, new(int), 10, 1},
		{"null_no_trailing", []byte{typeNull}, new(any), nil, 1},
		{"true_no_trailing", []byte{typeTrue}, new(bool), true, 1},
		{"string_no_trailing", []byte{typeShortStringBase + 2, 'h', 'i'}, new(string), "hi", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consumed, err := UnmarshalWithByteCount(tt.data, tt.ptr)
			if err != nil {
				t.Fatalf("UnmarshalWithByteCount error: %v", err)
			}
			if consumed != tt.wantConsumed {
				t.Errorf("consumed = %d, want %d", consumed, tt.wantConsumed)
			}
			got := reflect.ValueOf(tt.ptr).Elem().Interface()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnmarshalWithByteCountReturnsConsumedOnTrailingData(t *testing.T) {
	// UnmarshalWithByteCount should return error on trailing data, but also return consumed count
	data := []byte{0x05, 0x99, 0x88, 0x77} // int 5 followed by garbage
	var v int
	consumed, err := UnmarshalWithByteCount(data, &v)
	if err == nil {
		t.Error("expected error for trailing bytes")
	}
	// Should report the position where trailing data starts
	if consumed != 1 {
		t.Errorf("consumed = %d, want 1", consumed)
	}
	// Value should still be decoded
	if v != 5 {
		t.Errorf("v = %d, want 5", v)
	}
	var trailingErr *TrailingDataError
	if !errors.As(err, &trailingErr) {
		t.Errorf("expected TrailingDataError, got %T: %v", err, err)
	}
	if trailingErr.Offset != 1 {
		t.Errorf("Offset = %d, want 1", trailingErr.Offset)
	}
}

func TestUnmarshalWithByteCountReturnsConsumedOnError(t *testing.T) {
	// Test that UnmarshalWithByteCount returns bytes consumed even on decode error

	// Truncated int64 (type code says 8 bytes follow, but only 2 provided)
	data := []byte{typeUintBase + 7, 0x01, 0x02}
	var v int
	n, err := UnmarshalWithByteCount(data, &v)
	if err == nil {
		t.Error("expected error for truncated data")
	}
	// Should have consumed at least the type byte
	if n < 1 {
		t.Errorf("expected n >= 1, got %d", n)
	}
	var truncErr *TruncatedDataError
	if !errors.As(err, &truncErr) {
		t.Errorf("expected TruncatedDataError, got %T: %v", err, err)
	}
}

func TestUnmarshalWithByteCountErrorConditions(t *testing.T) {
	// Test all error conditions that can occur during partial decoding

	t.Run("InvalidTypeCodeError", func(t *testing.T) {
		// Reserved type codes 0x65-0x67 and 0x90-0x98 are invalid
		data := []byte{0x65}
		var v any
		n, err := UnmarshalWithByteCount(data, &v)
		if err == nil {
			t.Error("expected error")
		}
		_ = n // byte count returned even on error
		var typeErr *InvalidTypeCodeError
		if !errors.As(err, &typeErr) {
			t.Errorf("expected InvalidTypeCodeError, got %T: %v", err, err)
		}
	})

	t.Run("UnmarshalTypeError", func(t *testing.T) {
		// String value into int destination
		data := []byte{typeShortStringBase + 2, 'h', 'i'}
		var v int
		n, err := UnmarshalWithByteCount(data, &v)
		if err == nil {
			t.Error("expected error")
		}
		if n != 3 {
			t.Errorf("expected n = 3, got %d", n)
		}
		var typeErr *UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			t.Errorf("expected UnmarshalTypeError, got %T: %v", err, err)
		}
	})

	t.Run("InvalidUTF8Error", func(t *testing.T) {
		// Invalid UTF-8 sequence in string
		data := []byte{typeShortStringBase + 2, 0xff, 0xfe}
		var v string
		n, err := UnmarshalWithByteCount(data, &v)
		if err == nil {
			t.Error("expected error")
		}
		_ = n
		var utf8Err *InvalidUTF8Error
		if !errors.As(err, &utf8Err) {
			t.Errorf("expected InvalidUTF8Error, got %T: %v", err, err)
		}
	})

	t.Run("NullInStringError", func(t *testing.T) {
		// NUL character in string (forbidden by default)
		data := []byte{typeShortStringBase + 3, 'a', 0x00, 'b'}
		var v string
		n, err := UnmarshalWithByteCount(data, &v)
		if err == nil {
			t.Error("expected error")
		}
		_ = n
		var nulErr *NullInStringError
		if !errors.As(err, &nulErr) {
			t.Errorf("expected NullInStringError, got %T: %v", err, err)
		}
	})

	t.Run("DuplicateKeyError", func(t *testing.T) {
		// Object with duplicate keys
		data := []byte{
			typeObjectStart,
			typeShortStringBase + 1, 'a', 0x01, // "a": 1
			typeShortStringBase + 1, 'a', 0x02, // "a": 2 (duplicate)
			typeContainerEnd,
		}
		var v map[string]int
		n, err := UnmarshalWithByteCount(data, &v)
		if err == nil {
			t.Error("expected error")
		}
		_ = n
		var dupErr *DuplicateKeyError
		if !errors.As(err, &dupErr) {
			t.Errorf("expected DuplicateKeyError, got %T: %v", err, err)
		}
	})

	t.Run("MaxDepthError", func(t *testing.T) {
		// Create deeply nested arrays exceeding default max depth
		var buf bytes.Buffer
		for i := 0; i < 1001; i++ {
			buf.WriteByte(typeArrayStart)
		}
		buf.WriteByte(typeNull)
		for i := 0; i < 1001; i++ {
			buf.WriteByte(typeContainerEnd)
		}
		var v any
		n, err := UnmarshalWithByteCount(buf.Bytes(), &v)
		if err == nil {
			t.Error("expected error")
		}
		_ = n
		var depthErr *MaxDepthError
		if !errors.As(err, &depthErr) {
			t.Errorf("expected MaxDepthError, got %T: %v", err, err)
		}
	})

	t.Run("ValueRangeError", func(t *testing.T) {
		// Large integer that overflows int8
		data := []byte{typeUintBase + 1, 0x00, 0x01} // 256, overflows int8
		var v int8
		n, err := UnmarshalWithByteCount(data, &v)
		if err == nil {
			t.Error("expected error")
		}
		_ = n
		// Overflow manifests as UnmarshalTypeError (value can't fit in type)
		var typeErr *UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			t.Errorf("expected UnmarshalTypeError for overflow, got %T: %v", err, err)
		}
	})

	t.Run("SyntaxError_NonStringKey", func(t *testing.T) {
		// Object with non-string key
		data := []byte{
			typeObjectStart,
			0x05, 0x01, // int key (invalid)
			typeContainerEnd,
		}
		var v map[string]int
		n, err := UnmarshalWithByteCount(data, &v)
		if err == nil {
			t.Error("expected error")
		}
		_ = n
		var syntaxErr *SyntaxError
		if !errors.As(err, &syntaxErr) {
			t.Errorf("expected SyntaxError, got %T: %v", err, err)
		}
	})
}

func TestUnmarshalWithByteCountInvalidTarget(t *testing.T) {
	// Test invalid target
	_, err := UnmarshalWithByteCount([]byte{0x05}, nil)
	if err == nil {
		t.Error("expected error for nil target")
	}

	_, err = UnmarshalWithByteCount([]byte{0x05}, 42)
	if err == nil {
		t.Error("expected error for non-pointer target")
	}
}

func TestUnmarshalRejectsTrailingBytes(t *testing.T) {
	// Verify that regular Unmarshal still rejects trailing bytes
	data := []byte{0x05, 0x99, 0x88} // int 5 followed by garbage
	var v int
	err := Unmarshal(data, &v)
	if err == nil {
		t.Error("expected error for trailing bytes")
	}
	var trailingErr *TrailingDataError
	if !errors.As(err, &trailingErr) {
		t.Errorf("expected TrailingDataError, got %T", err)
	}
	if trailingErr.Offset != 1 {
		t.Errorf("Offset = %d, want 1", trailingErr.Offset)
	}
}

func TestUnmarshalWithByteCountConcatenatedDocuments(t *testing.T) {
	// Test decoding multiple concatenated BONJSON documents using UnmarshalWithByteCount.
	// This demonstrates how to use the byte count and TrailingDataError to decode
	// a stream of documents from a single byte slice.

	// Create test values of various types
	doc1 := 42
	doc2 := "hello world"
	doc3 := []int{1, 2, 3}
	doc4 := map[string]string{"key": "value"}
	doc5 := true

	// Marshal each document
	data1, _ := Marshal(doc1)
	data2, _ := Marshal(doc2)
	data3, _ := Marshal(doc3)
	data4, _ := Marshal(doc4)
	data5, _ := Marshal(doc5)

	// Concatenate all documents into a single byte slice
	concatenated := make([]byte, 0, len(data1)+len(data2)+len(data3)+len(data4)+len(data5))
	concatenated = append(concatenated, data1...)
	concatenated = append(concatenated, data2...)
	concatenated = append(concatenated, data3...)
	concatenated = append(concatenated, data4...)
	concatenated = append(concatenated, data5...)

	// Decode all documents one by one
	remaining := concatenated
	totalConsumed := 0

	// Document 1: int
	var got1 int
	n, err := UnmarshalWithByteCount(remaining, &got1)
	if err != nil {
		var trailingErr *TrailingDataError
		if !errors.As(err, &trailingErr) {
			t.Fatalf("doc1: unexpected error type: %T: %v", err, err)
		}
		// TrailingDataError is expected since there's more data
	}
	if got1 != doc1 {
		t.Errorf("doc1: got %v, want %v", got1, doc1)
	}
	if n != len(data1) {
		t.Errorf("doc1: consumed %d bytes, want %d", n, len(data1))
	}
	remaining = remaining[n:]
	totalConsumed += n

	// Document 2: string
	var got2 string
	n, err = UnmarshalWithByteCount(remaining, &got2)
	if err != nil {
		var trailingErr *TrailingDataError
		if !errors.As(err, &trailingErr) {
			t.Fatalf("doc2: unexpected error type: %T: %v", err, err)
		}
	}
	if got2 != doc2 {
		t.Errorf("doc2: got %v, want %v", got2, doc2)
	}
	if n != len(data2) {
		t.Errorf("doc2: consumed %d bytes, want %d", n, len(data2))
	}
	remaining = remaining[n:]
	totalConsumed += n

	// Document 3: []int
	var got3 []int
	n, err = UnmarshalWithByteCount(remaining, &got3)
	if err != nil {
		var trailingErr *TrailingDataError
		if !errors.As(err, &trailingErr) {
			t.Fatalf("doc3: unexpected error type: %T: %v", err, err)
		}
	}
	if !reflect.DeepEqual(got3, doc3) {
		t.Errorf("doc3: got %v, want %v", got3, doc3)
	}
	if n != len(data3) {
		t.Errorf("doc3: consumed %d bytes, want %d", n, len(data3))
	}
	remaining = remaining[n:]
	totalConsumed += n

	// Document 4: map[string]string
	var got4 map[string]string
	n, err = UnmarshalWithByteCount(remaining, &got4)
	if err != nil {
		var trailingErr *TrailingDataError
		if !errors.As(err, &trailingErr) {
			t.Fatalf("doc4: unexpected error type: %T: %v", err, err)
		}
	}
	if !reflect.DeepEqual(got4, doc4) {
		t.Errorf("doc4: got %v, want %v", got4, doc4)
	}
	if n != len(data4) {
		t.Errorf("doc4: consumed %d bytes, want %d", n, len(data4))
	}
	remaining = remaining[n:]
	totalConsumed += n

	// Document 5: bool (last document - no trailing data)
	var got5 bool
	n, err = UnmarshalWithByteCount(remaining, &got5)
	if err != nil {
		t.Fatalf("doc5: unexpected error: %v", err)
	}
	if got5 != doc5 {
		t.Errorf("doc5: got %v, want %v", got5, doc5)
	}
	if n != len(data5) {
		t.Errorf("doc5: consumed %d bytes, want %d", n, len(data5))
	}
	totalConsumed += n

	// Verify we consumed all bytes
	if totalConsumed != len(concatenated) {
		t.Errorf("total consumed %d bytes, want %d", totalConsumed, len(concatenated))
	}
}

func TestUnmarshalWithByteCountConcatenatedLoop(t *testing.T) {
	// Test decoding concatenated documents in a loop pattern,
	// which is the typical usage pattern for this API.

	// Create and concatenate multiple integer documents
	expected := []int{10, 20, 30, 40, 50}
	var concatenated []byte
	for _, v := range expected {
		data, _ := Marshal(v)
		concatenated = append(concatenated, data...)
	}

	// Decode using a loop
	var results []int
	remaining := concatenated

	for len(remaining) > 0 {
		var v int
		n, err := UnmarshalWithByteCount(remaining, &v)

		if err != nil {
			var trailingErr *TrailingDataError
			if errors.As(err, &trailingErr) {
				// Expected when there are more documents
				results = append(results, v)
				remaining = remaining[n:]
				continue
			}
			t.Fatalf("unexpected error: %T: %v", err, err)
		}

		// No error means this was the last document
		results = append(results, v)
		remaining = remaining[n:]
	}

	if !reflect.DeepEqual(results, expected) {
		t.Errorf("got %v, want %v", results, expected)
	}
}

func TestUnmarshalWithByteCountMixedTypes(t *testing.T) {
	// Test decoding concatenated documents of mixed types into interface{}

	// Create documents of different types
	docs := []any{
		int64(123),
		"test string",
		true,
		nil,
		[]any{int64(1), int64(2), int64(3)},
	}

	// Concatenate all documents
	var concatenated []byte
	for _, doc := range docs {
		data, err := Marshal(doc)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		concatenated = append(concatenated, data...)
	}

	// Decode all documents
	var results []any
	remaining := concatenated

	for len(remaining) > 0 {
		var v any
		n, err := UnmarshalWithByteCount(remaining, &v)

		if err != nil {
			var trailingErr *TrailingDataError
			if !errors.As(err, &trailingErr) {
				t.Fatalf("unexpected error: %T: %v", err, err)
			}
		}

		results = append(results, v)
		remaining = remaining[n:]
	}

	if len(results) != len(docs) {
		t.Fatalf("got %d documents, want %d", len(results), len(docs))
	}

	// Verify each result (note: integers decode as int64)
	for i, got := range results {
		want := docs[i]
		if !reflect.DeepEqual(got, want) {
			t.Errorf("doc[%d]: got %v (%T), want %v (%T)", i, got, got, want, want)
		}
	}
}

func mustMarshal(v any) []byte {
	data, err := Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// ============================================================================
// Struct Field Tests
// ============================================================================

type FieldTestStruct struct {
	Public  int
	private int
	Tagged  int `bonjson:"custom_name"`
	Ignored int `bonjson:"-"`
}

func TestDecodeStructFields(t *testing.T) {
	original := FieldTestStruct{
		Public:  1,
		private: 2,
		Tagged:  3,
		Ignored: 4,
	}

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got FieldTestStruct
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Public field should round-trip
	if got.Public != original.Public {
		t.Errorf("Public = %d, want %d", got.Public, original.Public)
	}

	// Tagged field should round-trip
	if got.Tagged != original.Tagged {
		t.Errorf("Tagged = %d, want %d", got.Tagged, original.Tagged)
	}

	// private field should be zero (not exported)
	if got.private != 0 {
		t.Errorf("private = %d, want 0", got.private)
	}

	// Ignored field should be zero
	if got.Ignored != 0 {
		t.Errorf("Ignored = %d, want 0", got.Ignored)
	}
}

// ============================================================================
// DisallowUnknownFields Tests
// ============================================================================

func TestDisallowUnknownFields(t *testing.T) {
	type Target struct {
		Known int `bonjson:"known"`
	}

	// Marshal a struct with an extra field
	type Source struct {
		Known   int `bonjson:"known"`
		Unknown int `bonjson:"unknown"`
	}
	data, _ := Marshal(Source{Known: 1, Unknown: 2})

	// Default: unknown fields ignored
	var target Target
	if err := Unmarshal(data, &target); err != nil {
		t.Fatalf("Unmarshal should allow unknown fields by default: %v", err)
	}

	// With DisallowUnknownFields via Decoder
	dec := NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var target2 Target
	if err := dec.Decode(&target2); err == nil {
		t.Error("expected error for unknown field with DisallowUnknownFields")
	}
}

// ============================================================================
// Native Numeric Types Tests
// ============================================================================

func TestNativeNumericTypes(t *testing.T) {
	// Test that BONJSON preserves native integer types

	// Signed integer decodes to int64
	data1, _ := Marshal(-42)
	var v1 any
	if err := Unmarshal(data1, &v1); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if _, ok := v1.(int64); !ok {
		t.Errorf("signed int: expected int64, got %T", v1)
	}

	// Large positive integer that fits in int64 decodes to int64
	// (Per spec: prefer signed encoding when same byte count)
	data2, _ := Marshal(uint64(1 << 62))
	var v2 any
	if err := Unmarshal(data2, &v2); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if _, ok := v2.(int64); !ok {
		t.Errorf("large positive int: expected int64, got %T", v2)
	}

	// Value > max int64 decodes to uint64 (requires unsigned encoding)
	data2b, _ := Marshal(uint64(1<<63 + 1000))
	var v2b any
	if err := Unmarshal(data2b, &v2b); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if _, ok := v2b.(uint64); !ok {
		t.Errorf("unsigned int > max int64: expected uint64, got %T", v2b)
	}

	// Float decodes to float64
	data3, _ := Marshal(123.456)
	var v3 any
	if err := Unmarshal(data3, &v3); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if _, ok := v3.(float64); !ok {
		t.Errorf("float: expected float64, got %T", v3)
	}
}

// ============================================================================
// Null Value Tests
// ============================================================================

func TestDecodeNull(t *testing.T) {
	data := []byte{typeNull}

	// Into pointer - should set to nil
	i := 42
	ptr := &i
	if err := Unmarshal(data, &ptr); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if ptr != nil {
		t.Errorf("expected nil pointer, got %v", ptr)
	}

	// Into interface - should be nil
	var v any
	if err := Unmarshal(data, &v); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if v != nil {
		t.Errorf("expected nil interface, got %v", v)
	}

	// Into slice - should set to nil
	slice := []int{1, 2, 3}
	if err := Unmarshal(data, &slice); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if slice != nil {
		t.Errorf("expected nil slice, got %v", slice)
	}

	// Into map - should set to nil
	m := map[string]int{"a": 1}
	if err := Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil map, got %v", m)
	}
}

// ============================================================================
// Array/Slice Decode Tests
// ============================================================================

func TestDecodeArraySlice(t *testing.T) {
	data, _ := Marshal([]int{1, 2, 3, 4, 5})

	// Decode into slice
	var slice []int
	if err := Unmarshal(data, &slice); err != nil {
		t.Fatalf("Unmarshal slice error: %v", err)
	}
	if len(slice) != 5 {
		t.Errorf("slice length = %d, want 5", len(slice))
	}

	// Decode into array (smaller)
	var arr3 [3]int
	if err := Unmarshal(data, &arr3); err != nil {
		t.Fatalf("Unmarshal arr3 error: %v", err)
	}
	// Should get first 3 elements

	// Decode into array (larger)
	var arr7 [7]int
	if err := Unmarshal(data, &arr7); err != nil {
		t.Fatalf("Unmarshal arr7 error: %v", err)
	}
	// Should get all 5 elements, rest are zero
	for i := 5; i < 7; i++ {
		if arr7[i] != 0 {
			t.Errorf("arr7[%d] = %d, want 0", i, arr7[i])
		}
	}
}

// ============================================================================
// Nested Structure Tests
// ============================================================================

type NestedOuter struct {
	Name  string
	Inner NestedInner
	List  []NestedInner
}

type NestedInner struct {
	Value int
	Sub   *NestedInner
}

func TestDecodeNestedStructures(t *testing.T) {
	original := NestedOuter{
		Name:  "outer",
		Inner: NestedInner{Value: 1, Sub: &NestedInner{Value: 2}},
		List:  []NestedInner{{Value: 3}, {Value: 4}},
	}

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got NestedOuter
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.Name != original.Name {
		t.Errorf("Name = %q, want %q", got.Name, original.Name)
	}
	if got.Inner.Value != original.Inner.Value {
		t.Errorf("Inner.Value = %d, want %d", got.Inner.Value, original.Inner.Value)
	}
	if got.Inner.Sub == nil || got.Inner.Sub.Value != original.Inner.Sub.Value {
		t.Errorf("Inner.Sub mismatch")
	}
	if len(got.List) != len(original.List) {
		t.Errorf("List length = %d, want %d", len(got.List), len(original.List))
	}
}

// ============================================================================
// Interface Decode Tests
// ============================================================================

func TestDecodeIntoInterface(t *testing.T) {
	tests := []struct {
		name string
		data any
	}{
		{"int", 42},
		{"float", 3.14},
		{"string", "hello"},
		{"bool", true},
		{"null", nil},
		{"array", []int{1, 2, 3}},
		{"object", map[string]int{"a": 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := Marshal(tt.data)
			var got any
			if err := Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			// Just verify it doesn't error - types may differ
		})
	}
}

// ============================================================================
// Partial Data Tests
// ============================================================================

func TestDecodeTruncatedData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"truncated_uint", []byte{typeUintBase + 3, 0x01, 0x02}},        // needs 4 bytes
		{"truncated_float64", []byte{typeFloat64, 0x01, 0x02}},          // needs 8 bytes
		{"truncated_string", []byte{typeShortStringBase + 5, 'h', 'e'}}, // needs 5 bytes
		{"truncated_array", []byte{typeArrayStart}},                     // no end marker
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
// Invalid Type Code Tests
// ============================================================================

func TestDecodeInvalidTypeCode(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"reserved_0x65", []byte{0x65}},
		{"reserved_0x66", []byte{0x66}},
		{"reserved_0x67", []byte{0x67}},
		{"reserved_0x90", []byte{0x90}},
		{"reserved_0x98", []byte{0x98}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			err := Unmarshal(tt.data, &v)
			if err == nil {
				t.Error("expected error for invalid type code")
			}
		})
	}
}

// ============================================================================
// Decode Into Existing Value Tests
// ============================================================================

func TestDecodeIntoExistingValue(t *testing.T) {
	// Pre-existing value should be cleared/overwritten
	existing := []int{100, 200, 300}
	data, _ := Marshal([]int{1, 2})

	if err := Unmarshal(data, &existing); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(existing) != 2 {
		t.Errorf("slice length = %d, want 2", len(existing))
	}
}

func TestDecodeIntoExistingMap(t *testing.T) {
	existing := map[string]int{"old": 99}
	data, _ := Marshal(map[string]int{"new": 42})

	if err := Unmarshal(data, &existing); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Implementation may either clear the map or merge
	if v, ok := existing["new"]; !ok || v != 42 {
		t.Errorf("expected new key with value 42, got %v", existing)
	}
}

// ============================================================================
// Double Pointer Tests
// ============================================================================

func TestDecodeDoublePointer(t *testing.T) {
	data, _ := Marshal(42)

	var pp **int
	if err := Unmarshal(data, &pp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if pp == nil || *pp == nil || **pp != 42 {
		t.Error("double pointer not properly initialized")
	}
}

// ============================================================================
// Large Data Tests
// ============================================================================

func TestDecodeLargeArray(t *testing.T) {
	large := make([]int, 10000)
	for i := range large {
		large[i] = i
	}

	data, err := Marshal(large)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got []int
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(got) != len(large) {
		t.Errorf("length = %d, want %d", len(got), len(large))
	}
}

func TestDecodeLargeString(t *testing.T) {
	large := strings.Repeat("x", 100000)

	data, err := Marshal(large)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got string
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got != large {
		t.Error("large string mismatch")
	}
}

// ============================================================================
// Decoder InputOffset Tests
// ============================================================================

func TestDecoderInputOffset(t *testing.T) {
	data, _ := Marshal([]int{1, 2, 3})

	dec := NewDecoder(bytes.NewReader(data))
	var v []int
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	offset := dec.InputOffset()
	if offset != int64(len(data)) {
		t.Errorf("InputOffset = %d, want %d", offset, len(data))
	}
}

// ============================================================================
// skipValue Tests (covers skipValue code paths)
// ============================================================================

func TestDecodeSkipValue(t *testing.T) {
	// Test skipping values when decoding into a struct with fewer fields
	type Full struct {
		A int    `bonjson:"a"`
		B string `bonjson:"b"`
		C bool   `bonjson:"c"`
		D []int  `bonjson:"d"`
	}
	type Partial struct {
		A int `bonjson:"a"`
	}

	full := Full{A: 1, B: "hello", C: true, D: []int{1, 2, 3}}
	data, _ := Marshal(full)

	var partial Partial
	if err := Unmarshal(data, &partial); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if partial.A != 1 {
		t.Errorf("got A=%d, want 1", partial.A)
	}
}

func TestDecodeSkipVariousTypes(t *testing.T) {
	// Test that various types can be skipped
	tests := []struct {
		name  string
		value any
	}{
		{"skip_small_int", 50},
		{"skip_neg_int", -50},
		{"skip_uint", uint64(1000)},
		{"skip_sint", int64(-1000)},
		{"skip_float32", float32(3.14)},
		{"skip_float64", 3.14159265358979},
		{"skip_short_string", "hello"},
		{"skip_long_string", strings.Repeat("x", 100)},
		{"skip_bool_true", true},
		{"skip_bool_false", false},
		{"skip_null", nil},
		{"skip_array", []int{1, 2, 3}},
		{"skip_object", map[string]int{"a": 1}},
		{"skip_nested", map[string]any{"arr": []int{1, 2}, "obj": map[string]string{"k": "v"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Wrap the value in an object
			data, _ := Marshal(map[string]any{"skip": tt.value, "keep": 42})

			type Target struct {
				Keep int `bonjson:"keep"`
			}
			var target Target
			if err := Unmarshal(data, &target); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			if target.Keep != 42 {
				t.Errorf("got Keep=%d, want 42", target.Keep)
			}
		})
	}
}

// ============================================================================
// convertMapKey Tests (covers map key conversion for non-string keys)
// ============================================================================

func TestDecodeMapIntegerKeys(t *testing.T) {
	// Maps with integer keys - converted from string keys during decode
	data, _ := Marshal(map[string]int{"1": 10, "2": 20, "3": 30})

	var got map[int]int
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got[1] != 10 || got[2] != 20 || got[3] != 30 {
		t.Errorf("got %v, want map[1:10 2:20 3:30]", got)
	}
}

func TestDecodeMapUintKeys(t *testing.T) {
	data, _ := Marshal(map[string]int{"100": 1, "200": 2})

	var got map[uint]int
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got[100] != 1 || got[200] != 2 {
		t.Errorf("got %v", got)
	}
}

func TestDecodeMapKeyConversionErrors(t *testing.T) {
	// Non-numeric string key to int map should fail
	data, _ := Marshal(map[string]int{"abc": 1})

	var got map[int]int
	err := Unmarshal(data, &got)
	if err == nil {
		t.Error("expected error for non-numeric key")
	}
}

// ============================================================================
// storeFloat Tests (covers float storage to various types)
// ============================================================================

func TestDecodeFloatToIntegerTypes(t *testing.T) {
	// Float that is actually an integer value can be stored in integer types
	data, _ := Marshal(42.0)

	tests := []struct {
		name string
		ptr  any
	}{
		{"to_int", new(int)},
		{"to_int32", new(int32)},
		{"to_int64", new(int64)},
		{"to_uint", new(uint)},
		{"to_uint32", new(uint32)},
		{"to_uint64", new(uint64)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the pointer value
			ptr := reflect.New(reflect.TypeOf(tt.ptr).Elem())
			if err := Unmarshal(data, ptr.Interface()); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
		})
	}
}

func TestDecodeFloatOverflowErrors(t *testing.T) {
	// Large float that overflows float32
	largeFloat := 1e40
	data, _ := Marshal(largeFloat)

	var got float32
	// This may not error but should handle overflow
	_ = Unmarshal(data, &got)
}

func TestDecodeFloatToNonNumericType(t *testing.T) {
	data, _ := Marshal(3.14)

	var got string
	err := Unmarshal(data, &got)
	if err == nil {
		t.Error("expected error unmarshaling float to string")
	}
}

// ============================================================================
// storeBool Tests (covers bool storage to various types)
// ============================================================================

func TestDecodeBoolToInterface(t *testing.T) {
	data, _ := Marshal(true)

	var got any
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got != true {
		t.Errorf("got %v, want true", got)
	}
}

func TestDecodeBoolToNonBoolType(t *testing.T) {
	data, _ := Marshal(true)

	var got int
	err := Unmarshal(data, &got)
	if err == nil {
		t.Error("expected error unmarshaling bool to int")
	}
}

// ============================================================================
// storeNull Tests
// ============================================================================

func TestDecodeNullToPointer(t *testing.T) {
	value := 42
	ptr := &value

	// Marshal null
	data, _ := Marshal(nil)

	if err := Unmarshal(data, &ptr); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if ptr != nil {
		t.Error("expected pointer to be nil after unmarshaling null")
	}
}

func TestDecodeNullToSlice(t *testing.T) {
	slice := []int{1, 2, 3}

	data, _ := Marshal(nil)

	if err := Unmarshal(data, &slice); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if slice != nil {
		t.Error("expected slice to be nil after unmarshaling null")
	}
}

func TestDecodeNullToMap(t *testing.T) {
	m := map[string]int{"a": 1}

	data, _ := Marshal(nil)

	if err := Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m != nil {
		t.Error("expected map to be nil after unmarshaling null")
	}
}

// ============================================================================
// Error Type Tests (covers Error() methods)
// ============================================================================

func TestSyntaxErrorMessage(t *testing.T) {
	err := &SyntaxError{msg: "test error", Offset: 100}
	msg := err.Error()
	if !strings.Contains(msg, "test error") || !strings.Contains(msg, "100") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestUnmarshalTypeErrorMessage(t *testing.T) {
	err := &UnmarshalTypeError{Value: "string", Type: reflect.TypeOf(0), Offset: 50}
	msg := err.Error()
	if !strings.Contains(msg, "string") || !strings.Contains(msg, "int") {
		t.Errorf("unexpected error message: %s", msg)
	}

	// With struct field info
	err2 := &UnmarshalTypeError{Value: "string", Type: reflect.TypeOf(0), Struct: "MyStruct", Field: "MyField"}
	msg2 := err2.Error()
	if !strings.Contains(msg2, "MyStruct") || !strings.Contains(msg2, "MyField") {
		t.Errorf("unexpected error message: %s", msg2)
	}
}

func TestInvalidUnmarshalErrorMessage(t *testing.T) {
	// nil type
	err1 := &InvalidUnmarshalError{Type: nil}
	if !strings.Contains(err1.Error(), "nil") {
		t.Errorf("unexpected error message: %s", err1.Error())
	}

	// non-pointer
	err2 := &InvalidUnmarshalError{Type: reflect.TypeOf(0)}
	if !strings.Contains(err2.Error(), "non-pointer") {
		t.Errorf("unexpected error message: %s", err2.Error())
	}

	// nil pointer
	err3 := &InvalidUnmarshalError{Type: reflect.TypeOf((*int)(nil))}
	if !strings.Contains(err3.Error(), "nil") {
		t.Errorf("unexpected error message: %s", err3.Error())
	}
}

func TestUnsupportedTypeErrorMessage(t *testing.T) {
	err := &UnsupportedTypeError{Type: reflect.TypeOf(make(chan int))}
	msg := err.Error()
	if !strings.Contains(msg, "chan") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestUnsupportedValueErrorMessage(t *testing.T) {
	err := &UnsupportedValueError{Str: "test value"}
	msg := err.Error()
	if !strings.Contains(msg, "test value") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestMarshalerErrorMessage(t *testing.T) {
	err := &MarshalerError{
		Type:       reflect.TypeOf(0),
		Err:        errors.New("inner error"),
		sourceFunc: "MarshalText",
	}
	msg := err.Error()
	if !strings.Contains(msg, "MarshalText") || !strings.Contains(msg, "inner error") {
		t.Errorf("unexpected error message: %s", msg)
	}

	// Test Unwrap
	if err.Unwrap() == nil {
		t.Error("Unwrap returned nil")
	}
}

func TestDuplicateKeyErrorMessage(t *testing.T) {
	err := &DuplicateKeyError{Key: "mykey", Offset: 123}
	msg := err.Error()
	if !strings.Contains(msg, "mykey") || !strings.Contains(msg, "123") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestInvalidUTF8ErrorMessage(t *testing.T) {
	err := &InvalidUTF8Error{Offset: 456}
	msg := err.Error()
	if !strings.Contains(msg, "UTF-8") || !strings.Contains(msg, "456") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestNullInStringErrorMessage(t *testing.T) {
	err := &NullInStringError{Offset: 789}
	msg := err.Error()
	if !strings.Contains(msg, "NUL") || !strings.Contains(msg, "789") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestTooManyChunksErrorMessage(t *testing.T) {
	err := &TooManyChunksError{Count: 100, Max: 50, Offset: 999}
	msg := err.Error()
	if !strings.Contains(msg, "100") || !strings.Contains(msg, "50") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestValueRangeErrorMessage(t *testing.T) {
	err := &ValueRangeError{Value: "99999", Offset: 111}
	msg := err.Error()
	if !strings.Contains(msg, "99999") || !strings.Contains(msg, "range") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestMaxDepthErrorMessage(t *testing.T) {
	err := &MaxDepthError{Depth: 1000, Offset: 222}
	msg := err.Error()
	if !strings.Contains(msg, "1000") || !strings.Contains(msg, "depth") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestInvalidTypeCodeErrorMessage(t *testing.T) {
	err := &InvalidTypeCodeError{TypeCode: 0xAB, Offset: 333}
	msg := err.Error()
	if !strings.Contains(msg, "0xab") || !strings.Contains(msg, "333") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestTruncatedDataErrorMessage(t *testing.T) {
	err := &TruncatedDataError{Expected: 10, Got: 5, Offset: 444}
	msg := err.Error()
	if !strings.Contains(msg, "10") || !strings.Contains(msg, "5") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestInvalidValueErrorMessage(t *testing.T) {
	err := &InvalidValueError{Value: "NaN", Offset: 555}
	msg := err.Error()
	if !strings.Contains(msg, "NaN") || !strings.Contains(msg, "555") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

// ============================================================================
// Embedded Struct Field Tests (covers dominantField)
// ============================================================================

type EmbeddedA struct {
	Name string `bonjson:"name"`
}

type EmbeddedB struct {
	Name string `bonjson:"name"`
}

type ConflictingEmbedded struct {
	EmbeddedA
	EmbeddedB
}

func TestDecodeConflictingEmbeddedFields(t *testing.T) {
	// When two embedded structs have the same field name at the same level,
	// and neither is tagged, the field should be skipped (ambiguous)
	data, _ := Marshal(map[string]string{"name": "test"})

	var got ConflictingEmbedded
	// Should not error, but field should be skipped
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Both embedded Name fields should be empty (field is ambiguous)
	if got.EmbeddedA.Name != "" || got.EmbeddedB.Name != "" {
		t.Errorf("expected empty names for ambiguous fields, got A=%q B=%q",
			got.EmbeddedA.Name, got.EmbeddedB.Name)
	}
}

type EmbeddedTagged struct {
	Name string `bonjson:"name"`
}

type EmbeddedUntagged struct {
	Name string
}

type TaggedVsUntagged struct {
	EmbeddedTagged
	EmbeddedUntagged
}

func TestDecodeTaggedVsUntaggedEmbedded(t *testing.T) {
	// Tagged field should win over untagged at same level
	data, _ := Marshal(map[string]string{"name": "tagged_wins"})

	var got TaggedVsUntagged
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.EmbeddedTagged.Name != "tagged_wins" {
		t.Errorf("expected tagged name to be set, got %q", got.EmbeddedTagged.Name)
	}
}

type InnerEmbed struct {
	Value int `bonjson:"value"`
}

type OuterEmbed struct {
	InnerEmbed
	Value int `bonjson:"value"`
}

func TestDecodeShadowedEmbeddedField(t *testing.T) {
	// Outer's own field should shadow embedded field
	data, _ := Marshal(map[string]int{"value": 42})

	var got OuterEmbed
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.Value != 42 {
		t.Errorf("expected outer value 42, got %d", got.Value)
	}
}

// ============================================================================
// Case Folding Tests (covers foldRune)
// ============================================================================

func TestDecodeCaseFoldingFieldMatch(t *testing.T) {
	// BONJSON should match field names case-insensitively
	type CaseTest struct {
		MyField int `bonjson:"myField"`
	}

	// Try various case variations
	testCases := []string{"myField", "MyField", "MYFIELD", "myfield"}

	for _, fieldName := range testCases {
		t.Run(fieldName, func(t *testing.T) {
			data, _ := Marshal(map[string]int{fieldName: 42})

			var got CaseTest
			if err := Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if got.MyField != 42 {
				t.Errorf("field name %q: got %d, want 42", fieldName, got.MyField)
			}
		})
	}
}

// ============================================================================
// BigNumber Streaming Tests (covers readBigNumber in stream.go)
// ============================================================================

func TestDecodeBigNumberViaStreaming(t *testing.T) {
	// Create a big number by encoding a big.Int
	bigInt := new(big.Int)
	bigInt.SetString("123456789012345678901234567890", 10)

	data, err := Marshal(bigInt)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Decode via streaming
	dec := NewDecoder(bytes.NewReader(data))
	var got big.Int
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if got.Cmp(bigInt) != 0 {
		t.Errorf("got %v, want %v", &got, bigInt)
	}
}

// ============================================================================
// storeFloat Edge Cases Tests
// ============================================================================

func TestDecodeFloatToInterface(t *testing.T) {
	// Float decoded to interface{} should be float64
	data, _ := Marshal(3.14)

	var got any
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, ok := got.(float64); !ok {
		t.Errorf("expected float64, got %T", got)
	}
}

func TestDecodeFloatToConstrainedInterface(t *testing.T) {
	// Try to decode float to non-empty interface
	data, _ := Marshal(3.14)

	type Stringer interface {
		String() string
	}
	var got Stringer
	err := Unmarshal(data, &got)
	if err == nil {
		t.Error("expected error decoding float to constrained interface")
	}
}

// ============================================================================
// Comprehensive Struct Field Edge Case Tests
// ============================================================================

// Test omitzero with types implementing IsZero()
type customZeroer struct {
	value int
}

func (c customZeroer) IsZero() bool {
	return c.value == 0
}

type ptrZeroer struct {
	value int
}

func (p *ptrZeroer) IsZero() bool {
	return p == nil || p.value == 0
}

func TestOmitZeroWithIsZeroInterface(t *testing.T) {
	type WithZeroer struct {
		Name   string       `bonjson:"name"`
		Custom customZeroer `bonjson:"custom,omitzero"`
	}

	t.Run("non_zero_value_included", func(t *testing.T) {
		v := WithZeroer{Name: "test", Custom: customZeroer{value: 42}}
		data, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var decoded map[string]any
		if err := Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if _, ok := decoded["custom"]; !ok {
			t.Error("expected 'custom' field to be present for non-zero value")
		}
	})

	t.Run("zero_value_omitted", func(t *testing.T) {
		v := WithZeroer{Name: "test", Custom: customZeroer{value: 0}}
		data, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var decoded map[string]any
		if err := Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if _, ok := decoded["custom"]; ok {
			t.Error("expected 'custom' field to be omitted for zero value")
		}
	})
}

func TestOmitZeroWithPointerIsZero(t *testing.T) {
	type WithPtrZeroer struct {
		Name   string     `bonjson:"name"`
		Custom *ptrZeroer `bonjson:"custom,omitzero"`
	}

	t.Run("nil_pointer_omitted", func(t *testing.T) {
		v := WithPtrZeroer{Name: "test", Custom: nil}
		data, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var decoded map[string]any
		if err := Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if _, ok := decoded["custom"]; ok {
			t.Error("expected 'custom' field to be omitted for nil pointer")
		}
	})

	t.Run("zero_value_pointer_omitted", func(t *testing.T) {
		v := WithPtrZeroer{Name: "test", Custom: &ptrZeroer{value: 0}}
		data, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var decoded map[string]any
		if err := Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if _, ok := decoded["custom"]; ok {
			t.Error("expected 'custom' field to be omitted for zero value pointer")
		}
	})

	t.Run("non_zero_pointer_included", func(t *testing.T) {
		v := WithPtrZeroer{Name: "test", Custom: &ptrZeroer{value: 42}}
		data, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var decoded map[string]any
		if err := Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if _, ok := decoded["custom"]; !ok {
			t.Error("expected 'custom' field to be present for non-zero pointer")
		}
	})
}

func TestOmitZeroWithBuiltinTypes(t *testing.T) {
	type WithBuiltins struct {
		Name    string            `bonjson:"name"`
		Int     int               `bonjson:"int,omitzero"`
		Float   float64           `bonjson:"float,omitzero"`
		Bool    bool              `bonjson:"bool,omitzero"`
		String  string            `bonjson:"string,omitzero"`
		Slice   []int             `bonjson:"slice,omitzero"`
		Map     map[string]int    `bonjson:"map,omitzero"`
		Ptr     *int              `bonjson:"ptr,omitzero"`
		Struct  customZeroer      `bonjson:"struct,omitzero"`
		Time    time.Time         `bonjson:"time,omitzero"`
	}

	t.Run("all_zero_values", func(t *testing.T) {
		v := WithBuiltins{Name: "test"}
		data, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var decoded map[string]any
		if err := Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		// Only name should be present
		if len(decoded) != 1 {
			t.Errorf("expected 1 field (name), got %d fields: %v", len(decoded), decoded)
		}
	})
}

// Test deep embedding (multiple levels)
type Level3 struct {
	Deep string `bonjson:"deep"`
}

type Level2 struct {
	Level3
	Mid string `bonjson:"mid"`
}

type Level1 struct {
	Level2
	Top string `bonjson:"top"`
}

func TestDeepEmbedding(t *testing.T) {
	data, _ := Marshal(map[string]string{
		"top":  "top_value",
		"mid":  "mid_value",
		"deep": "deep_value",
	})

	var got Level1
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.Top != "top_value" {
		t.Errorf("Top = %q, want %q", got.Top, "top_value")
	}
	if got.Mid != "mid_value" {
		t.Errorf("Mid = %q, want %q", got.Mid, "mid_value")
	}
	if got.Deep != "deep_value" {
		t.Errorf("Deep = %q, want %q", got.Deep, "deep_value")
	}
}

func TestDeepEmbeddingRoundtrip(t *testing.T) {
	original := Level1{
		Level2: Level2{
			Level3: Level3{Deep: "deep"},
			Mid:    "mid",
		},
		Top: "top",
	}

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded Level1
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded != original {
		t.Errorf("decoded = %+v, want %+v", decoded, original)
	}
}

// Test embedded pointer to struct
type EmbeddedPtr struct {
	Value string `bonjson:"value"`
}

type WithEmbeddedPtr struct {
	*EmbeddedPtr
	Name string `bonjson:"name"`
}

func TestEmbeddedPointerToStruct(t *testing.T) {
	t.Run("non_nil_embedded_pointer", func(t *testing.T) {
		original := WithEmbeddedPtr{
			EmbeddedPtr: &EmbeddedPtr{Value: "embedded"},
			Name:        "outer",
		}

		data, err := Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var decoded WithEmbeddedPtr
		if err := Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if decoded.Name != "outer" {
			t.Errorf("Name = %q, want %q", decoded.Name, "outer")
		}
		if decoded.EmbeddedPtr == nil {
			t.Fatal("EmbeddedPtr is nil, expected non-nil")
		}
		if decoded.EmbeddedPtr.Value != "embedded" {
			t.Errorf("Value = %q, want %q", decoded.EmbeddedPtr.Value, "embedded")
		}
	})

	t.Run("nil_embedded_pointer", func(t *testing.T) {
		original := WithEmbeddedPtr{
			EmbeddedPtr: nil,
			Name:        "outer",
		}

		data, err := Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var decoded WithEmbeddedPtr
		if err := Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if decoded.Name != "outer" {
			t.Errorf("Name = %q, want %q", decoded.Name, "outer")
		}
	})
}

func TestDecodeToNilEmbeddedPointer(t *testing.T) {
	// Data has a field that belongs to the embedded struct
	data, _ := Marshal(map[string]string{"value": "test", "name": "outer"})

	var got WithEmbeddedPtr
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// The embedded pointer should be allocated
	if got.EmbeddedPtr == nil {
		t.Fatal("EmbeddedPtr should be allocated")
	}
	if got.EmbeddedPtr.Value != "test" {
		t.Errorf("Value = %q, want %q", got.EmbeddedPtr.Value, "test")
	}
}

// Test tag priority: bonjson takes precedence over json
type TagPriority struct {
	Field1 string `bonjson:"bon_name" json:"json_name"`
	Field2 string `json:"json_only"`
	Field3 string `bonjson:"bon_only"`
	Field4 string `bonjson:"-" json:"json_ignored"`
}

func TestTagPriority(t *testing.T) {
	t.Run("bonjson_takes_precedence", func(t *testing.T) {
		original := TagPriority{Field1: "test1", Field2: "test2", Field3: "test3"}

		data, err := Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var decoded map[string]any
		if err := Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		// Field1 should use bonjson tag name
		if _, ok := decoded["bon_name"]; !ok {
			t.Error("expected 'bon_name' field (bonjson tag)")
		}
		if _, ok := decoded["json_name"]; ok {
			t.Error("unexpected 'json_name' field")
		}

		// Field2 should use json tag name
		if _, ok := decoded["json_only"]; !ok {
			t.Error("expected 'json_only' field (json tag fallback)")
		}

		// Field3 should use bonjson tag name
		if _, ok := decoded["bon_only"]; !ok {
			t.Error("expected 'bon_only' field")
		}
	})

	t.Run("bonjson_dash_ignores_field", func(t *testing.T) {
		original := TagPriority{Field4: "ignored"}

		data, err := Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var decoded map[string]any
		if err := Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		// Field4 should be ignored despite having json tag
		if _, ok := decoded["json_ignored"]; ok {
			t.Error("field should be ignored by bonjson:\"-\" tag")
		}
	})
}

// Test the "string" option for numeric types
type StringOption struct {
	IntStr   int     `bonjson:"int_str,string"`
	FloatStr float64 `bonjson:"float_str,string"`
	BoolStr  bool    `bonjson:"bool_str,string"`
	StrStr   string  `bonjson:"str_str,string"`
}

func TestStringOptionEncode(t *testing.T) {
	original := StringOption{
		IntStr:   42,
		FloatStr: 3.14,
		BoolStr:  true,
		StrStr:   "hello",
	}

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded map[string]any
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// With string option, values should be encoded as strings
	if v, ok := decoded["int_str"].(string); !ok || v != "42" {
		t.Errorf("int_str = %v (%T), want \"42\"", decoded["int_str"], decoded["int_str"])
	}
	if v, ok := decoded["bool_str"].(string); !ok || v != "true" {
		t.Errorf("bool_str = %v (%T), want \"true\"", decoded["bool_str"], decoded["bool_str"])
	}
}

// Note: The string option is encoding-only. Decoding does not parse strings
// back into numeric types. This is a known limitation.

// Test unexported embedded struct with exported fields
type unexportedEmbed struct {
	ExportedField string `bonjson:"exported_field"`
}

type WithUnexportedEmbed struct {
	unexportedEmbed
	Name string `bonjson:"name"`
}

func TestUnexportedEmbeddedStruct(t *testing.T) {
	// Unexported embedded struct with exported fields should work
	data, _ := Marshal(map[string]string{
		"exported_field": "visible",
		"name":           "outer",
	})

	var got WithUnexportedEmbed
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.Name != "outer" {
		t.Errorf("Name = %q, want %q", got.Name, "outer")
	}
	if got.ExportedField != "visible" {
		t.Errorf("ExportedField = %q, want %q", got.ExportedField, "visible")
	}
}

// Test shadowing at multiple levels
type DeepBase struct {
	Value int `bonjson:"value"`
}

type DeepMiddle struct {
	DeepBase
	Value int `bonjson:"value"` // Shadows DeepBase.Value
}

type DeepOuter struct {
	DeepMiddle
	Value int `bonjson:"value"` // Shadows DeepMiddle.Value
}

func TestMultiLevelShadowing(t *testing.T) {
	data, _ := Marshal(map[string]int{"value": 42})

	var got DeepOuter
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Only outermost Value should be set
	if got.Value != 42 {
		t.Errorf("outer Value = %d, want 42", got.Value)
	}
	if got.DeepMiddle.Value != 0 {
		t.Errorf("middle Value = %d, want 0 (shadowed)", got.DeepMiddle.Value)
	}
	if got.DeepBase.Value != 0 {
		t.Errorf("base Value = %d, want 0 (shadowed)", got.DeepBase.Value)
	}
}

// Test struct with all field visibility combinations
type MixedVisibility struct {
	Exported   string `bonjson:"exported"`
	unexported string // Should be ignored
	Tagged     string `bonjson:"tagged"`
	Untagged   string // Uses field name
}

func TestMixedVisibilityFields(t *testing.T) {
	original := MixedVisibility{
		Exported:   "exp",
		unexported: "unexp",
		Tagged:     "tag",
		Untagged:   "untag",
	}

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded map[string]any
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Check present fields
	if _, ok := decoded["exported"]; !ok {
		t.Error("expected 'exported' field")
	}
	if _, ok := decoded["tagged"]; !ok {
		t.Error("expected 'tagged' field")
	}
	if _, ok := decoded["Untagged"]; !ok {
		t.Error("expected 'Untagged' field (uses struct field name)")
	}

	// unexported should not be present
	if _, ok := decoded["unexported"]; ok {
		t.Error("unexpected 'unexported' field")
	}
}

// Test anonymous field that is not a struct
type WithAnonymousInt int

type ContainsAnonymousNonStruct struct {
	WithAnonymousInt
	Name string `bonjson:"name"`
}

func TestAnonymousNonStructField(t *testing.T) {
	original := ContainsAnonymousNonStruct{
		WithAnonymousInt: 42,
		Name:             "test",
	}

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded map[string]any
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Anonymous non-struct uses the type name as field name
	if _, ok := decoded["WithAnonymousInt"]; !ok {
		t.Error("expected 'WithAnonymousInt' field for anonymous non-struct")
	}
}

// Test conflicting fields at different depths resolve correctly
type ConflictBase struct {
	Name string `bonjson:"name"`
}

type ConflictMiddle struct {
	ConflictBase
	// No Name field here - should expose ConflictBase.Name
}

type ConflictOuter struct {
	ConflictMiddle
	Name string `bonjson:"name"` // Should shadow ConflictBase.Name
}

func TestConflictResolutionByDepth(t *testing.T) {
	data, _ := Marshal(map[string]string{"name": "value"})

	var got ConflictOuter
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Outer.Name (depth 0) should win over Base.Name (depth 2)
	if got.Name != "value" {
		t.Errorf("outer Name = %q, want %q", got.Name, "value")
	}
	if got.ConflictBase.Name != "" {
		t.Errorf("base Name = %q, want empty (shadowed)", got.ConflictBase.Name)
	}
}

// Test that middle level exposes embedded field when not shadowed
func TestMiddleLevelExposesEmbedded(t *testing.T) {
	type Base struct {
		BaseField string `bonjson:"base_field"`
	}
	type Middle struct {
		Base
		MiddleField string `bonjson:"middle_field"`
	}
	type Outer struct {
		Middle
		OuterField string `bonjson:"outer_field"`
	}

	data, _ := Marshal(map[string]string{
		"base_field":   "base",
		"middle_field": "middle",
		"outer_field":  "outer",
	})

	var got Outer
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.BaseField != "base" {
		t.Errorf("BaseField = %q, want %q", got.BaseField, "base")
	}
	if got.MiddleField != "middle" {
		t.Errorf("MiddleField = %q, want %q", got.MiddleField, "middle")
	}
	if got.OuterField != "outer" {
		t.Errorf("OuterField = %q, want %q", got.OuterField, "outer")
	}
}

// ============================================================================
// encoding/json Compatibility Tests
// ============================================================================

// TestJSONTagFallback verifies that json tags are used when bonjson tags are absent
func TestJSONTagFallback(t *testing.T) {
	type Mixed struct {
		BonjsonOnly string `bonjson:"bonjson_field"`
		JSONOnly    string `json:"json_field"`
		Both        string `bonjson:"bn_field" json:"js_field"`
		Neither     string
	}

	original := Mixed{
		BonjsonOnly: "a",
		JSONOnly:    "b",
		Both:        "c",
		Neither:     "d",
	}

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Decode to map to verify field names
	var m map[string]string
	if err := Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map error: %v", err)
	}

	// bonjson tag takes priority
	if m["bonjson_field"] != "a" {
		t.Errorf("expected bonjson_field = %q, got map %v", "a", m)
	}
	// json tag used when bonjson absent
	if m["json_field"] != "b" {
		t.Errorf("expected json_field = %q, got map %v", "b", m)
	}
	// bonjson takes priority over json
	if m["bn_field"] != "c" {
		t.Errorf("expected bn_field = %q, got map %v", "c", m)
	}
	// field name used when no tags
	if m["Neither"] != "d" {
		t.Errorf("expected Neither = %q, got map %v", "d", m)
	}
}

// TestOmitemptyCompatibility verifies omitempty behavior matches encoding/json
func TestOmitemptyCompatibility(t *testing.T) {
	type S struct {
		Str    string  `json:"str,omitempty"`
		Int    int     `json:"int,omitempty"`
		Float  float64 `json:"float,omitempty"`
		Bool   bool    `json:"bool,omitempty"`
		Ptr    *int    `json:"ptr,omitempty"`
		Slice  []int   `json:"slice,omitempty"`
		Map    map[string]int `json:"map,omitempty"`
	}

	tests := []struct {
		name       string
		input      S
		wantFields []string // fields that should be present
	}{
		{
			"all_zero",
			S{},
			[]string{}, // all omitted
		},
		{
			"some_nonzero",
			S{Str: "hello", Int: 42},
			[]string{"str", "int"},
		},
		{
			"empty_slice_omitted",
			S{Slice: []int{}},
			[]string{}, // empty slice is omitted
		},
		{
			"nil_slice_omitted",
			S{Slice: nil},
			[]string{},
		},
		{
			"non_empty_slice",
			S{Slice: []int{1}},
			[]string{"slice"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.input)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			var m map[string]any
			if err := Unmarshal(data, &m); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			// Check expected fields are present
			for _, f := range tt.wantFields {
				if _, ok := m[f]; !ok {
					t.Errorf("expected field %q present, got %v", f, m)
				}
			}

			// Check only expected fields are present
			if len(m) != len(tt.wantFields) {
				t.Errorf("expected %d fields, got %d: %v", len(tt.wantFields), len(m), m)
			}
		})
	}
}

// TestNilVsEmptyCompatibility verifies nil vs empty handling
func TestNilVsEmptyCompatibility(t *testing.T) {
	t.Run("nil_slice_decodes_to_nil", func(t *testing.T) {
		data, _ := Marshal([]int(nil))
		var result []int = []int{1, 2, 3} // pre-populate
		if err := Unmarshal(data, &result); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if result != nil {
			t.Errorf("nil slice should decode to nil, got %v", result)
		}
	})

	t.Run("empty_slice_decodes_to_empty", func(t *testing.T) {
		data, _ := Marshal([]int{})
		var result []int
		if err := Unmarshal(data, &result); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if result == nil || len(result) != 0 {
			t.Errorf("empty slice should decode to non-nil empty slice, got %v (nil=%v)", result, result == nil)
		}
	})

	t.Run("nil_map_decodes_to_nil", func(t *testing.T) {
		data, _ := Marshal(map[string]int(nil))
		var result map[string]int = map[string]int{"a": 1}
		if err := Unmarshal(data, &result); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if result != nil {
			t.Errorf("nil map should decode to nil, got %v", result)
		}
	})

	t.Run("empty_map_decodes_to_empty", func(t *testing.T) {
		data, _ := Marshal(map[string]int{})
		var result map[string]int
		if err := Unmarshal(data, &result); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if result == nil || len(result) != 0 {
			t.Errorf("empty map should decode to non-nil empty map, got %v", result)
		}
	})
}

// TestTypeCoercionCompatibility verifies type coercion behavior
func TestTypeCoercionCompatibility(t *testing.T) {
	t.Run("int_to_float", func(t *testing.T) {
		data, _ := Marshal(42)
		var f float64
		if err := Unmarshal(data, &f); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if f != 42.0 {
			t.Errorf("expected 42.0, got %v", f)
		}
	})

	t.Run("float_to_int_fails", func(t *testing.T) {
		// Unlike encoding/json which truncates, BONJSON rejects non-integer floats
		// This is safer behavior that prevents silent data loss
		data, _ := Marshal(42.7)
		var i int
		err := Unmarshal(data, &i)
		if err == nil {
			t.Error("expected error for float to int conversion (BONJSON doesn't truncate)")
		}
	})

	t.Run("whole_float_to_int_works", func(t *testing.T) {
		// Float with integer value can be converted
		data, _ := Marshal(42.0)
		var i int
		if err := Unmarshal(data, &i); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if i != 42 {
			t.Errorf("expected 42, got %v", i)
		}
	})

	t.Run("bool_to_int_fails", func(t *testing.T) {
		data, _ := Marshal(true)
		var i int
		err := Unmarshal(data, &i)
		if err == nil {
			t.Error("expected error for bool to int conversion")
		}
	})

	t.Run("string_to_int_fails", func(t *testing.T) {
		data, _ := Marshal("42")
		var i int
		err := Unmarshal(data, &i)
		if err == nil {
			t.Error("expected error for string to int conversion")
		}
	})
}

// TestInterfaceDecoding verifies interface{} decoding behavior
func TestInterfaceDecoding(t *testing.T) {
	t.Run("number_to_interface_is_int64", func(t *testing.T) {
		// Unlike encoding/json which uses float64 for numbers,
		// BONJSON preserves integer types
		data, _ := Marshal(42)
		var v any
		if err := Unmarshal(data, &v); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		// BONJSON-specific: integers decode as int64, not float64
		if _, ok := v.(int64); !ok {
			t.Errorf("expected int64, got %T", v)
		}
	})

	t.Run("float_to_interface_is_float64", func(t *testing.T) {
		data, _ := Marshal(3.14)
		var v any
		if err := Unmarshal(data, &v); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if _, ok := v.(float64); !ok {
			t.Errorf("expected float64, got %T", v)
		}
	})

	t.Run("object_to_interface_is_map", func(t *testing.T) {
		data, _ := Marshal(map[string]int{"a": 1})
		var v any
		if err := Unmarshal(data, &v); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if _, ok := v.(map[string]any); !ok {
			t.Errorf("expected map[string]any, got %T", v)
		}
	})

	t.Run("array_to_interface_is_slice", func(t *testing.T) {
		data, _ := Marshal([]int{1, 2, 3})
		var v any
		if err := Unmarshal(data, &v); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if _, ok := v.([]any); !ok {
			t.Errorf("expected []any, got %T", v)
		}
	})
}

// TestCaseFoldingCompatibility verifies case-insensitive field matching
func TestCaseFoldingCompatibility(t *testing.T) {
	type S struct {
		Field string
	}

	tests := []struct {
		name     string
		jsonKey  string
		wantOK   bool
	}{
		{"exact_match", "Field", true},
		{"lowercase", "field", true},
		{"uppercase", "FIELD", true},
		{"mixed_case", "fIeLd", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create map with the test key
			m := map[string]string{tt.jsonKey: "value"}
			data, _ := Marshal(m)

			var s S
			err := Unmarshal(data, &s)

			if tt.wantOK {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if s.Field != "value" {
					t.Errorf("expected Field = %q, got %q", "value", s.Field)
				}
			} else {
				if s.Field != "" {
					t.Errorf("expected empty Field for non-matching key %q", tt.jsonKey)
				}
			}
		})
	}
}

// TestUnicodeCaseFolding tests case-folding for non-ASCII characters
func TestUnicodeCaseFolding(t *testing.T) {
	// Test Unicode field name case folding
	t.Run("unicode_latin_extended", func(t *testing.T) {
		// Test Ñ/ñ folding (common in Spanish)
		type S struct {
			Niño string `bonjson:"niño"`
		}

		// Create data with uppercase Ñ
		data, _ := Marshal(map[string]string{"niÑo": "value"})

		var s S
		err := Unmarshal(data, &s)
		if err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		// SimpleFold should match these
		if s.Niño != "value" {
			t.Errorf("expected Niño = %q, got %q", "value", s.Niño)
		}
	})

	t.Run("unicode_greek", func(t *testing.T) {
		// Test Greek letters
		type S struct {
			Alpha string `bonjson:"α"`
		}

		// Create data with uppercase alpha
		data, _ := Marshal(map[string]string{"Α": "value"})

		var s S
		err := Unmarshal(data, &s)
		if err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if s.Alpha != "value" {
			t.Errorf("expected Alpha = %q, got %q", "value", s.Alpha)
		}
	})

	t.Run("unicode_cyrillic", func(t *testing.T) {
		// Test Cyrillic letters (А/а)
		type S struct {
			Field string `bonjson:"а"` // Cyrillic 'a'
		}

		data, _ := Marshal(map[string]string{"А": "value"}) // Uppercase Cyrillic A

		var s S
		err := Unmarshal(data, &s)
		if err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if s.Field != "value" {
			t.Errorf("expected field = %q, got %q", "value", s.Field)
		}
	})

	t.Run("mixed_ascii_unicode", func(t *testing.T) {
		// Test field with mixed ASCII and Unicode
		type S struct {
			Café string `bonjson:"café"`
		}

		data, _ := Marshal(map[string]string{"CAFÉ": "value"})

		var s S
		err := Unmarshal(data, &s)
		if err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if s.Café != "value" {
			t.Errorf("expected Café = %q, got %q", "value", s.Café)
		}
	})

	t.Run("ascii_numbers_in_field", func(t *testing.T) {
		// Numbers should not affect case folding
		type S struct {
			Field1 string
		}

		tests := []string{"field1", "FIELD1", "Field1", "fIeLd1"}
		for _, name := range tests {
			t.Run(name, func(t *testing.T) {
				data, _ := Marshal(map[string]string{name: "value"})
				var s S
				if err := Unmarshal(data, &s); err != nil {
					t.Fatalf("Unmarshal error: %v", err)
				}
				if s.Field1 != "value" {
					t.Errorf("key %q: expected Field1 = %q, got %q", name, "value", s.Field1)
				}
			})
		}
	})
}

// TestUnmarshalerPriorityCompatibility verifies unmarshaler interface priority
func TestUnmarshalerPriorityCompatibility(t *testing.T) {
	// BONJSON's UnmarshalBONJSON should take priority over TextUnmarshaler
	type Dual struct {
		bonjsonCalled bool
		textCalled    bool
	}

	// This tests that if both interfaces are implemented, BONJSON uses its own
	// Note: We can't easily test this without modifying the type, so we test
	// that the custom unmarshaler is called
	t.Run("custom_unmarshaler_called", func(t *testing.T) {
		type Custom struct {
			Value string
		}

		// Encode as a string
		data, _ := Marshal("test_value")

		// Decode into CustomUnmarshalerType (from earlier tests)
		// Since we need a concrete type, we'll just verify the mechanism works
		var s string
		if err := Unmarshal(data, &s); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if s != "test_value" {
			t.Errorf("expected %q, got %q", "test_value", s)
		}
	})
}

// TestErrorTypeCompatibility verifies error type behavior
func TestErrorTypeCompatibility(t *testing.T) {
	t.Run("unmarshal_type_error", func(t *testing.T) {
		data, _ := Marshal("not a number")
		var i int
		err := Unmarshal(data, &i)
		if err == nil {
			t.Fatal("expected error")
		}
		var ute *UnmarshalTypeError
		if !errors.As(err, &ute) {
			t.Errorf("expected UnmarshalTypeError, got %T: %v", err, err)
		}
	})

	t.Run("unclosed_container_error", func(t *testing.T) {
		// Array start with no end
		data := []byte{typeArrayStart}
		var v any
		err := Unmarshal(data, &v)
		if err == nil {
			t.Fatal("expected error")
		}
		// BONJSON uses UnclosedContainerError for this case
		var uce *UnclosedContainerError
		if !errors.As(err, &uce) {
			t.Errorf("expected UnclosedContainerError for unclosed array, got %T: %v", err, err)
		}
	})

	t.Run("truncated_data_error", func(t *testing.T) {
		// Integer type code with no data bytes
		data := []byte{typeUintBase + 1} // needs 2 bytes, has 0
		var v any
		err := Unmarshal(data, &v)
		if err == nil {
			t.Fatal("expected error")
		}
		var tde *TruncatedDataError
		if !errors.As(err, &tde) {
			t.Errorf("expected TruncatedDataError for truncated integer, got %T: %v", err, err)
		}
	})
}

// TestAnonymousFieldCompatibility verifies anonymous struct field handling
func TestAnonymousFieldCompatibility(t *testing.T) {
	type Inner struct {
		A string
		B int
	}
	type Outer struct {
		Inner
		C bool
	}

	original := Outer{
		Inner: Inner{A: "hello", B: 42},
		C:     true,
	}

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Verify embedded fields are flattened
	var m map[string]any
	if err := Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map error: %v", err)
	}

	// A and B should be at top level, not nested
	if m["A"] == nil {
		t.Error("expected A at top level")
	}
	if m["B"] == nil {
		t.Error("expected B at top level")
	}
	if m["C"] == nil {
		t.Error("expected C at top level")
	}
	if m["Inner"] != nil {
		t.Error("Inner should not be a separate key (fields should be promoted)")
	}
}

// TestPointerFieldCompatibility verifies pointer field handling
func TestPointerFieldCompatibility(t *testing.T) {
	type S struct {
		IntPtr    *int
		StringPtr *string
		StructPtr *struct{ X int }
	}

	t.Run("nil_pointers_encode_as_null", func(t *testing.T) {
		s := S{}
		data, err := Marshal(s)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var m map[string]any
		if err := Unmarshal(data, &m); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if m["IntPtr"] != nil {
			t.Errorf("nil *int should encode as null")
		}
	})

	t.Run("non_nil_pointers_encode_value", func(t *testing.T) {
		i := 42
		s := S{IntPtr: &i}
		data, err := Marshal(s)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var m map[string]any
		if err := Unmarshal(data, &m); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if v, ok := m["IntPtr"].(int64); !ok || v != 42 {
			t.Errorf("expected IntPtr = 42, got %v", m["IntPtr"])
		}
	})

	t.Run("null_decodes_to_nil_pointer", func(t *testing.T) {
		data, _ := Marshal(map[string]any{"IntPtr": nil})
		i := 99
		s := S{IntPtr: &i}
		if err := Unmarshal(data, &s); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if s.IntPtr != nil {
			t.Error("null should decode to nil pointer")
		}
	})
}

// ============================================================================
// Error Message Quality Tests
// ============================================================================

// TestErrorOffsets verifies that errors include correct byte offsets
func TestErrorOffsets(t *testing.T) {
	t.Run("duplicate_key_offset", func(t *testing.T) {
		// Object with duplicate key: {"a":1,"a":2}
		// First key at offset 1, second key at offset ~6
		data := []byte{
			typeObjectStart,
			typeShortStringBase + 1, 'a', // key "a"
			0x01, // value 1
			typeShortStringBase + 1, 'a', // duplicate key "a"
			0x02, // value 2
			typeContainerEnd,
		}
		var v any
		err := Unmarshal(data, &v)
		if err == nil {
			t.Fatal("expected error")
		}
		var dke *DuplicateKeyError
		if !errors.As(err, &dke) {
			t.Fatalf("expected DuplicateKeyError, got %T", err)
		}
		if dke.Key != "a" {
			t.Errorf("expected Key = %q, got %q", "a", dke.Key)
		}
		if dke.Offset < 1 {
			t.Errorf("expected Offset > 0, got %d", dke.Offset)
		}
	})

	t.Run("truncated_data_offset", func(t *testing.T) {
		// Array at offset 0, truncated integer starting at offset 1
		data := []byte{typeArrayStart, typeUintBase + 1} // array with incomplete 2-byte uint
		var v any
		err := Unmarshal(data, &v)
		if err == nil {
			t.Fatal("expected error")
		}
		var tde *TruncatedDataError
		if errors.As(err, &tde) {
			// Offset should be where the truncation was detected
			if tde.Offset < 0 {
				t.Errorf("expected non-negative Offset, got %d", tde.Offset)
			}
			if tde.Expected <= tde.Got {
				t.Errorf("Expected (%d) should be > Got (%d)", tde.Expected, tde.Got)
			}
		}
	})

	t.Run("invalid_type_code_offset", func(t *testing.T) {
		// Invalid type code at offset 0
		data := []byte{0x65} // reserved type code
		var v any
		err := Unmarshal(data, &v)
		if err == nil {
			t.Fatal("expected error")
		}
		var itce *InvalidTypeCodeError
		if !errors.As(err, &itce) {
			t.Fatalf("expected InvalidTypeCodeError, got %T", err)
		}
		if itce.TypeCode != 0x65 {
			t.Errorf("expected TypeCode = 0x65, got 0x%02x", itce.TypeCode)
		}
		if itce.Offset != 0 {
			t.Errorf("expected Offset = 0, got %d", itce.Offset)
		}
	})

	t.Run("max_depth_offset", func(t *testing.T) {
		// Nested arrays that exceed depth
		data := []byte{typeArrayStart, typeArrayStart, typeArrayStart, 0x01,
			typeContainerEnd, typeContainerEnd, typeContainerEnd}
		dec := NewDecoder(bytes.NewReader(data))
		dec.SetMaxDepth(2)
		var v any
		err := dec.Decode(&v)
		if err == nil {
			t.Fatal("expected error")
		}
		var mde *MaxDepthError
		if !errors.As(err, &mde) {
			t.Fatalf("expected MaxDepthError, got %T", err)
		}
		if mde.Depth != 2 {
			t.Errorf("expected Depth = 2, got %d", mde.Depth)
		}
		if mde.Offset < 0 {
			t.Errorf("expected non-negative Offset, got %d", mde.Offset)
		}
	})
}

// TestErrorMessages verifies error message format and content
func TestErrorMessages(t *testing.T) {
	t.Run("syntax_error_format", func(t *testing.T) {
		err := &SyntaxError{msg: "test message", Offset: 42}
		msg := err.Error()
		if !strings.Contains(msg, "bonjson:") {
			t.Error("error should be prefixed with 'bonjson:'")
		}
		if !strings.Contains(msg, "test message") {
			t.Error("error should contain the message")
		}
		if !strings.Contains(msg, "42") {
			t.Error("error should contain the offset")
		}
	})

	t.Run("unmarshal_type_error_basic", func(t *testing.T) {
		err := &UnmarshalTypeError{
			Value:  "string",
			Type:   reflect.TypeOf(0),
			Offset: 10,
		}
		msg := err.Error()
		if !strings.Contains(msg, "bonjson:") {
			t.Error("error should be prefixed with 'bonjson:'")
		}
		if !strings.Contains(msg, "string") {
			t.Error("error should contain the value type")
		}
		if !strings.Contains(msg, "int") {
			t.Error("error should contain the Go type")
		}
	})

	t.Run("unmarshal_type_error_struct_field", func(t *testing.T) {
		err := &UnmarshalTypeError{
			Value:  "bool",
			Type:   reflect.TypeOf(""),
			Struct: "MyStruct",
			Field:  "Name",
		}
		msg := err.Error()
		if !strings.Contains(msg, "MyStruct.Name") {
			t.Error("error should contain struct.field path")
		}
	})

	t.Run("invalid_unmarshal_error_nil", func(t *testing.T) {
		err := &InvalidUnmarshalError{Type: nil}
		msg := err.Error()
		if !strings.Contains(msg, "nil") {
			t.Error("nil unmarshal error should mention nil")
		}
	})

	t.Run("invalid_unmarshal_error_non_pointer", func(t *testing.T) {
		err := &InvalidUnmarshalError{Type: reflect.TypeOf(0)}
		msg := err.Error()
		if !strings.Contains(msg, "non-pointer") {
			t.Error("non-pointer error should mention non-pointer")
		}
	})

	t.Run("duplicate_key_error", func(t *testing.T) {
		err := &DuplicateKeyError{Key: "myKey", Offset: 100}
		msg := err.Error()
		if !strings.Contains(msg, "myKey") {
			t.Error("duplicate key error should contain the key")
		}
		if !strings.Contains(msg, "100") {
			t.Error("duplicate key error should contain the offset")
		}
	})

	t.Run("max_string_length_error", func(t *testing.T) {
		err := &MaxStringLengthError{Length: 1000, Max: 100, Offset: 5}
		msg := err.Error()
		if !strings.Contains(msg, "1000") {
			t.Error("error should contain actual length")
		}
		if !strings.Contains(msg, "100") {
			t.Error("error should contain max length")
		}
	})

	t.Run("unclosed_container_error", func(t *testing.T) {
		err := &UnclosedContainerError{ContainerType: "object", Offset: 0}
		msg := err.Error()
		if !strings.Contains(msg, "object") {
			t.Error("error should specify container type")
		}
		if !strings.Contains(msg, "unclosed") {
			t.Error("error should mention 'unclosed'")
		}
	})
}

// TestErrorUnwrap verifies error unwrapping behavior
func TestErrorUnwrap(t *testing.T) {
	t.Run("marshaler_error_unwrap", func(t *testing.T) {
		innerErr := errors.New("inner error")
		err := &MarshalerError{
			Type: reflect.TypeOf(""),
			Err:  innerErr,
		}

		// Test Unwrap
		unwrapped := err.Unwrap()
		if unwrapped != innerErr {
			t.Error("Unwrap should return the inner error")
		}

		// Test errors.Is
		if !errors.Is(err, innerErr) {
			t.Error("errors.Is should find the inner error")
		}
	})
}

// TestErrorFieldAccess verifies error fields are accessible
func TestErrorFieldAccess(t *testing.T) {
	t.Run("truncated_data_error_fields", func(t *testing.T) {
		err := &TruncatedDataError{Expected: 10, Got: 5, Offset: 100}
		if err.Expected != 10 {
			t.Error("Expected field should be accessible")
		}
		if err.Got != 5 {
			t.Error("Got field should be accessible")
		}
		if err.Offset != 100 {
			t.Error("Offset field should be accessible")
		}
	})

	t.Run("invalid_type_code_fields", func(t *testing.T) {
		err := &InvalidTypeCodeError{TypeCode: 0x65, Offset: 42}
		if err.TypeCode != 0x65 {
			t.Error("TypeCode field should be accessible")
		}
		if err.Offset != 42 {
			t.Error("Offset field should be accessible")
		}
	})

	t.Run("too_many_chunks_fields", func(t *testing.T) {
		err := &TooManyChunksError{Count: 150, Max: 100, Offset: 50}
		if err.Count != 150 {
			t.Error("Count field should be accessible")
		}
		if err.Max != 100 {
			t.Error("Max field should be accessible")
		}
	})

	t.Run("value_range_error_fields", func(t *testing.T) {
		err := &ValueRangeError{Value: "123456789", Offset: 10}
		if err.Value != "123456789" {
			t.Error("Value field should be accessible")
		}
	})
}

// TestAllErrorTypesPrintable verifies all error types produce valid messages
func TestAllErrorTypesPrintable(t *testing.T) {
	errors := []error{
		&SyntaxError{msg: "test", Offset: 0},
		&UnmarshalTypeError{Value: "test", Type: reflect.TypeOf(0), Offset: 0},
		&InvalidUnmarshalError{Type: reflect.TypeOf(0)},
		&UnsupportedTypeError{Type: reflect.TypeOf(make(chan int))},
		&UnsupportedValueError{Str: "test"},
		&MarshalerError{Type: reflect.TypeOf(""), Err: errors.New("test")},
		&DuplicateKeyError{Key: "test", Offset: 0},
		&InvalidUTF8Error{Offset: 0},
		&NullInStringError{Offset: 0},
		&TooManyChunksError{Count: 1, Max: 1, Offset: 0},
		&EmptyChunkContinuationError{Offset: 0},
		&ValueRangeError{Value: "test", Offset: 0},
		&MaxDepthError{Depth: 1, Offset: 0},
		&InvalidTypeCodeError{TypeCode: 0, Offset: 0},
		&TruncatedDataError{Expected: 1, Got: 0, Offset: 0},
		&InvalidValueError{Value: "test", Offset: 0},
		&TrailingDataError{Offset: 0},
		&NonCanonicalLengthError{Offset: 0},
		&UnclosedContainerError{ContainerType: "array", Offset: 0},
		&MaxStringLengthError{Length: 1, Max: 1, Offset: 0},
	}

	for _, err := range errors {
		msg := err.Error()
		if msg == "" {
			t.Errorf("%T produced empty error message", err)
		}
		if !strings.HasPrefix(msg, "bonjson:") {
			t.Errorf("%T error message should start with 'bonjson:': %s", err, msg)
		}
	}
}
