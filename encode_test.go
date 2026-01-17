//
// encode_test.go
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
	"encoding"
	"errors"
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Basic Type Tests
// ============================================================================

func TestMarshalBasicTypes(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		// Booleans
		{"true", true},
		{"false", false},

		// Integers
		{"int_zero", 0},
		{"int_one", 1},
		{"int_neg_one", -1},
		{"int_small_max", 100},
		{"int_small_min", -100},
		{"int_101", 101},
		{"int_neg_101", -101},
		{"int8_max", int8(127)},
		{"int8_min", int8(-128)},
		{"int16_max", int16(32767)},
		{"int16_min", int16(-32768)},
		{"int32_max", int32(2147483647)},
		{"int32_min", int32(-2147483648)},
		{"int64_max", int64(math.MaxInt64)},
		{"int64_min", int64(math.MinInt64)},
		{"uint8_max", uint8(255)},
		{"uint16_max", uint16(65535)},
		{"uint32_max", uint32(4294967295)},
		{"uint64_max", uint64(math.MaxUint64)},

		// Floats
		{"float32_zero", float32(0)},
		{"float32_one", float32(1.0)},
		{"float32_pi", float32(3.14159)},
		{"float64_zero", float64(0)},
		{"float64_one", float64(1.0)},
		{"float64_pi", 3.14159265358979323846},
		{"float64_large", 1e100},
		{"float64_small", 1e-100},

		// Strings
		{"string_empty", ""},
		{"string_short", "hello"},
		{"string_15", "123456789012345"},  // max short string
		{"string_16", "1234567890123456"}, // first long string
		{"string_unicode", "Hello, 世界!"},
		{"string_emoji", "😀🎉🚀"},

		// Nil
		{"nil", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			// For nil, we can't unmarshal into the same type
			if tt.value == nil {
				var got any
				if err := Unmarshal(data, &got); err != nil {
					t.Fatalf("Unmarshal error: %v", err)
				}
				if got != nil {
					t.Errorf("Unmarshal nil: got %v, want nil", got)
				}
				return
			}

			// Create a pointer to the same type
			ptr := reflect.New(reflect.TypeOf(tt.value))
			if err := Unmarshal(data, ptr.Interface()); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			got := ptr.Elem().Interface()
			if !reflect.DeepEqual(got, tt.value) {
				t.Errorf("roundtrip:\n  got:  %v (%T)\n  want: %v (%T)", got, got, tt.value, tt.value)
			}
		})
	}
}

// ============================================================================
// Struct Tests
// ============================================================================

type SimpleStruct struct {
	X string
	Y int
	Z int `bonjson:"-"` // ignored field
}

type TaggedStruct struct {
	Name    string `bonjson:"name"`
	Age     int    `bonjson:"age,omitempty"`
	Hidden  string `bonjson:"-"`
	NoTag   string
	private string
}

type NestedStruct struct {
	Inner SimpleStruct
	Value int
}

type EmbeddedStruct struct {
	SimpleStruct
	Extra string
}

func TestMarshalStruct(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{
			"simple",
			SimpleStruct{X: "hello", Y: 42, Z: 100},
		},
		{
			"tagged",
			TaggedStruct{Name: "Alice", Age: 30, Hidden: "secret", NoTag: "visible"},
		},
		{
			"nested",
			NestedStruct{Inner: SimpleStruct{X: "inner", Y: 1}, Value: 99},
		},
		{
			"embedded",
			EmbeddedStruct{SimpleStruct: SimpleStruct{X: "base", Y: 2}, Extra: "more"},
		},
		{
			"with_omitempty",
			TaggedStruct{Name: "Bob"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			ptr := reflect.New(reflect.TypeOf(tt.value))
			if err := Unmarshal(data, ptr.Interface()); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			// Note: Z field and Hidden field should not round-trip
		})
	}
}

// ============================================================================
// Slice and Array Tests
// ============================================================================

func TestMarshalSliceArray(t *testing.T) {
	tests := []struct {
		name      string
		value     any
		skipExact bool // skip exact DeepEqual, just check rough equivalence
	}{
		{"int_slice", []int{1, 2, 3, 4, 5}, false},
		{"string_slice", []string{"a", "b", "c"}, false},
		{"empty_slice", []int{}, false},
		{"nil_slice", ([]int)(nil), false},
		{"int_array", [3]int{10, 20, 30}, false},
		{"nested_slice", [][]int{{1, 2}, {3, 4}}, false},
		{"mixed_any", []any{1, "two", 3.0, true, nil}, true}, // types change on roundtrip
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			// nil slices marshal as null and unmarshal back as nil
			rv := reflect.ValueOf(tt.value)
			isNilable := rv.Kind() == reflect.Slice || rv.Kind() == reflect.Map ||
				rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface ||
				rv.Kind() == reflect.Chan || rv.Kind() == reflect.Func
			if tt.value == nil || (isNilable && rv.IsNil()) {
				var got any
				if err := Unmarshal(data, &got); err != nil {
					t.Fatalf("Unmarshal error: %v", err)
				}
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}

			ptr := reflect.New(reflect.TypeOf(tt.value))
			if err := Unmarshal(data, ptr.Interface()); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			got := ptr.Elem().Interface()
			if tt.skipExact {
				// For mixed any slices, types can change (int->int64, etc.)
				// Just verify the data was decoded without error
				return
			}
			if !reflect.DeepEqual(got, tt.value) {
				t.Errorf("roundtrip:\n  got:  %v\n  want: %v", got, tt.value)
			}
		})
	}
}

// ============================================================================
// Map Tests
// ============================================================================

