//
// errors.go
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
	"fmt"
	"reflect"
)

// A SyntaxError is a description of a BONJSON syntax error.
type SyntaxError struct {
	msg    string // description of error
	Offset int64  // error occurred after reading Offset bytes
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("bonjson: %s at offset %d", e.msg, e.Offset)
}

// An UnmarshalTypeError describes a BONJSON value that was
// not appropriate for a value of a specific Go type.
type UnmarshalTypeError struct {
	Value  string       // description of BONJSON value - "bool", "array", "number -5"
	Type   reflect.Type // type of Go value it could not be assigned to
	Offset int64        // error occurred after reading Offset bytes
	Struct string       // name of the struct type containing the field
	Field  string       // the full path from root node to the field
}

func (e *UnmarshalTypeError) Error() string {
	if e.Struct != "" || e.Field != "" {
		return "bonjson: cannot unmarshal " + e.Value + " into Go struct field " + e.Struct + "." + e.Field + " of type " + e.Type.String()
	}
	return "bonjson: cannot unmarshal " + e.Value + " into Go value of type " + e.Type.String()
}

// An InvalidUnmarshalError describes an invalid argument passed to Unmarshal.
// (The argument to Unmarshal must be a non-nil pointer.)
type InvalidUnmarshalError struct {
	Type reflect.Type
}

func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil {
		return "bonjson: Unmarshal(nil)"
	}
	if e.Type.Kind() != reflect.Pointer {
		return "bonjson: Unmarshal(non-pointer " + e.Type.String() + ")"
	}
	return "bonjson: Unmarshal(nil " + e.Type.String() + ")"
}

// An UnsupportedTypeError is returned by Marshal when attempting
// to encode an unsupported value type.
type UnsupportedTypeError struct {
	Type reflect.Type
}

func (e *UnsupportedTypeError) Error() string {
	return "bonjson: unsupported type: " + e.Type.String()
}

// An UnsupportedValueError is returned by Marshal when attempting
// to encode an unsupported value.
type UnsupportedValueError struct {
	Value reflect.Value
	Str   string
}

func (e *UnsupportedValueError) Error() string {
	return "bonjson: unsupported value: " + e.Str
}

// A MarshalerError represents an error from calling a
// Marshaler.MarshalBONJSON or encoding.TextMarshaler.MarshalText method.
type MarshalerError struct {
	Type       reflect.Type
	Err        error
	sourceFunc string
}

