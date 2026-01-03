// Copyright 2024 Karl Stenerud. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bonjson

import (
	"bytes"
	"errors"
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