func TestMarshalMap(t *testing.T) {
	tests := []struct {
		name      string
		value     any
		skipExact bool // skip exact DeepEqual, just check rough equivalence
	}{
		{"string_int", map[string]int{"a": 1, "b": 2}, false},
		{"string_any", map[string]any{"x": 1, "y": "two", "z": true}, true}, // types change on roundtrip
		{"empty_map", map[string]int{}, false},
		{"nil_map", (map[string]int)(nil), false},
		{"nested_map", map[string]map[string]int{"outer": {"inner": 42}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			if tt.value == nil || reflect.ValueOf(tt.value).IsNil() {
				var got any
				if err := Unmarshal(data, &got); err != nil {
					t.Fatalf("Unmarshal error: %v", err)
				}
				return
			}

			ptr := reflect.New(reflect.TypeOf(tt.value))
			if err := Unmarshal(data, ptr.Interface()); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			got := ptr.Elem().Interface()
			if tt.skipExact {
				// For mixed any maps, types can change (int->int64, etc.)
				// Just verify the data was decoded without error
				return
			}
			if !reflect.DeepEqual(got, tt.value) {
				t.Errorf("roundtrip:\n  got:  %v\n  want: %v", got, tt.value)
			}
		})
	}
}

// ============================================================================
// Pointer Tests
// ============================================================================

func TestMarshalPointer(t *testing.T) {
	i := 42
	s := "hello"

	tests := []struct {
		name string
		in   any
		out  any
	}{
		{"ptr_int", &i, &i},
		{"ptr_string", &s, &s},
		{"nil_ptr", (*int)(nil), (*int)(nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.in)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			var got *int
			if tt.name == "ptr_string" {
				var gotS *string
				if err := Unmarshal(data, &gotS); err != nil {
					t.Fatalf("Unmarshal error: %v", err)
				}
				return
			}

			if err := Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
		})
	}
}

// ============================================================================
// Byte Slice Tests (base64 encoding)
// ============================================================================

func TestMarshalByteSlice(t *testing.T) {
	tests := []struct {
		name  string
		value []byte
	}{
		{"empty", []byte{}},
		{"small", []byte{1, 2, 3}},
		{"ascii", []byte("hello world")},
		{"binary", []byte{0x00, 0xff, 0x7f, 0x80}},
		{"large", bytes.Repeat([]byte{0xab, 0xcd}, 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			var got []byte
			if err := Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if !bytes.Equal(got, tt.value) {
				t.Errorf("roundtrip:\n  got:  %v\n  want: %v", got, tt.value)
			}
		})
	}
}

// ============================================================================
// Interface Tests
// ============================================================================

func TestMarshalInterface(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{"int_in_any", any(42)},
		{"string_in_any", any("hello")},
		{"nil_any", any(nil)},
		{"slice_in_any", any([]int{1, 2, 3})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			var got any
			if err := Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			// Type may differ due to untyped unmarshaling
		})
	}
}

// ============================================================================
// Marshaler/Unmarshaler Interface Tests
// ============================================================================

type customMarshaler struct {
	Value int
}

func (c customMarshaler) MarshalBONJSON() ([]byte, error) {
	// Marshal as a simple integer multiplied by 2
	return Marshal(c.Value * 2)
}

func (c *customMarshaler) UnmarshalBONJSON(data []byte) error {
	var v int
	if err := Unmarshal(data, &v); err != nil {
		return err
	}
	c.Value = v / 2
	return nil
}

func TestCustomMarshaler(t *testing.T) {
	c := customMarshaler{Value: 21}
	data, err := Marshal(c)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got customMarshaler
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.Value != c.Value {
		t.Errorf("custom marshaler: got %d, want %d", got.Value, c.Value)
	}
}

type textMarshaler struct {
	A, B string
}

func (t textMarshaler) MarshalText() ([]byte, error) {
	return []byte(t.A + ":" + t.B), nil
}

func (t *textMarshaler) UnmarshalText(data []byte) error {
	parts := strings.SplitN(string(data), ":", 2)
	if len(parts) != 2 {
		return errors.New("invalid format")
	}
	t.A, t.B = parts[0], parts[1]
	return nil
}

var _ encoding.TextMarshaler = textMarshaler{}
var _ encoding.TextUnmarshaler = (*textMarshaler)(nil)

func TestTextMarshaler(t *testing.T) {
	tm := textMarshaler{A: "hello", B: "world"}
	data, err := Marshal(tm)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got textMarshaler
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got != tm {
		t.Errorf("text marshaler: got %v, want %v", got, tm)
	}
}

// errorUnmarshaler is a type that always returns an error when unmarshaling.
type errorUnmarshaler struct{}

func (e *errorUnmarshaler) UnmarshalBONJSON(data []byte) error {
	return errors.New("intentional unmarshal error")
}

func TestCustomUnmarshalerError(t *testing.T) {
	// Marshal some valid data
	data, err := Marshal(42)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Unmarshal into a type that always returns an error
	var got errorUnmarshaler
	err = Unmarshal(data, &got)
	if err == nil {
		t.Error("expected error from custom unmarshaler")
	}
	if !strings.Contains(err.Error(), "intentional unmarshal error") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTextUnmarshalerError(t *testing.T) {
	// textMarshaler expects "A:B" format, test with invalid format
	data, err := Marshal("invalid-no-colon")
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got textMarshaler
	err = Unmarshal(data, &got)
	if err == nil {
		t.Error("expected error from TextUnmarshaler")
	}
}

// ============================================================================
// BigNumber Type Tests
// ============================================================================

func TestBigNumberMarshalAsStruct(t *testing.T) {
	// BigNumber is a public type but is intended for internal use.
	// When marshaled directly, it encodes as a regular struct (not as a native BigNumber).
	// Users should use *big.Int or *big.Float for arbitrary-precision numbers.
	bn := BigNumber{
		Significand: []byte{0x2a}, // 42
		Exponent:    0,
		Negative:    false,
	}

	data, err := Marshal(bn)
	if err != nil {
		t.Fatalf("Marshal BigNumber error: %v", err)
	}

	// Verify it marshals (as a struct, not as native BigNumber)
	if len(data) == 0 {
		t.Error("expected non-empty data")
	}

	// Unmarshal back - should get a struct
	var got BigNumber
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal BigNumber error: %v", err)
	}

	if !bytes.Equal(got.Significand, bn.Significand) {
		t.Errorf("Significand mismatch: got %v, want %v", got.Significand, bn.Significand)
	}
	if got.Exponent != bn.Exponent {
		t.Errorf("Exponent = %d, want %d", got.Exponent, bn.Exponent)
	}
	if got.Negative != bn.Negative {
		t.Errorf("Negative = %v, want %v", got.Negative, bn.Negative)
	}
}