func (e *MarshalerError) Error() string {
	srcFunc := e.sourceFunc
	if srcFunc == "" {
		srcFunc = "MarshalBONJSON"
	}
	return "bonjson: error calling " + srcFunc +
		" for type " + e.Type.String() +
		": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *MarshalerError) Unwrap() error { return e.Err }

// DuplicateKeyError is returned when an object contains duplicate keys.
type DuplicateKeyError struct {
	Key    string
	Offset int64
}

func (e *DuplicateKeyError) Error() string {
	return fmt.Sprintf("bonjson: duplicate key %q at offset %d", e.Key, e.Offset)
}

// InvalidUTF8Error is returned when a string contains invalid UTF-8.
type InvalidUTF8Error struct {
	Offset int64
}

func (e *InvalidUTF8Error) Error() string {
	return fmt.Sprintf("bonjson: invalid UTF-8 at offset %d", e.Offset)
}

// NullInStringError is returned when a string contains a NUL character.
type NullInStringError struct {
	Offset int64
}

func (e *NullInStringError) Error() string {
	return fmt.Sprintf("bonjson: NUL character in string at offset %d", e.Offset)
}

// TooManyChunksError is returned when a string has more chunks than allowed.
type TooManyChunksError struct {
	Count  int
	Max    int
	Offset int64
}

func (e *TooManyChunksError) Error() string {
	return fmt.Sprintf("bonjson: string has %d chunks, max allowed is %d at offset %d", e.Count, e.Max, e.Offset)
}

// EmptyChunkContinuationError is returned when a zero-length chunk has the
// continuation bit set. This is invalid because empty chunks with continuation
// serve no purpose and could be used for DoS attacks.
type EmptyChunkContinuationError struct {
	Offset int64
}

func (e *EmptyChunkContinuationError) Error() string {
	return fmt.Sprintf("bonjson: empty chunk with continuation bit set at offset %d", e.Offset)
}

// ValueRangeError is returned when a numeric value is out of range.
type ValueRangeError struct {
	Value  string
	Offset int64
}

func (e *ValueRangeError) Error() string {
	return fmt.Sprintf("bonjson: value %s out of range at offset %d", e.Value, e.Offset)
}

// MaxDepthError is returned when container nesting exceeds the maximum depth.
type MaxDepthError struct {
	Depth  int
	Offset int64
}

func (e *MaxDepthError) Error() string {
	return fmt.Sprintf("bonjson: maximum depth %d exceeded at offset %d", e.Depth, e.Offset)
}

// MaxContainerSizeError is returned when a container has too many elements.
type MaxContainerSizeError struct {
	Size   int
	Max    int
	Offset int64
}

func (e *MaxContainerSizeError) Error() string {
	return fmt.Sprintf("bonjson: container size %d exceeds maximum %d at offset %d", e.Size, e.Max, e.Offset)
}

// MaxDocumentSizeError is returned when the document exceeds the size limit.
type MaxDocumentSizeError struct {
	Size   int64
	Max    int64
	Offset int64
}

func (e *MaxDocumentSizeError) Error() string {
	return fmt.Sprintf("bonjson: document size %d exceeds maximum %d at offset %d", e.Size, e.Max, e.Offset)
}

// InvalidTypeCodeError is returned when an invalid type code is encountered.
type InvalidTypeCodeError struct {
	TypeCode byte
	Offset   int64
}

func (e *InvalidTypeCodeError) Error() string {
	return fmt.Sprintf("bonjson: invalid type code 0x%02x at offset %d", e.TypeCode, e.Offset)
}

// TruncatedDataError is returned when the data is truncated.
type TruncatedDataError struct {
	Expected int
	Got      int
	Offset   int64
}

func (e *TruncatedDataError) Error() string {
	return fmt.Sprintf("bonjson: truncated data at offset %d: expected %d bytes, got %d", e.Offset, e.Expected, e.Got)
}

// InvalidValueError is returned when an invalid value is encountered (e.g., NaN, Infinity).
type InvalidValueError struct {
	Value  string
	Offset int64
}

func (e *InvalidValueError) Error() string {
	return fmt.Sprintf("bonjson: invalid value %s at offset %d", e.Value, e.Offset)
}

// TrailingDataError is returned when there is unexpected data after a complete value.
type TrailingDataError struct {
	Offset int64
}

func (e *TrailingDataError) Error() string {
	return fmt.Sprintf("bonjson: trailing data after value at offset %d", e.Offset)
}

// NonCanonicalLengthError is returned when a length field uses more bytes than necessary.
type NonCanonicalLengthError struct {
	Offset int64
}

func (e *NonCanonicalLengthError) Error() string {
	return fmt.Sprintf("bonjson: non-canonical length encoding at offset %d", e.Offset)
}

// UnclosedContainerError is returned when a container (array or object) is not properly closed.
type UnclosedContainerError struct {
	ContainerType string // "array" or "object"
	Offset        int64
}

func (e *UnclosedContainerError) Error() string {
	return fmt.Sprintf("bonjson: unclosed %s starting at offset %d", e.ContainerType, e.Offset)
}

// MaxStringLengthError is returned when a string exceeds the maximum allowed length.
type MaxStringLengthError struct {
	Length int64
	Max    int64
	Offset int64
}

func (e *MaxStringLengthError) Error() string {
	return fmt.Sprintf("bonjson: string length %d exceeds maximum %d at offset %d", e.Length, e.Max, e.Offset)
}
