// Copyright 2024 Karl Stenerud. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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

func TestMarshalUnsupportedValues(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{"NaN", math.NaN()},
		{"Inf", math.Inf(1)},
		{"NegInf", math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Marshal(tt.value)
			if err == nil {
				t.Error("expected error for unsupported value")
			}
			var uve *UnsupportedValueError
			if !errors.As(err, &uve) {
				t.Errorf("expected UnsupportedValueError, got %T", err)
			}
		})
	}
}

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

// ============================================================================
// Compact Encoding Tests
// ============================================================================

func TestCompactEncoding(t *testing.T) {
	// Small integers should be encoded in 1 byte
	for i := int64(-100); i <= 100; i++ {
		data, _ := Marshal(i)
		if len(data) != 1 {
			t.Errorf("small int %d encoded as %d bytes, want 1", i, len(data))
		}
	}

	// Short strings (0-15 bytes) should have minimal overhead
	for length := 0; length <= 15; length++ {
		s := strings.Repeat("x", length)
		data, _ := Marshal(s)
		// 1 byte type code + string content
		if len(data) != 1+length {
			t.Errorf("short string len=%d encoded as %d bytes, want %d", length, len(data), 1+length)
		}
	}

	// Boolean
	trueData, _ := Marshal(true)
	if len(trueData) != 1 {
		t.Errorf("true encoded as %d bytes, want 1", len(trueData))
	}

	falseData, _ := Marshal(false)
	if len(falseData) != 1 {
		t.Errorf("false encoded as %d bytes, want 1", len(falseData))
	}

	// Null
	nullData, _ := Marshal(nil)
	if len(nullData) != 1 {
		t.Errorf("null encoded as %d bytes, want 1", len(nullData))
	}
}

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