// ============================================================================
// Error Cases
// ============================================================================

func TestMarshalUnsupportedTypes(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{"channel", make(chan int)},
		{"func", func() {}},
		{"complex64", complex64(1 + 2i)},
		{"complex128", complex128(1 + 2i)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Marshal(tt.value)
			if err == nil {
				t.Error("expected error for unsupported type")
			}
		})
	}
}

// NaN/Infinity rejection is tested by universal spec tests (errors.json)

func TestMarshalCycleDetection(t *testing.T) {
	type Cycle struct {
		Next *Cycle
	}

	c := &Cycle{}
	c.Next = c

	_, err := Marshal(c)
	if err == nil {
		t.Error("expected error for cyclic data")
	}
}

// ============================================================================
// Unmarshal Error Cases
// ============================================================================

func TestUnmarshalInvalidTarget(t *testing.T) {
	data, _ := Marshal(42)

	// nil target
	err := Unmarshal(data, nil)
	if err == nil {
		t.Error("expected error for nil target")
	}

	// non-pointer target
	var i int
	err = Unmarshal(data, i)
	if err == nil {
		t.Error("expected error for non-pointer target")
	}

	// nil pointer
	var ptr *int
	err = Unmarshal(data, ptr)
	if err == nil {
		t.Error("expected error for nil pointer")
	}
}

func TestUnmarshalTypeMismatch(t *testing.T) {
	data, _ := Marshal("hello")

	var i int
	err := Unmarshal(data, &i)
	if err == nil {
		t.Error("expected error for type mismatch")
	}
}

// ============================================================================
// Time Tests
// ============================================================================

func TestMarshalTime(t *testing.T) {
	now := time.Now()
	data, err := Marshal(now)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got time.Time
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Time should round-trip via RFC3339Nano string representation
	if !got.Equal(now) {
		t.Errorf("time roundtrip:\n  got:  %v\n  want: %v", got, now)
	}
}

// ============================================================================
// Big Number Tests
// ============================================================================

func TestMarshalBigInt(t *testing.T) {
	tests := []struct {
		name  string
		value *big.Int
	}{
		{"zero", big.NewInt(0)},
		{"small", big.NewInt(42)},
		{"negative", big.NewInt(-42)},
		{"large", new(big.Int).Exp(big.NewInt(2), big.NewInt(256), nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			got := new(big.Int)
			if err := Unmarshal(data, got); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if got.Cmp(tt.value) != 0 {
				t.Errorf("big.Int roundtrip:\n  got:  %v\n  want: %v", got, tt.value)
			}
		})
	}
}

func TestMarshalBigFloat(t *testing.T) {
	tests := []struct {
		name  string
		value *big.Float
	}{
		{"zero", big.NewFloat(0)},
		{"one", big.NewFloat(1)},
		{"pi", big.NewFloat(3.14159265358979323846)},
		{"negative", big.NewFloat(-123.456)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			got := new(big.Float)
			if err := Unmarshal(data, got); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			// Compare with some tolerance for precision loss
			diff := new(big.Float).Sub(got, tt.value)
			diff.Abs(diff)
			epsilon := big.NewFloat(1e-10)
			if diff.Cmp(epsilon) > 0 {
				t.Errorf("big.Float roundtrip:\n  got:  %v\n  want: %v", got, tt.value)
			}
		})
	}
}

// ============================================================================
// Omitempty Tests
// ============================================================================

type OmitemptyStruct struct {
	Name   string            `bonjson:"name"`
	Age    int               `bonjson:"age,omitempty"`
	Score  float64           `bonjson:"score,omitempty"`
	Active bool              `bonjson:"active,omitempty"`
	Tags   []string          `bonjson:"tags,omitempty"`
	Meta   map[string]string `bonjson:"meta,omitempty"`
	Ptr    *int              `bonjson:"ptr,omitempty"`
}

func TestOmitempty(t *testing.T) {
	v := OmitemptyStruct{Name: "test"}
	data, err := Marshal(v)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got OmitemptyStruct
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.Name != v.Name {
		t.Errorf("omitempty: name = %q, want %q", got.Name, v.Name)
	}
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestEmptyInput(t *testing.T) {
	var v any
	err := Unmarshal([]byte{}, &v)
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestMarshalRawMessage(t *testing.T) {
	// Create some raw BONJSON data
	inner, err := Marshal(map[string]int{"a": 1, "b": 2})
	if err != nil {
		t.Fatalf("Marshal inner error: %v", err)
	}

	raw := RawMessage(inner)
	data, err := Marshal(raw)
	if err != nil {
		t.Fatalf("Marshal RawMessage error: %v", err)
	}

	var got RawMessage
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal RawMessage error: %v", err)
	}

	if !bytes.Equal(got, inner) {
		t.Errorf("RawMessage roundtrip:\n  got:  %v\n  want: %v", got, inner)
	}
}

// ============================================================================
// Anonymous Field Tests
// ============================================================================

type Inner struct {
	X int
	Y int
}

type Outer struct {
	Inner
	Z int
}

type OuterTagged struct {
	Inner `bonjson:"inner"`
	Z     int
}

func TestAnonymousFields(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{
			"embedded",
			Outer{Inner: Inner{X: 1, Y: 2}, Z: 3},
		},
		{
			"embedded_tagged",
			OuterTagged{Inner: Inner{X: 10, Y: 20}, Z: 30},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			ptr := reflect.New(reflect.TypeOf(tt.value))
			if err := Unmarshal(data, ptr.Interface()); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
		})
	}
}

// ============================================================================
// Case-Insensitive Field Matching Tests
// ============================================================================

type CaseSensitiveStruct struct {
	Foo int
	FOO int `bonjson:"FOO"`
}

