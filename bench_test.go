//
// bench_test.go
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
	"encoding/json"
	"strings"
	"testing"
)

// ============================================================================
// Basic Type Benchmarks
// ============================================================================

func BenchmarkMarshalBool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Marshal(true)
	}
}

func BenchmarkAppendMarshalBool(b *testing.B) {
	buf := make([]byte, 0, 16)
	for i := 0; i < b.N; i++ {
		buf, _ = AppendMarshal(buf[:0], true)
	}
}

func BenchmarkUnmarshalBool(b *testing.B) {
	data, _ := Marshal(true)
	var v bool
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkMarshalInt(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Marshal(42)
	}
}

func BenchmarkUnmarshalInt(b *testing.B) {
	data, _ := Marshal(42)
	var v int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkMarshalSmallInt(b *testing.B) {
	// Small ints (-100 to 100) encode in single byte
	for i := 0; i < b.N; i++ {
		Marshal(50)
	}
}

func BenchmarkMarshalLargeInt(b *testing.B) {
	// Larger ints need multi-byte encoding
	for i := 0; i < b.N; i++ {
		Marshal(1000000)
	}
}

func BenchmarkMarshalFloat64(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Marshal(3.14159265358979)
	}
}

func BenchmarkUnmarshalFloat64(b *testing.B) {
	data, _ := Marshal(3.14159265358979)
	var v float64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

// ============================================================================
// String Benchmarks
// ============================================================================

func BenchmarkMarshalShortString(b *testing.B) {
	s := "hello" // 5 bytes, fits in short string
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkUnmarshalShortString(b *testing.B) {
	data, _ := Marshal("hello")
	var v string
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkMarshalLongString(b *testing.B) {
	s := strings.Repeat("x", 100) // 100 bytes, long string
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkUnmarshalLongString(b *testing.B) {
	data, _ := Marshal(strings.Repeat("x", 100))
	var v string
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

// Unicode string with mix of 1, 2, 3, and 4-byte UTF-8 sequences
var unicodeTestString = strings.Repeat("Hello世界🌍Ñoël", 10) // ~200 bytes with complex UTF-8

func BenchmarkMarshalUnicodeString(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Marshal(unicodeTestString)
	}
}

func BenchmarkUnmarshalUnicodeString(b *testing.B) {
	data, _ := Marshal(unicodeTestString)
	var v string
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkMarshalVeryLongString(b *testing.B) {
	s := strings.Repeat("x", 10000) // 10KB string
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

// ============================================================================
// Struct Benchmarks
// ============================================================================

type SmallStruct struct {
	X int
	Y int
	Z int
}

type MediumStruct struct {
	Name   string
	Age    int
	Email  string
	Active bool
	Score  float64
}

type LargeStruct struct {
	ID         int64
	Name       string
	Email      string
	Phone      string
	Address    string
	City       string
	Country    string
	PostalCode string
	Active     bool
	Score      float64
	Tags       []string
	Metadata   map[string]string
}

func BenchmarkMarshalSmallStruct(b *testing.B) {
	s := SmallStruct{X: 1, Y: 2, Z: 3}
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkUnmarshalSmallStruct(b *testing.B) {
	data, _ := Marshal(SmallStruct{X: 1, Y: 2, Z: 3})
	var v SmallStruct
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkMarshalMediumStruct(b *testing.B) {
	s := MediumStruct{
		Name:   "John Doe",
		Age:    30,
		Email:  "john@example.com",
		Active: true,
		Score:  95.5,
	}
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkUnmarshalMediumStruct(b *testing.B) {
	data, _ := Marshal(MediumStruct{
		Name:   "John Doe",
		Age:    30,
		Email:  "john@example.com",
		Active: true,
		Score:  95.5,
	})
	var v MediumStruct
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkMarshalLargeStruct(b *testing.B) {
	s := LargeStruct{
		ID:         12345,
		Name:       "John Doe",
		Email:      "john.doe@example.com",
		Phone:      "+1-555-123-4567",
		Address:    "123 Main Street",
		City:       "New York",
		Country:    "USA",
		PostalCode: "10001",
		Active:     true,
		Score:      98.7,
		Tags:       []string{"premium", "verified", "active"},
		Metadata:   map[string]string{"source": "web", "version": "2.0"},
	}
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkUnmarshalLargeStruct(b *testing.B) {
	data, _ := Marshal(LargeStruct{
		ID:         12345,
		Name:       "John Doe",
		Email:      "john.doe@example.com",
		Phone:      "+1-555-123-4567",
		Address:    "123 Main Street",
		City:       "New York",
		Country:    "USA",
		PostalCode: "10001",
		Active:     true,
		Score:      98.7,
		Tags:       []string{"premium", "verified", "active"},
		Metadata:   map[string]string{"source": "web", "version": "2.0"},
	})
	var v LargeStruct
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

// ============================================================================
// Slice/Array Benchmarks
// ============================================================================

func BenchmarkMarshalSmallSlice(b *testing.B) {
	s := []int{1, 2, 3, 4, 5}
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkUnmarshalSmallSlice(b *testing.B) {
	data, _ := Marshal([]int{1, 2, 3, 4, 5})
	var v []int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkMarshalLargeSlice(b *testing.B) {
	s := make([]int, 1000)
	for i := range s {
		s[i] = i
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkUnmarshalLargeSlice(b *testing.B) {
	s := make([]int, 1000)
	for i := range s {
		s[i] = i
	}
	data, _ := Marshal(s)
	var v []int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

// ============================================================================
// Map Benchmarks
// ============================================================================

func BenchmarkMarshalSmallMap(b *testing.B) {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	for i := 0; i < b.N; i++ {
		Marshal(m)
	}
}

func BenchmarkUnmarshalSmallMap(b *testing.B) {
	data, _ := Marshal(map[string]int{"a": 1, "b": 2, "c": 3})
	var v map[string]int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkMarshalLargeMap(b *testing.B) {
	m := make(map[string]int, 100)
	for i := 0; i < 100; i++ {
		m[string(rune('a'+i%26))+string(rune('0'+i/26))] = i
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Marshal(m)
	}
}

func BenchmarkUnmarshalLargeMap(b *testing.B) {
	m := make(map[string]int, 100)
	for i := 0; i < 100; i++ {
		m[string(rune('a'+i%26))+string(rune('0'+i/26))] = i
	}
	data, _ := Marshal(m)
	var v map[string]int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

// ============================================================================
// Nested Structure Benchmarks
// ============================================================================

func BenchmarkMarshalNestedStruct(b *testing.B) {
	type Inner struct {
		Value int
	}
	type Outer struct {
		Inner Inner
		Name  string
	}
	s := Outer{Inner: Inner{Value: 42}, Name: "test"}
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkMarshalDeeplyNested(b *testing.B) {
	// Create 10 levels of nesting
	var v any = 42
	for i := 0; i < 10; i++ {
		v = []any{v}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Marshal(v)
	}
}

// ============================================================================
// Stream Benchmarks
// ============================================================================

func BenchmarkEncoderEncode(b *testing.B) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	s := SmallStruct{X: 1, Y: 2, Z: 3}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		enc.Encode(s)
	}
}

func BenchmarkDecoderDecode(b *testing.B) {
	data, _ := Marshal(SmallStruct{X: 1, Y: 2, Z: 3})
	var v SmallStruct
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec := NewDecoder(bytes.NewReader(data))
		dec.Decode(&v)
	}
}

// ============================================================================
// Allocation Benchmarks
// ============================================================================

func BenchmarkMarshalAllocations(b *testing.B) {
	s := MediumStruct{
		Name:   "John Doe",
		Age:    30,
		Email:  "john@example.com",
		Active: true,
		Score:  95.5,
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkUnmarshalAllocations(b *testing.B) {
	data, _ := Marshal(MediumStruct{
		Name:   "John Doe",
		Age:    30,
		Email:  "john@example.com",
		Active: true,
		Score:  95.5,
	})
	var v MediumStruct
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

// ============================================================================
// Wire Format Benchmarks (Low-Level)
// ============================================================================

func BenchmarkEncodeLengthField(b *testing.B) {
	var buf [16]byte
	for i := 0; i < b.N; i++ {
		encodeLengthField(buf[:], 12345, false)
	}
}

func BenchmarkDecodeLengthField(b *testing.B) {
	var buf [16]byte
	n := encodeLengthField(buf[:], 12345, false)
	data := buf[:n]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeLengthField(data)
	}
}

func BenchmarkEncodeSignedInt(b *testing.B) {
	var buf [16]byte
	for i := 0; i < b.N; i++ {
		encodeSignedInt(buf[:], 12345)
	}
}

func BenchmarkEncodeSignedInt_Small(b *testing.B) {
	var buf [16]byte
	for i := 0; i < b.N; i++ {
		encodeSignedInt(buf[:], 50) // fits in single byte
	}
}

func BenchmarkEncodeSignedInt_Medium(b *testing.B) {
	var buf [16]byte
	for i := 0; i < b.N; i++ {
		encodeSignedInt(buf[:], -10000) // needs 2 bytes
	}
}

func BenchmarkEncodeSignedInt_Large(b *testing.B) {
	var buf [16]byte
	for i := 0; i < b.N; i++ {
		encodeSignedInt(buf[:], -9223372036854775808) // min int64, needs 8 bytes
	}
}

func BenchmarkEncodeUnsignedInt_Small(b *testing.B) {
	var buf [16]byte
	for i := 0; i < b.N; i++ {
		encodeUnsignedInt(buf[:], 50) // fits in single byte
	}
}

func BenchmarkEncodeUnsignedInt_Medium(b *testing.B) {
	var buf [16]byte
	for i := 0; i < b.N; i++ {
		encodeUnsignedInt(buf[:], 10000) // needs 2 bytes
	}
}

func BenchmarkEncodeUnsignedInt_Large(b *testing.B) {
	var buf [16]byte
	for i := 0; i < b.N; i++ {
		encodeUnsignedInt(buf[:], 18446744073709551615) // max uint64, needs 8 bytes
	}
}

func BenchmarkDecodeInteger(b *testing.B) {
	var buf [16]byte
	n := encodeSignedInt(buf[:], 12345)
	typeCode := buf[0]
	data := buf[1:n]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeInteger(data, typeCode)
	}
}

func BenchmarkDecodeInteger_SmallPositive(b *testing.B) {
	// Small positive integer: encoded as single byte type code
	typeCode := byte(50)
	data := []byte{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeInteger(data, typeCode)
	}
}

func BenchmarkDecodeInteger_SmallNegative(b *testing.B) {
	// Small negative integer: encoded as single byte type code
	typeCode := byte(0xCE) // -50 as unsigned byte
	data := []byte{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeInteger(data, typeCode)
	}
}

func BenchmarkDecodeInteger_Signed2Bytes(b *testing.B) {
	var buf [16]byte
	n := encodeSignedInt(buf[:], -10000)
	typeCode := buf[0]
	data := buf[1:n]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeInteger(data, typeCode)
	}
}

func BenchmarkDecodeInteger_Signed8Bytes(b *testing.B) {
	var buf [16]byte
	n := encodeSignedInt(buf[:], -9223372036854775808)
	typeCode := buf[0]
	data := buf[1:n]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeInteger(data, typeCode)
	}
}

func BenchmarkDecodeInteger_Unsigned4Bytes(b *testing.B) {
	var buf [16]byte
	n := encodeUnsignedInt(buf[:], 1000000000)
	typeCode := buf[0]
	data := buf[1:n]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeInteger(data, typeCode)
	}
}

func BenchmarkDecodeInteger_Unsigned8Bytes(b *testing.B) {
	var buf [16]byte
	n := encodeUnsignedInt(buf[:], 18446744073709551615)
	typeCode := buf[0]
	data := buf[1:n]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeInteger(data, typeCode)
	}
}

// ============================================================================
// Comparison Benchmarks (with/without various features)
// ============================================================================

func BenchmarkMarshalWithOmitempty(b *testing.B) {
	type WithOmitempty struct {
		A int    `bonjson:"a,omitempty"`
		B string `bonjson:"b,omitempty"`
		C bool   `bonjson:"c,omitempty"`
	}
	s := WithOmitempty{A: 1} // B and C are zero, should be omitted
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkMarshalWithoutOmitempty(b *testing.B) {
	type WithoutOmitempty struct {
		A int    `bonjson:"a"`
		B string `bonjson:"b"`
		C bool   `bonjson:"c"`
	}
	s := WithoutOmitempty{A: 1}
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

// ============================================================================
// Valid Function Benchmark
// ============================================================================

func BenchmarkValid(b *testing.B) {
	data, _ := Marshal(map[string]any{
		"name":  "test",
		"value": 42,
		"tags":  []string{"a", "b", "c"},
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Valid(data)
	}
}

// ============================================================================
// Byte Slice Benchmarks
// ============================================================================

func BenchmarkMarshalByteSlice(b *testing.B) {
	data := bytes.Repeat([]byte{0xAB}, 1000)
	for i := 0; i < b.N; i++ {
		Marshal(data)
	}
}

func BenchmarkUnmarshalByteSlice(b *testing.B) {
	encoded, _ := Marshal(bytes.Repeat([]byte{0xAB}, 1000))
	var v []byte
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(encoded, &v)
	}
}

// ============================================================================
// Low-Level Wire Format Benchmarks
// ============================================================================

func BenchmarkEncodeLengthPayload_Small(b *testing.B) {
	dst := make([]byte, 16)
	for i := 0; i < b.N; i++ {
		encodeLengthPayload(dst, 100)
	}
}

func BenchmarkEncodeLengthPayload_Medium(b *testing.B) {
	dst := make([]byte, 16)
	for i := 0; i < b.N; i++ {
		encodeLengthPayload(dst, 10000)
	}
}

func BenchmarkEncodeLengthPayload_Large(b *testing.B) {
	dst := make([]byte, 16)
	for i := 0; i < b.N; i++ {
		encodeLengthPayload(dst, 1000000000)
	}
}

func BenchmarkEncodeLengthField_Small(b *testing.B) {
	dst := make([]byte, 16)
	for i := 0; i < b.N; i++ {
		encodeLengthField(dst, 50, false)
	}
}

func BenchmarkEncodeLengthField_Medium(b *testing.B) {
	dst := make([]byte, 16)
	for i := 0; i < b.N; i++ {
		encodeLengthField(dst, 5000, false)
	}
}

func BenchmarkEncodeLengthField_Large(b *testing.B) {
	dst := make([]byte, 16)
	for i := 0; i < b.N; i++ {
		encodeLengthField(dst, 500000000, false)
	}
}

func BenchmarkDecodeLengthPayload_Small(b *testing.B) {
	dst := make([]byte, 16)
	encodeLengthPayload(dst, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeLengthPayload(dst)
	}
}

func BenchmarkDecodeLengthPayload_Medium(b *testing.B) {
	dst := make([]byte, 16)
	encodeLengthPayload(dst, 10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeLengthPayload(dst)
	}
}

func BenchmarkDecodeLengthPayload_Large(b *testing.B) {
	dst := make([]byte, 16)
	encodeLengthPayload(dst, 1000000000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeLengthPayload(dst)
	}
}

// ============================================================================
// JSON Comparison Benchmarks
// These benchmarks compare BONJSON performance against encoding/json
// ============================================================================

func BenchmarkComparison_MarshalInt_BONJSON(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Marshal(42)
	}
}

func BenchmarkComparison_MarshalInt_JSON(b *testing.B) {
	for i := 0; i < b.N; i++ {
		json.Marshal(42)
	}
}

func BenchmarkComparison_UnmarshalInt_BONJSON(b *testing.B) {
	data, _ := Marshal(42)
	var v int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkComparison_UnmarshalInt_JSON(b *testing.B) {
	data, _ := json.Marshal(42)
	var v int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Unmarshal(data, &v)
	}
}

func BenchmarkComparison_MarshalFloat_BONJSON(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Marshal(3.14159265358979)
	}
}

func BenchmarkComparison_MarshalFloat_JSON(b *testing.B) {
	for i := 0; i < b.N; i++ {
		json.Marshal(3.14159265358979)
	}
}

func BenchmarkComparison_UnmarshalFloat_BONJSON(b *testing.B) {
	data, _ := Marshal(3.14159265358979)
	var v float64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkComparison_UnmarshalFloat_JSON(b *testing.B) {
	data, _ := json.Marshal(3.14159265358979)
	var v float64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Unmarshal(data, &v)
	}
}

func BenchmarkComparison_MarshalString_BONJSON(b *testing.B) {
	s := "hello world"
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkComparison_MarshalString_JSON(b *testing.B) {
	s := "hello world"
	for i := 0; i < b.N; i++ {
		json.Marshal(s)
	}
}

func BenchmarkComparison_UnmarshalString_BONJSON(b *testing.B) {
	data, _ := Marshal("hello world")
	var v string
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkComparison_UnmarshalString_JSON(b *testing.B) {
	data, _ := json.Marshal("hello world")
	var v string
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Unmarshal(data, &v)
	}
}

func BenchmarkComparison_MarshalLongString_BONJSON(b *testing.B) {
	s := strings.Repeat("x", 1000)
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkComparison_MarshalLongString_JSON(b *testing.B) {
	s := strings.Repeat("x", 1000)
	for i := 0; i < b.N; i++ {
		json.Marshal(s)
	}
}

func BenchmarkComparison_UnmarshalLongString_BONJSON(b *testing.B) {
	s := strings.Repeat("x", 1000)
	data, _ := Marshal(s)
	var v string
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkComparison_UnmarshalLongString_JSON(b *testing.B) {
	s := strings.Repeat("x", 1000)
	data, _ := json.Marshal(s)
	var v string
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Unmarshal(data, &v)
	}
}

func BenchmarkComparison_MarshalUnicodeString_BONJSON(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Marshal(unicodeTestString)
	}
}

func BenchmarkComparison_MarshalUnicodeString_JSON(b *testing.B) {
	for i := 0; i < b.N; i++ {
		json.Marshal(unicodeTestString)
	}
}

func BenchmarkComparison_UnmarshalUnicodeString_BONJSON(b *testing.B) {
	data, _ := Marshal(unicodeTestString)
	var v string
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkComparison_UnmarshalUnicodeString_JSON(b *testing.B) {
	data, _ := json.Marshal(unicodeTestString)
	var v string
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Unmarshal(data, &v)
	}
}

func BenchmarkComparison_MarshalStruct_BONJSON(b *testing.B) {
	s := MediumStruct{
		Name:   "John Doe",
		Age:    30,
		Email:  "john@example.com",
		Active: true,
		Score:  95.5,
	}
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkComparison_MarshalStruct_JSON(b *testing.B) {
	s := MediumStruct{
		Name:   "John Doe",
		Age:    30,
		Email:  "john@example.com",
		Active: true,
		Score:  95.5,
	}
	for i := 0; i < b.N; i++ {
		json.Marshal(s)
	}
}

func BenchmarkComparison_UnmarshalStruct_BONJSON(b *testing.B) {
	data, _ := Marshal(MediumStruct{
		Name:   "John Doe",
		Age:    30,
		Email:  "john@example.com",
		Active: true,
		Score:  95.5,
	})
	var v MediumStruct
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkComparison_UnmarshalStruct_JSON(b *testing.B) {
	data, _ := json.Marshal(MediumStruct{
		Name:   "John Doe",
		Age:    30,
		Email:  "john@example.com",
		Active: true,
		Score:  95.5,
	})
	var v MediumStruct
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Unmarshal(data, &v)
	}
}

func BenchmarkComparison_MarshalSlice_BONJSON(b *testing.B) {
	s := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkComparison_MarshalSlice_JSON(b *testing.B) {
	s := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for i := 0; i < b.N; i++ {
		json.Marshal(s)
	}
}

func BenchmarkComparison_UnmarshalSlice_BONJSON(b *testing.B) {
	data, _ := Marshal([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	var v []int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkComparison_UnmarshalSlice_JSON(b *testing.B) {
	data, _ := json.Marshal([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	var v []int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Unmarshal(data, &v)
	}
}

func BenchmarkComparison_MarshalMap_BONJSON(b *testing.B) {
	m := map[string]any{
		"name":    "test",
		"value":   42,
		"enabled": true,
		"score":   3.14,
		"tags":    []string{"a", "b", "c"},
	}
	for i := 0; i < b.N; i++ {
		Marshal(m)
	}
}

func BenchmarkComparison_MarshalMap_JSON(b *testing.B) {
	m := map[string]any{
		"name":    "test",
		"value":   42,
		"enabled": true,
		"score":   3.14,
		"tags":    []string{"a", "b", "c"},
	}
	for i := 0; i < b.N; i++ {
		json.Marshal(m)
	}
}

func BenchmarkComparison_UnmarshalMap_BONJSON(b *testing.B) {
	data, _ := Marshal(map[string]any{
		"name":    "test",
		"value":   42,
		"enabled": true,
		"score":   3.14,
		"tags":    []string{"a", "b", "c"},
	})
	var v map[string]any
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkComparison_UnmarshalMap_JSON(b *testing.B) {
	data, _ := json.Marshal(map[string]any{
		"name":    "test",
		"value":   42,
		"enabled": true,
		"score":   3.14,
		"tags":    []string{"a", "b", "c"},
	})
	var v map[string]any
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Unmarshal(data, &v)
	}
}

func BenchmarkComparison_MarshalLargeStruct_BONJSON(b *testing.B) {
	s := LargeStruct{
		ID:         12345,
		Name:       "John Doe",
		Email:      "john.doe@example.com",
		Phone:      "+1-555-123-4567",
		Address:    "123 Main Street",
		City:       "New York",
		Country:    "USA",
		PostalCode: "10001",
		Active:     true,
		Score:      98.7,
		Tags:       []string{"premium", "verified", "active"},
		Metadata:   map[string]string{"source": "web", "version": "2.0"},
	}
	for i := 0; i < b.N; i++ {
		Marshal(s)
	}
}

func BenchmarkComparison_MarshalLargeStruct_JSON(b *testing.B) {
	s := LargeStruct{
		ID:         12345,
		Name:       "John Doe",
		Email:      "john.doe@example.com",
		Phone:      "+1-555-123-4567",
		Address:    "123 Main Street",
		City:       "New York",
		Country:    "USA",
		PostalCode: "10001",
		Active:     true,
		Score:      98.7,
		Tags:       []string{"premium", "verified", "active"},
		Metadata:   map[string]string{"source": "web", "version": "2.0"},
	}
	for i := 0; i < b.N; i++ {
		json.Marshal(s)
	}
}

func BenchmarkComparison_UnmarshalLargeStruct_BONJSON(b *testing.B) {
	data, _ := Marshal(LargeStruct{
		ID:         12345,
		Name:       "John Doe",
		Email:      "john.doe@example.com",
		Phone:      "+1-555-123-4567",
		Address:    "123 Main Street",
		City:       "New York",
		Country:    "USA",
		PostalCode: "10001",
		Active:     true,
		Score:      98.7,
		Tags:       []string{"premium", "verified", "active"},
		Metadata:   map[string]string{"source": "web", "version": "2.0"},
	})
	var v LargeStruct
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Unmarshal(data, &v)
	}
}

func BenchmarkComparison_UnmarshalLargeStruct_JSON(b *testing.B) {
	data, _ := json.Marshal(LargeStruct{
		ID:         12345,
		Name:       "John Doe",
		Email:      "john.doe@example.com",
		Phone:      "+1-555-123-4567",
		Address:    "123 Main Street",
		City:       "New York",
		Country:    "USA",
		PostalCode: "10001",
		Active:     true,
		Score:      98.7,
		Tags:       []string{"premium", "verified", "active"},
		Metadata:   map[string]string{"source": "web", "version": "2.0"},
	})
	var v LargeStruct
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Unmarshal(data, &v)
	}
}

// ============================================================================
// Data Size Comparison
// Shows the encoded size difference between BONJSON and JSON
// ============================================================================

func BenchmarkComparison_EncodedSize(b *testing.B) {
	testCases := []struct {
		name string
		data any
	}{
		{"int_small", 42},
		{"int_large", 1000000},
		{"float", 3.14159265358979},
		{"string_short", "hello"},
		{"string_long", strings.Repeat("x", 100)},
		{"bool", true},
		{"slice_int", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{"struct_medium", MediumStruct{Name: "John Doe", Age: 30, Email: "john@example.com", Active: true, Score: 95.5}},
		{"map_mixed", map[string]any{"name": "test", "value": 42, "enabled": true}},
	}

	for _, tc := range testCases {
		bonjsonData, _ := Marshal(tc.data)
		jsonData, _ := json.Marshal(tc.data)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportMetric(float64(len(bonjsonData)), "bonjson_bytes")
			b.ReportMetric(float64(len(jsonData)), "json_bytes")
			b.ReportMetric(float64(len(bonjsonData))/float64(len(jsonData))*100, "ratio_%")
		})
	}
}
