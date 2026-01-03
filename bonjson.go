// Copyright 2024 Karl Stenerud. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package bonjson implements encoding and decoding of BONJSON (Binary Object
// Notation for JSON), a binary format that is 1:1 compatible with JSON.
//
// BONJSON is designed to be a drop-in replacement for JSON with the following
// advantages:
//   - Much faster to parse (no escape sequence handling, no number parsing)
//   - More compact representation
//   - Safer against common JSON attack vectors
//
// # API Compatibility with encoding/json
//
// This package provides a compatible API with encoding/json for easy migration.
// The following functions and types work identically:
//
//   - Marshal, Unmarshal, Valid
//   - NewEncoder, NewDecoder
//   - Encoder.Encode, Decoder.Decode
//   - Decoder.DisallowUnknownFields
//   - Decoder.Buffered, Decoder.More, Decoder.Token, Decoder.InputOffset
//   - RawMessage, Delim, Token
//   - All error types (SyntaxError, UnmarshalTypeError, etc.)
//
// The following encoding/json functions are NOT provided because they are
// specific to text-based JSON formatting and not applicable to binary BONJSON:
//
//   - MarshalIndent (BONJSON is binary, no indentation)
//   - Compact (BONJSON is always compact)
//   - Indent (BONJSON is binary, no indentation)
//   - HTMLEscape (BONJSON is binary, no HTML escaping needed)
//   - Encoder.SetEscapeHTML (BONJSON is binary, no HTML escaping needed)
//   - Encoder.SetIndent (BONJSON is binary, no indentation)
//
// Note: Decoder.UseNumber() is NOT provided. Unlike JSON where all numbers are
// text (and float64 loses precision for large integers), BONJSON stores numbers
// in their native binary format. When decoding to interface{}, signed integers
// become int64, unsigned integers become uint64, and floats become float64.
// This preserves full precision without needing UseNumber().
//
// # Security Considerations
//
// Unlike JSON, BONJSON enforces strict security rules by default:
//
//   - Duplicate object keys are rejected
//   - Invalid UTF-8 in strings is rejected (not replaced)
//   - NUL characters in strings are rejected by default
//   - Chunked strings are rejected by default
//   - NaN and Infinity values are rejected
//
// These behaviors can be configured using Decoder options, but the secure
// defaults should be used unless there's a specific reason to allow them.
//
// # Basic Usage
//
// The simplest way to use this package is through Marshal and Unmarshal:
//
//	type Person struct {
//	    Name string `json:"name"`
//	    Age  int    `json:"age"`
//	}
//
//	// Encoding
//	p := Person{Name: "Alice", Age: 30}
//	data, err := bonjson.Marshal(p)
//
//	// Decoding
//	var p2 Person
//	err = bonjson.Unmarshal(data, &p2)
//
// # Streaming
//
// For streaming scenarios, use Encoder and Decoder:
//
//	// Encoding to a writer
//	enc := bonjson.NewEncoder(w)
//	err := enc.Encode(value)
//
//	// Decoding from a reader
//	dec := bonjson.NewDecoder(r)
//	err := dec.Decode(&value)
//
// # Struct Tags
//
// This package supports the same struct tags as encoding/json:
//
//	type Example struct {
//	    Field1 string `json:"field1"`           // rename to "field1"
//	    Field2 string `json:"field2,omitempty"` // omit if empty
//	    Field3 string `json:"-"`                // always omit
//	    Field4 int    `json:",string"`          // encode as string
//	}
//
// Additionally, the "bonjson" tag is checked first, allowing different
// behavior for JSON and BONJSON encoding of the same struct.
//
// # Type Mapping
//
// BONJSON has the same types as JSON and uses the same Go type mappings:
//
//   - BONJSON null -> nil
//   - BONJSON bool -> bool
//   - BONJSON signed integer -> int64
//   - BONJSON unsigned integer -> uint64
//   - BONJSON float -> float64
//   - BONJSON string -> string
//   - BONJSON array -> []any
//   - BONJSON object -> map[string]any
//
// Unlike JSON (where all numbers are text and typically become float64),
// BONJSON preserves numeric types in their native binary format.
//
// # Custom Types
//
// Types can implement Marshaler and Unmarshaler for custom encoding:
//
//	type Marshaler interface {
//	    MarshalBONJSON() ([]byte, error)
//	}
//
//	type Unmarshaler interface {
//	    UnmarshalBONJSON([]byte) error
//	}
//
// If these interfaces are not implemented, the package falls back to
// encoding.TextMarshaler and encoding.TextUnmarshaler.
package bonjson

import "reflect"

// Valid reports whether data is a valid BONJSON encoding.
func Valid(data []byte) bool {
	d := newDecodeState()
	defer decodeStatePool.Put(d)

	d.init(data)
	var v any
	rv := reflect.ValueOf(&v).Elem()
	err := d.value(rv)
	if err != nil {
		return false
	}
	return d.off == len(data)
}