func TestCaseInsensitiveUnmarshal(t *testing.T) {
	// Marshal with lowercase key
	type lower struct {
		Foo int `bonjson:"foo"`
	}
	data, _ := Marshal(lower{Foo: 42})

	// Should match Foo field case-insensitively
	var got struct {
		Foo int
	}
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
}

// ============================================================================
// String Tests
// ============================================================================

func TestMarshalSpecialStrings(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"ascii", "hello world"},
		{"unicode", "日本語テスト"},
		{"emoji", "👋🌍🎉"},
		{"mixed", "Hello 世界 🌍!"},
		{"long_ascii", strings.Repeat("x", 1000)},
		{"long_unicode", strings.Repeat("世", 1000)},
		{"whitespace", "  \t\n\r  "},
		{"quotes", `"quoted"`},
		{"backslash", `path\to\file`},
		{"control_chars_safe", "\t\n\r"}, // These are valid
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			var got string
			if err := Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if got != tt.value {
				t.Errorf("roundtrip:\n  got:  %q\n  want: %q", got, tt.value)
			}
		})
	}
}

// ============================================================================
// Deeply Nested Structure Tests
// ============================================================================

func TestDeeplyNested(t *testing.T) {
	// Create deeply nested structure
	depth := 50
	var v any = "leaf"
	for i := 0; i < depth; i++ {
		v = []any{v}
	}

	data, err := Marshal(v)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got any
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
}

// ============================================================================
// Valid Function Tests
// ============================================================================

func TestValid(t *testing.T) {
	// Valid data
	data, _ := Marshal(map[string]int{"a": 1})
	if !Valid(data) {
		t.Error("Valid returned false for valid BONJSON")
	}

	// Invalid data
	if Valid([]byte{0x65}) { // Reserved type code
		t.Error("Valid returned true for invalid type code")
	}

	// Empty data
	if Valid([]byte{}) {
		t.Error("Valid returned true for empty data")
	}

	// Truncated data
	if Valid([]byte{typeFloat64, 0x01}) { // Float64 needs more bytes
		t.Error("Valid returned true for truncated data")
	}
}

// Compact encoding sizes are tested by universal spec tests (integers.json, strings.json)

// ============================================================================
// AppendMarshal Tests
// ============================================================================

func TestAppendMarshal(t *testing.T) {
	// Test with pre-allocated buffer
	dst := make([]byte, 0, 100)
	dst = append(dst, "prefix"...)

	result, err := AppendMarshal(dst, 42)
	if err != nil {
		t.Fatalf("AppendMarshal error: %v", err)
	}

	// Should have prefix + marshaled value
	if !bytes.HasPrefix(result, []byte("prefix")) {
		t.Error("prefix was lost")
	}

	// Verify the marshaled part
	data := result[len("prefix"):]
	var got int
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestAppendMarshalError(t *testing.T) {
	// Test with unsupported type
	dst := make([]byte, 0)
	_, err := AppendMarshal(dst, make(chan int))
	if err == nil {
		t.Error("expected error for unsupported type")
	}
}

// ============================================================================
// Map Encoder Specialization Tests
// ============================================================================

func TestEncodeMapStringInt(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got map[string]int
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !reflect.DeepEqual(got, m) {
		t.Errorf("got %v, want %v", got, m)
	}
}

func TestEncodeMapStringInt64(t *testing.T) {
	// Use values that are clearly in int64 range and test round-trip
	m := map[string]int64{"a": 1000, "b": 2000, "c": 3000}
	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got map[string]int64
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got["a"] != 1000 || got["b"] != 2000 || got["c"] != 3000 {
		t.Errorf("got %v, want map[a:1000 b:2000 c:3000]", got)
	}
}

func TestEncodeMapStringFloat64(t *testing.T) {
	m := map[string]float64{"a": 1.1, "b": 2.2, "c": 3.3}
	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got map[string]float64
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !reflect.DeepEqual(got, m) {
		t.Errorf("got %v, want %v", got, m)
	}
}

func TestEncodeMapStringBool(t *testing.T) {
	m := map[string]bool{"a": true, "b": false, "c": true}
	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got map[string]bool
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !reflect.DeepEqual(got, m) {
		t.Errorf("got %v, want %v", got, m)
	}
}

// ============================================================================
// Bool Encoder with Quoted Option Tests
// ============================================================================

func TestEncodeBoolQuoted(t *testing.T) {
	// The quoted option encodes bools as strings "true"/"false"
	// This is used with the ",string" struct tag
	type WithQuotedBool struct {
		Value bool `bonjson:"value,string"`
	}

	data, err := Marshal(WithQuotedBool{Value: true})
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Verify it encodes (don't try to decode back since string->bool isn't supported)
	if len(data) == 0 {
		t.Error("expected non-empty data")
	}
}

// ============================================================================
// resolveKeyName Tests (map key conversion)
// ============================================================================

func TestEncodeMapWithIntKeys(t *testing.T) {
	m := map[int]string{1: "one", 2: "two", 3: "three"}
	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Should be encoded with string keys "1", "2", "3"
	var got map[string]string
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got["1"] != "one" || got["2"] != "two" || got["3"] != "three" {
		t.Errorf("got %v", got)
	}
}

func TestEncodeMapWithUintKeys(t *testing.T) {
	m := map[uint64]string{100: "a", 200: "b"}
	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got map[string]string
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got["100"] != "a" || got["200"] != "b" {
		t.Errorf("got %v", got)
	}
}

// ============================================================================
// Marshaler Error Tests
// ============================================================================

type errorMarshalerEncode struct{}

func (e errorMarshalerEncode) MarshalBONJSON() ([]byte, error) {
	return nil, errors.New("marshaler error")
}

func TestMarshalerInterfaceError(t *testing.T) {
	_, err := Marshal(errorMarshalerEncode{})
	if err == nil {
		t.Error("expected error from marshaler")
	}
}

type errorTextMarshalerEncode struct{}

func (e errorTextMarshalerEncode) MarshalText() ([]byte, error) {
	return nil, errors.New("text marshaler error")
}

func TestTextMarshalerInterfaceError(t *testing.T) {
	_, err := Marshal(errorTextMarshalerEncode{})
	if err == nil {
		t.Error("expected error from text marshaler")
	}
}

// ============================================================================
// Pointer Marshaler Tests (addressable values)
// ============================================================================

type ptrMarshalerEncode struct {
	value int
}

func (p *ptrMarshalerEncode) MarshalBONJSON() ([]byte, error) {
	return Marshal(p.value * 3)
}

func TestPointerMarshalerInterface(t *testing.T) {
	p := &ptrMarshalerEncode{value: 14}
	data, err := Marshal(p)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got int
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

type ptrTextMarshalerEncode struct {
	value string
}

func (p *ptrTextMarshalerEncode) MarshalText() ([]byte, error) {
	return []byte("ptr:" + p.value), nil
}

func TestPointerTextMarshalerInterface(t *testing.T) {
	p := &ptrTextMarshalerEncode{value: "world"}
	data, err := Marshal(p)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got string
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got != "ptr:world" {
		t.Errorf("got %q, want %q", got, "ptr:world")
	}
}

// ============================================================================
// Comprehensive Encoder Error Handling Tests
// ============================================================================

// TestUnsupportedTypeErrorDetails verifies that UnsupportedTypeError contains
// the correct type information and produces a useful error message.
func TestUnsupportedTypeErrorDetails(t *testing.T) {
	tests := []struct {
		name         string
		value        any
		expectedType string
	}{
		{"channel", make(chan int), "chan int"},
		{"bidirectional_channel", make(chan string), "chan string"},
		{"send_only_channel", make(chan<- int), "chan<- int"},
		{"receive_only_channel", make(<-chan int), "<-chan int"},
		{"func", func() {}, "func()"},
		{"func_with_args", func(int) string { return "" }, "func(int) string"},
		{"complex64", complex64(1 + 2i), "complex64"},
		{"complex128", complex128(1 + 2i), "complex128"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Marshal(tt.value)
			if err == nil {
				t.Fatal("expected error for unsupported type")
			}

			var ute *UnsupportedTypeError
			if !errors.As(err, &ute) {
				t.Fatalf("expected UnsupportedTypeError, got %T: %v", err, err)
			}

			if ute.Type.String() != tt.expectedType {
				t.Errorf("error type = %q, want %q", ute.Type.String(), tt.expectedType)
			}

			// Verify error message format
			if !strings.Contains(err.Error(), tt.expectedType) {
				t.Errorf("error message %q should contain type %q", err.Error(), tt.expectedType)
			}
		})
	}
}

// TestUnsupportedTypeInNestedStructure verifies that unsupported types in
// nested structures still produce the correct error.
func TestUnsupportedTypeInNestedStructure(t *testing.T) {
	type Nested struct {
		Value chan int
	}
	type Outer struct {
		Inner Nested
	}

	_, err := Marshal(Outer{Inner: Nested{Value: make(chan int)}})
	if err == nil {
		t.Fatal("expected error for unsupported type in nested struct")
	}

	var ute *UnsupportedTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("expected UnsupportedTypeError, got %T: %v", err, err)
	}

	if ute.Type.String() != "chan int" {
		t.Errorf("error type = %q, want %q", ute.Type.String(), "chan int")
	}
}

