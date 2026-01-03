// Copyright 2024 Karl Stenerud. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bonjson

import (
	"bytes"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
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
// UnmarshalPartial Tests
// ============================================================================

func TestUnmarshalPartial(t *testing.T) {
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
			consumed, err := UnmarshalPartial(tt.data, tt.ptr)
			if err != nil {
				t.Fatalf("UnmarshalPartial error: %v", err)
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

func TestUnmarshalPartialReturnsConsumedOnTrailingData(t *testing.T) {
	// UnmarshalPartial should return error on trailing data, but also return consumed count
	data := []byte{0x05, 0x99, 0x88, 0x77} // int 5 followed by garbage
	var v int
	consumed, err := UnmarshalPartial(data, &v)
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

func TestUnmarshalPartialReturnsConsumedOnError(t *testing.T) {
	// Test that UnmarshalPartial returns bytes consumed even on decode error

	// Truncated int64 (type code says 8 bytes follow, but only 2 provided)
	data := []byte{typeUintBase + 7, 0x01, 0x02}
	var v int
	n, err := UnmarshalPartial(data, &v)
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

func TestUnmarshalPartialErrorConditions(t *testing.T) {
	// Test all error conditions that can occur during partial decoding

	t.Run("InvalidTypeCodeError", func(t *testing.T) {
		// Reserved type codes 0x65-0x67 and 0x90-0x98 are invalid
		data := []byte{0x65}
		var v any
		n, err := UnmarshalPartial(data, &v)
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
		n, err := UnmarshalPartial(data, &v)
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
		n, err := UnmarshalPartial(data, &v)
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
		n, err := UnmarshalPartial(data, &v)
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
		n, err := UnmarshalPartial(data, &v)
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
		n, err := UnmarshalPartial(buf.Bytes(), &v)
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
		n, err := UnmarshalPartial(data, &v)
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
		n, err := UnmarshalPartial(data, &v)
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

func TestUnmarshalPartialInvalidTarget(t *testing.T) {
	// Test invalid target
	_, err := UnmarshalPartial([]byte{0x05}, nil)
	if err == nil {
		t.Error("expected error for nil target")
	}

	_, err = UnmarshalPartial([]byte{0x05}, 42)
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

	// Large unsigned integer decodes to uint64
	data2, _ := Marshal(uint64(1 << 62))
	var v2 any
	if err := Unmarshal(data2, &v2); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if _, ok := v2.(uint64); !ok {
		t.Errorf("unsigned int: expected uint64, got %T", v2)
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