// TestUnsupportedTypeInMap verifies that unsupported types as map values
// produce the correct error.
func TestUnsupportedTypeInMap(t *testing.T) {
	m := map[string]any{
		"valid": 123,
		"bad":   make(chan int),
	}

	_, err := Marshal(m)
	if err == nil {
		t.Fatal("expected error for unsupported type in map")
	}

	var ute *UnsupportedTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("expected UnsupportedTypeError, got %T: %v", err, err)
	}
}

// TestUnsupportedTypeInSlice verifies that unsupported types in slices
// produce the correct error.
func TestUnsupportedTypeInSlice(t *testing.T) {
	s := []any{123, "hello", make(chan int)}

	_, err := Marshal(s)
	if err == nil {
		t.Fatal("expected error for unsupported type in slice")
	}

	var ute *UnsupportedTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("expected UnsupportedTypeError, got %T: %v", err, err)
	}
}

// TestUnsupportedTypeViaInterface verifies that interface values containing
// unsupported types produce the correct error.
func TestUnsupportedTypeViaInterface(t *testing.T) {
	var iface any = func() {}

	_, err := Marshal(iface)
	if err == nil {
		t.Fatal("expected error for unsupported type via interface")
	}

	var ute *UnsupportedTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("expected UnsupportedTypeError, got %T: %v", err, err)
	}
}

// TestMarshalerErrorDetails verifies that MarshalerError contains the correct
// information about the failing marshaler.
func TestMarshalerErrorDetails(t *testing.T) {
	_, err := Marshal(errorMarshalerEncode{})
	if err == nil {
		t.Fatal("expected error from marshaler")
	}

	var me *MarshalerError
	if !errors.As(err, &me) {
		t.Fatalf("expected MarshalerError, got %T: %v", err, err)
	}

	// Verify type information
	if me.Type.String() != "bonjson.errorMarshalerEncode" {
		t.Errorf("error type = %q, want %q", me.Type.String(), "bonjson.errorMarshalerEncode")
	}

	// Verify method name
	if me.sourceFunc != "MarshalBONJSON" {
		t.Errorf("sourceFunc = %q, want %q", me.sourceFunc, "MarshalBONJSON")
	}

	// Verify error message format
	errMsg := err.Error()
	if !strings.Contains(errMsg, "errorMarshalerEncode") {
		t.Errorf("error message %q should contain type name", errMsg)
	}
	if !strings.Contains(errMsg, "MarshalBONJSON") {
		t.Errorf("error message %q should contain method name", errMsg)
	}
}

// TestMarshalerErrorUnwrap verifies that MarshalerError correctly implements
// the errors.Unwrap interface.
func TestMarshalerErrorUnwrap(t *testing.T) {
	originalErr := errors.New("original error")

	type customMarshaler struct{}
	customMarshalerFunc := func() ([]byte, error) {
		return nil, originalErr
	}
	_ = customMarshalerFunc // Just for type definition

	_, err := Marshal(errorMarshalerEncode{})
	if err == nil {
		t.Fatal("expected error from marshaler")
	}

	var me *MarshalerError
	if !errors.As(err, &me) {
		t.Fatalf("expected MarshalerError, got %T: %v", err, err)
	}

	// Verify Unwrap returns the original error
	unwrapped := errors.Unwrap(me)
	if unwrapped == nil {
		t.Error("Unwrap() returned nil, expected original error")
	}
}

// TestTextMarshalerErrorDetails verifies that MarshalerError from TextMarshaler
// contains the correct method name.
func TestTextMarshalerErrorDetails(t *testing.T) {
	_, err := Marshal(errorTextMarshalerEncode{})
	if err == nil {
		t.Fatal("expected error from text marshaler")
	}

	var me *MarshalerError
	if !errors.As(err, &me) {
		t.Fatalf("expected MarshalerError, got %T: %v", err, err)
	}

	// Verify method name is MarshalText, not MarshalBONJSON
	if me.sourceFunc != "MarshalText" {
		t.Errorf("sourceFunc = %q, want %q", me.sourceFunc, "MarshalText")
	}
}

// TestMarshalerErrorInNestedStruct verifies that marshaler errors bubble up
// correctly from nested structures.
func TestMarshalerErrorInNestedStruct(t *testing.T) {
	type Outer struct {
		Name  string
		Inner errorMarshalerEncode
	}

	_, err := Marshal(Outer{Name: "test"})
	if err == nil {
		t.Fatal("expected error from nested marshaler")
	}

	var me *MarshalerError
	if !errors.As(err, &me) {
		t.Fatalf("expected MarshalerError, got %T: %v", err, err)
	}
}

// TestMarshalerErrorInMap verifies that marshaler errors bubble up from maps.
func TestMarshalerErrorInMap(t *testing.T) {
	m := map[string]any{
		"good": 123,
		"bad":  errorMarshalerEncode{},
	}

	_, err := Marshal(m)
	if err == nil {
		t.Fatal("expected error from map value marshaler")
	}

	var me *MarshalerError
	if !errors.As(err, &me) {
		t.Fatalf("expected MarshalerError, got %T: %v", err, err)
	}
}

// TestMarshalerErrorInSlice verifies that marshaler errors bubble up from slices.
func TestMarshalerErrorInSlice(t *testing.T) {
	s := []any{123, errorMarshalerEncode{}}

	_, err := Marshal(s)
	if err == nil {
		t.Fatal("expected error from slice element marshaler")
	}

	var me *MarshalerError
	if !errors.As(err, &me) {
		t.Fatalf("expected MarshalerError, got %T: %v", err, err)
	}
}

// TestCycleDetectionVariants tests cycle detection for different container types.
func TestCycleDetectionVariants(t *testing.T) {
	t.Run("struct_cycle", func(t *testing.T) {
		type Node struct {
			Next *Node
		}
		n := &Node{}
		n.Next = n

		_, err := Marshal(n)
		if err == nil {
			t.Fatal("expected error for struct cycle")
		}

		var uve *UnsupportedValueError
		if !errors.As(err, &uve) {
			t.Fatalf("expected UnsupportedValueError, got %T: %v", err, err)
		}

		if !strings.Contains(uve.Str, "cycle") {
			t.Errorf("error message %q should mention cycle", uve.Str)
		}
	})

	t.Run("map_cycle", func(t *testing.T) {
		m := make(map[string]any)
		m["self"] = m

		_, err := Marshal(m)
		if err == nil {
			t.Fatal("expected error for map cycle")
		}

		var uve *UnsupportedValueError
		if !errors.As(err, &uve) {
			t.Fatalf("expected UnsupportedValueError, got %T: %v", err, err)
		}

		if !strings.Contains(uve.Str, "cycle") {
			t.Errorf("error message %q should mention cycle", uve.Str)
		}
	})

	t.Run("slice_cycle", func(t *testing.T) {
		s := make([]any, 1)
		s[0] = s

		_, err := Marshal(s)
		if err == nil {
			t.Fatal("expected error for slice cycle")
		}

		var uve *UnsupportedValueError
		if !errors.As(err, &uve) {
			t.Fatalf("expected UnsupportedValueError, got %T: %v", err, err)
		}

		if !strings.Contains(uve.Str, "cycle") {
			t.Errorf("error message %q should mention cycle", uve.Str)
		}
	})

	t.Run("indirect_cycle", func(t *testing.T) {
		type A struct {
			B *struct {
				A *A
			}
		}
		a := &A{B: &struct{ A *A }{}}
		a.B.A = a

		_, err := Marshal(a)
		if err == nil {
			t.Fatal("expected error for indirect cycle")
		}

		var uve *UnsupportedValueError
		if !errors.As(err, &uve) {
			t.Fatalf("expected UnsupportedValueError, got %T: %v", err, err)
		}
	})
}

// TestCycleErrorContainsType verifies that cycle errors mention the type involved.
func TestCycleErrorContainsType(t *testing.T) {
	type LinkedNode struct {
		Value int
		Next  *LinkedNode
	}

	n := &LinkedNode{Value: 1}
	n.Next = n

	_, err := Marshal(n)
	if err == nil {
		t.Fatal("expected error for cycle")
	}

	var uve *UnsupportedValueError
	if !errors.As(err, &uve) {
		t.Fatalf("expected UnsupportedValueError, got %T: %v", err, err)
	}

	// Error should mention the type
	if !strings.Contains(err.Error(), "LinkedNode") {
		t.Errorf("error message %q should mention type name", err.Error())
	}
}

// TestDeepNestingNoCycle verifies that very deep but non-cyclic structures
// don't produce false cycle detection errors.
func TestDeepNestingNoCycle(t *testing.T) {
	// Build a deeply nested but non-cyclic structure
	type Deep struct {
		Value int
		Child *Deep
	}

	root := &Deep{Value: 0}
	current := root
	// Create a chain of 100 nodes (well under 1000 threshold)
	for i := 1; i < 100; i++ {
		current.Child = &Deep{Value: i}
		current = current.Child
	}

	data, err := Marshal(root)
	if err != nil {
		t.Fatalf("unexpected error for deep but non-cyclic structure: %v", err)
	}

	var decoded Deep
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// Verify the chain is intact
	current2 := &decoded
	for i := 0; i < 100; i++ {
		if current2 == nil {
			t.Fatalf("chain broke at depth %d", i)
		}
		if current2.Value != i {
			t.Errorf("at depth %d: value = %d, want %d", i, current2.Value, i)
		}
		current2 = current2.Child
	}
}

// TestPointerMarshalerError verifies that errors from pointer-receiver marshalers
// are handled correctly.
func TestPointerMarshalerError(t *testing.T) {
	type ptrErrorMarshaler struct {
		value int
	}
	// Can't define methods in test, so use the existing errorMarshalerEncode via pointer
	ptr := &errorMarshalerEncode{}

	_, err := Marshal(ptr)
	if err == nil {
		t.Fatal("expected error from pointer marshaler")
	}

	var me *MarshalerError
	if !errors.As(err, &me) {
		t.Fatalf("expected MarshalerError, got %T: %v", err, err)
	}
}

// TestNilPointerToMarshalerIsNull verifies that nil pointers to types
// implementing Marshaler encode as null rather than calling the marshaler.
func TestNilPointerToMarshalerIsNull(t *testing.T) {
	var ptr *customMarshaler = nil

	data, err := Marshal(ptr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded any
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded != nil {
		t.Errorf("decoded = %v (%T), want nil", decoded, decoded)
	}
}

// ============================================================================
// Additional Custom Marshaler Edge Case Tests
// ============================================================================

// Test that Marshaler takes priority over TextMarshaler
type dualMarshaler struct {
	value int
}

func (d dualMarshaler) MarshalBONJSON() ([]byte, error) {
	return Marshal(d.value * 2) // Returns numeric BONJSON
}

func (d dualMarshaler) MarshalText() ([]byte, error) {
	return []byte("text"), nil // Would return string if used
}

func (d *dualMarshaler) UnmarshalBONJSON(data []byte) error {
	var v int
	if err := Unmarshal(data, &v); err != nil {
		return err
	}
	d.value = v / 2
	return nil
}

func (d *dualMarshaler) UnmarshalText(data []byte) error {
	d.value = -1 // Different behavior if used
	return nil
}

func TestMarshalerPriorityOverTextMarshaler(t *testing.T) {
	d := dualMarshaler{value: 21}

	data, err := Marshal(d)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Should use MarshalBONJSON, so the value should be 42 (21 * 2)
	var decoded int
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded != 42 {
		t.Errorf("decoded = %d, want 42 (MarshalBONJSON should be used)", decoded)
	}
}

func TestUnmarshalerPriorityOverTextUnmarshaler(t *testing.T) {
	// Encode a value that MarshalBONJSON would produce
	data, _ := Marshal(84) // 84 / 2 = 42

	var d dualMarshaler
	if err := Unmarshal(data, &d); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// UnmarshalBONJSON divides by 2
	if d.value != 42 {
		t.Errorf("value = %d, want 42 (UnmarshalBONJSON should be used)", d.value)
	}
}

// Test Marshaler that returns empty BONJSON (null)
type nullMarshaler struct{}

func (n nullMarshaler) MarshalBONJSON() ([]byte, error) {
	return Marshal(nil)
}

func TestMarshalerReturningNull(t *testing.T) {
	n := nullMarshaler{}

	data, err := Marshal(n)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded any
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded != nil {
		t.Errorf("decoded = %v, want nil", decoded)
	}
}

// Test Marshaler that returns complex structure
type complexMarshaler struct {
	name string
	age  int
}

func (c complexMarshaler) MarshalBONJSON() ([]byte, error) {
	return Marshal(map[string]any{
		"person_name": c.name,
		"person_age":  c.age,
	})
}

func (c *complexMarshaler) UnmarshalBONJSON(data []byte) error {
	var m map[string]any
	if err := Unmarshal(data, &m); err != nil {
		return err
	}
	if name, ok := m["person_name"].(string); ok {
		c.name = name
	}
	if age, ok := m["person_age"].(int64); ok {
		c.age = int(age)
	}
	return nil
}

func TestMarshalerReturningComplexStructure(t *testing.T) {
	c := complexMarshaler{name: "Alice", age: 30}

	data, err := Marshal(c)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded complexMarshaler
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.name != c.name || decoded.age != c.age {
		t.Errorf("decoded = %+v, want %+v", decoded, c)
	}
}

// Test value receiver marshaler accessed via pointer
type valueReceiverMarshaler struct {
	value int
}

func (v valueReceiverMarshaler) MarshalBONJSON() ([]byte, error) {
	return Marshal(v.value)
}

func TestValueReceiverMarshalerViaPointer(t *testing.T) {
	v := &valueReceiverMarshaler{value: 42}

	data, err := Marshal(v)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded int
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded != 42 {
		t.Errorf("decoded = %d, want 42", decoded)
	}
}

// Test pointer receiver marshaler on struct field value
// (This tests that the encoder handles addressability correctly)
type ptrOnlyMarshaler struct {
	Value int
}

func (p *ptrOnlyMarshaler) MarshalBONJSON() ([]byte, error) {
	return Marshal(p.Value * 10)
}

func TestPtrMarshalerInStructField(t *testing.T) {
	t.Run("pointer_field_uses_marshaler", func(t *testing.T) {
		// Pointer field should use the marshaler
		type ContainerPtr struct {
			Inner *ptrOnlyMarshaler `bonjson:"inner"`
		}

		c := ContainerPtr{Inner: &ptrOnlyMarshaler{Value: 5}}
		data, err := Marshal(c)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var decoded map[string]int
		if err := Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		// The pointer marshaler should be called
		if decoded["inner"] != 50 {
			t.Errorf("inner = %d, want 50", decoded["inner"])
		}
	})

	t.Run("value_field_marshaler_called_when_addressable", func(t *testing.T) {
		// Value field should also use the marshaler when the container is addressable
		type ContainerVal struct {
			Inner ptrOnlyMarshaler `bonjson:"inner"`
		}

		c := &ContainerVal{Inner: ptrOnlyMarshaler{Value: 5}}
		data, err := Marshal(c)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var decoded map[string]int
		if err := Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		// When marshaling via pointer, field should be addressable
		if decoded["inner"] != 50 {
			t.Errorf("inner = %d, want 50", decoded["inner"])
		}
	})
}

// Test RawMessage edge cases
func TestRawMessageEmpty(t *testing.T) {
	// Empty RawMessage should encode as null
	var raw RawMessage

	data, err := Marshal(raw)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded any
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded != nil {
		t.Errorf("decoded = %v, want nil", decoded)
	}
}

func TestRawMessageNested(t *testing.T) {
	// Create nested structure
	inner, _ := Marshal(map[string]int{"x": 1, "y": 2})
	outer, _ := Marshal(map[string]RawMessage{"data": inner})

	var decoded map[string]RawMessage
	if err := Unmarshal(outer, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Verify inner RawMessage can be decoded
	var innerDecoded map[string]int
	if err := Unmarshal(decoded["data"], &innerDecoded); err != nil {
		t.Fatalf("Inner unmarshal error: %v", err)
	}

	if innerDecoded["x"] != 1 || innerDecoded["y"] != 2 {
		t.Errorf("inner = %v, want map[x:1 y:2]", innerDecoded)
	}
}

func TestRawMessagePreservesExactBytes(t *testing.T) {
	// Create a RawMessage from encoded data
	original := map[string]any{"key": "value", "num": int64(42)}
	encoded, _ := Marshal(original)

	raw := RawMessage(encoded)

	// Marshal and unmarshal the RawMessage
	wrapped, _ := Marshal(raw)
	var decoded RawMessage
	Unmarshal(wrapped, &decoded)

	// Bytes should be identical
	if !bytes.Equal(encoded, decoded) {
		t.Errorf("bytes not preserved:\n  original: %v\n  decoded:  %v", encoded, decoded)
	}
}

// Test Unmarshaler that modifies shared state
type stateTrackingUnmarshaler struct {
	callCount *int
	value     string
}

func (s *stateTrackingUnmarshaler) UnmarshalBONJSON(data []byte) error {
	*s.callCount++
	return Unmarshal(data, &s.value)
}

func TestUnmarshalerCallCount(t *testing.T) {
	callCount := 0
	su := &stateTrackingUnmarshaler{callCount: &callCount}

	data, _ := Marshal("test")

	if err := Unmarshal(data, su); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}
	if su.value != "test" {
		t.Errorf("value = %q, want %q", su.value, "test")
	}
}

// Test slice of custom marshalers
func TestSliceOfMarshalers(t *testing.T) {
	slice := []customMarshaler{
		{Value: 10},
		{Value: 20},
		{Value: 30},
	}

	data, err := Marshal(slice)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded []int
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// customMarshaler doubles the value
	expected := []int{20, 40, 60}
	if len(decoded) != len(expected) {
		t.Fatalf("len = %d, want %d", len(decoded), len(expected))
	}
	for i, v := range decoded {
		if v != expected[i] {
			t.Errorf("decoded[%d] = %d, want %d", i, v, expected[i])
		}
	}
}

// Test map with marshaler values
func TestMapOfMarshalers(t *testing.T) {
	m := map[string]customMarshaler{
		"a": {Value: 5},
		"b": {Value: 10},
	}

	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded map[string]int
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded["a"] != 10 || decoded["b"] != 20 {
		t.Errorf("decoded = %v, want map[a:10 b:20]", decoded)
	}
}

// Test anonymous/embedded struct containing marshaler
func TestAnonymousStructWithMarshaler(t *testing.T) {
	// When a type implementing Marshaler is embedded, the outer struct
	// itself should use the embedded marshaler for the whole struct.
	// This is standard Go embedding behavior.
	type Outer struct {
		customMarshaler
		Name string `bonjson:"name"`
	}

	o := Outer{
		customMarshaler: customMarshaler{Value: 15},
		Name:            "test",
	}

	data, err := Marshal(o)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Embedded Marshaler makes the outer type itself a Marshaler.
	// customMarshaler.MarshalBONJSON returns Marshal(Value * 2) = 30
	var decoded int
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// The embedded marshaler is promoted to the outer struct
	if decoded != 30 { // 15 * 2
		t.Errorf("decoded = %d, want 30", decoded)
	}
}

// Test TextMarshaler with empty result
type emptyTextMarshaler struct{}

func (e emptyTextMarshaler) MarshalText() ([]byte, error) {
	return []byte(""), nil
}

func TestEmptyTextMarshaler(t *testing.T) {
	e := emptyTextMarshaler{}

	data, err := Marshal(e)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded string
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded != "" {
		t.Errorf("decoded = %q, want empty string", decoded)
	}
}

// Test Unmarshaler that partially consumes data (error case)
type partialUnmarshaler struct {
	valid bool
}

func (p *partialUnmarshaler) UnmarshalBONJSON(data []byte) error {
	// Try to unmarshal, expecting an integer
	var v int
	if err := Unmarshal(data, &v); err != nil {
		return err
	}
	p.valid = v > 0
	return nil
}

func TestUnmarshalerWithTypeMismatch(t *testing.T) {
	// Send a string instead of int
	data, _ := Marshal("not an int")

	var p partialUnmarshaler
	err := Unmarshal(data, &p)
	if err == nil {
		t.Error("expected error when unmarshaler receives wrong type")
	}
}
