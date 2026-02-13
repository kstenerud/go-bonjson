//
// encode.go
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

// ABOUTME: BONJSON encoding (Go values to binary). Provides Marshal(),
// ABOUTME: AppendMarshal(), and the Marshaler interface. Uses
// ABOUTME: panic/recover for error propagation and sync.Pool for
// ABOUTME: buffer reuse.

package bonjson

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Marshal returns the BONJSON encoding of v.
//
// Marshal traverses the value v recursively.
// If an encountered value implements Marshaler
// and is not a nil pointer, Marshal calls Marshaler.MarshalBONJSON
// to produce BONJSON. If no MarshalBONJSON method is present but the
// value implements encoding.TextMarshaler instead, Marshal calls
// encoding.TextMarshaler.MarshalText and encodes the result as a BONJSON string.
//
// Otherwise, Marshal uses the following type-dependent default encodings:
//
// Boolean values encode as BONJSON booleans.
//
// Floating point and integer values encode as BONJSON numbers.
// NaN and +/-Inf values will return an UnsupportedValueError.
//
// String values encode as BONJSON strings. Invalid UTF-8 sequences will
// return an error (unlike JSON which replaces them with U+FFFD).
//
// Array and slice values encode as BONJSON arrays, except that
// []byte encodes as a base64-encoded string, and a nil slice
// encodes as the null BONJSON value.
//
// Struct values encode as BONJSON objects. Each exported struct field
// becomes a member of the object.
//
// Map values encode as BONJSON objects. The map's key type must either be a
// string, an integer type, or implement encoding.TextMarshaler.
//
// Pointer values encode as the value pointed to.
// A nil pointer encodes as the null BONJSON value.
//
// Interface values encode as the value contained in the interface.
// A nil interface value encodes as the null BONJSON value.
//
// Channel, complex, and function values cannot be encoded in BONJSON.
func Marshal(v any) ([]byte, error) {
	e := newEncodeState()
	defer encodeStatePool.Put(e)

	err := e.marshal(v, encOpts{})
	if err != nil {
		return nil, err
	}
	buf := append([]byte(nil), e.Bytes()...)

	return buf, nil
}

// AppendMarshal appends the BONJSON encoding of v to dst and returns the extended buffer.
// This is more efficient than Marshal when the caller can provide a pre-allocated buffer,
// as it avoids an allocation for the result slice.
//
// See Marshal for details on the encoding of Go values.
func AppendMarshal(dst []byte, v any) ([]byte, error) {
	e := newEncodeState()
	defer encodeStatePool.Put(e)

	err := e.marshal(v, encOpts{})
	if err != nil {
		return dst, err
	}
	return append(dst, e.Bytes()...), nil
}

// Marshaler is the interface implemented by types that
// can marshal themselves into valid BONJSON.
type Marshaler interface {
	MarshalBONJSON() ([]byte, error)
}

// An encodeState encodes BONJSON into a bytes.Buffer.
type encodeState struct {
	bytes.Buffer // accumulated output

	// Scratch buffer for encoding values without allocation
	scratch [64]byte

	// Keep track of what pointers we've seen in the current recursive call
	// path, to avoid cycles that could lead to a stack overflow.
	ptrLevel uint
	ptrSeen  map[any]struct{}

	// NaN/Infinity handling mode
	nanInfMode NaNInfinityMode

	// Allow NUL characters in strings
	allowNUL bool

	// Maximum container nesting depth (0 = no limit)
	maxDepth int
	// Current container nesting depth
	depth int

	// Maximum big number magnitude byte length (0 = no limit)
	maxBigNumMagnitude int
	// Maximum big number exponent absolute value (0 = no limit)
	maxBigNumExponent int

	// Record definitions for the current document (nil if no records)
	recordDefs *recordAnalysis
}

const startDetectingCyclesAfter = 1000

var encodeStatePool sync.Pool

func newEncodeState() *encodeState {
	if v := encodeStatePool.Get(); v != nil {
		e := v.(*encodeState)
		e.Reset()
		if len(e.ptrSeen) > 0 {
			panic("ptrEncoder.encode should have emptied ptrSeen via defers")
		}
		e.ptrLevel = 0
		e.nanInfMode = NaNInfReject
		e.allowNUL = false
		e.maxDepth = 0
		e.depth = 0
		e.maxBigNumMagnitude = 0
		e.maxBigNumExponent = 0
		e.recordDefs = nil
		return e
	}
	return &encodeState{ptrSeen: make(map[any]struct{}), nanInfMode: NaNInfReject}
}

// jsonError is an error wrapper type for internal use only.
type jsonError struct{ error }

func (e *encodeState) marshal(v any, opts encOpts) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if je, ok := r.(jsonError); ok {
				err = je.error
			} else {
				panic(r)
			}
		}
	}()

	rv := reflect.ValueOf(v)

	// Analyze type tree for record candidates
	if rv.IsValid() {
		if ra := analyzeTypeForRecords(rv.Type()); ra != nil {
			e.recordDefs = ra
			// Write record definitions before the root value
			for _, def := range ra.defs {
				e.WriteByte(typeRecordDef)
				fields := cachedTypeFields(def.structType)
				for i := range fields.list {
					e.writeString(fields.list[i].name)
				}
				e.WriteByte(typeContainerEnd)
			}
		}
	}

	e.reflectValue(rv, opts)
	return nil
}

// error aborts the encoding by panicking with err wrapped in jsonError.
func (e *encodeState) error(err error) {
	panic(jsonError{err})
}

// enterContainer increments depth and panics if max depth is exceeded.
func (e *encodeState) enterContainer() {
	e.depth++
	if e.maxDepth > 0 && e.depth > e.maxDepth {
		e.error(&MaxDepthError{Depth: e.maxDepth, Offset: int64(e.Len())})
	}
}

// exitContainer decrements depth.
func (e *encodeState) exitContainer() {
	e.depth--
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Interface, reflect.Pointer:
		return v.IsZero()
	}
	return false
}

func (e *encodeState) reflectValue(v reflect.Value, opts encOpts) {
	valueEncoder(v)(e, v, opts)
}

type encOpts struct {
	// quoted causes primitive fields to be encoded inside strings.
	quoted bool
}

type encoderFunc func(e *encodeState, v reflect.Value, opts encOpts)

var encoderCache sync.Map // map[reflect.Type]encoderFunc

// Pre-cached encoders for exact primitive types indexed by Kind.
// Only used when the type exactly matches the built-in type (no custom types).
var primitiveEncoders = [...]encoderFunc{
	reflect.Bool:    boolEncoder,
	reflect.Int:     intEncoder,
	reflect.Int8:    intEncoder,
	reflect.Int16:   intEncoder,
	reflect.Int32:   intEncoder,
	reflect.Int64:   intEncoder,
	reflect.Uint:    uintEncoder,
	reflect.Uint8:   uintEncoder,
	reflect.Uint16:  uintEncoder,
	reflect.Uint32:  uintEncoder,
	reflect.Uint64:  uintEncoder,
	reflect.Uintptr: uintEncoder,
	reflect.Float32: float32Encoder,
	reflect.Float64: float64Encoder,
	reflect.String:  stringEncoder,
}

// primitiveTypes maps Kind to the canonical primitive type for fast comparison.
var primitiveTypes = [...]reflect.Type{
	reflect.Bool:    reflect.TypeOf(false),
	reflect.Int:     reflect.TypeOf(int(0)),
	reflect.Int8:    reflect.TypeOf(int8(0)),
	reflect.Int16:   reflect.TypeOf(int16(0)),
	reflect.Int32:   reflect.TypeOf(int32(0)),
	reflect.Int64:   reflect.TypeOf(int64(0)),
	reflect.Uint:    reflect.TypeOf(uint(0)),
	reflect.Uint8:   reflect.TypeOf(uint8(0)),
	reflect.Uint16:  reflect.TypeOf(uint16(0)),
	reflect.Uint32:  reflect.TypeOf(uint32(0)),
	reflect.Uint64:  reflect.TypeOf(uint64(0)),
	reflect.Uintptr: reflect.TypeOf(uintptr(0)),
	reflect.Float32: reflect.TypeOf(float32(0)),
	reflect.Float64: reflect.TypeOf(float64(0)),
	reflect.String:  reflect.TypeOf(""),
}

func valueEncoder(v reflect.Value) encoderFunc {
	if !v.IsValid() {
		return invalidValueEncoder
	}
	return typeEncoder(v.Type())
}

func typeEncoder(t reflect.Type) encoderFunc {
	// Fast path for exact primitive types - avoids sync.Map lookup.
	// Check Kind first (cheap int comparison), then verify exact type match.
	k := t.Kind()
	if k <= reflect.String {
		if enc := primitiveEncoders[k]; enc != nil && t == primitiveTypes[k] {
			return enc
		}
	}

	// Slow path for other types - use cache
	if fi, ok := encoderCache.Load(t); ok {
		return fi.(encoderFunc)
	}

	// To deal with recursive types, populate the map with an
	// indirect func before we build it.
	indirect := sync.OnceValue(func() encoderFunc {
		return newTypeEncoder(t, true)
	})
	fi, loaded := encoderCache.LoadOrStore(t, encoderFunc(func(e *encodeState, v reflect.Value, opts encOpts) {
		indirect()(e, v, opts)
	}))
	if loaded {
		return fi.(encoderFunc)
	}

	f := indirect()
	encoderCache.Store(t, f)
	return f
}

var (
	marshalerType     = reflect.TypeOf((*Marshaler)(nil)).Elem()
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	bigIntType        = reflect.TypeOf((*big.Int)(nil))
	bigFloatType      = reflect.TypeOf((*big.Float)(nil))
)

// newTypeEncoder constructs an encoderFunc for a type.
func newTypeEncoder(t reflect.Type, allowAddr bool) encoderFunc {
	// Check for BONJSON marshaler first
	if t.Kind() != reflect.Pointer && allowAddr && reflect.PointerTo(t).Implements(marshalerType) {
		return newCondAddrEncoder(addrMarshalerEncoder, newTypeEncoder(t, false))
	}
	if t.Implements(marshalerType) {
		return marshalerEncoder
	}
	// Check for big.Int and big.Float before TextMarshaler to use native BigNumber encoding
	if t == bigIntType {
		return bigIntEncoder
	}
	if t == bigFloatType {
		return bigFloatEncoder
	}
	if t.Kind() != reflect.Pointer && allowAddr && reflect.PointerTo(t).Implements(textMarshalerType) {
		return newCondAddrEncoder(addrTextMarshalerEncoder, newTypeEncoder(t, false))
	}
	if t.Implements(textMarshalerType) {
		return textMarshalerEncoder
	}

	switch t.Kind() {
	case reflect.Bool:
		return boolEncoder
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return intEncoder
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return uintEncoder
	case reflect.Float32:
		return float32Encoder
	case reflect.Float64:
		return float64Encoder
	case reflect.String:
		return stringEncoder
	case reflect.Interface:
		return interfaceEncoder
	case reflect.Struct:
		return newStructEncoder(t)
	case reflect.Map:
		return newMapEncoder(t)
	case reflect.Slice:
		return newSliceEncoder(t)
	case reflect.Array:
		return newArrayEncoder(t)
	case reflect.Pointer:
		return newPtrEncoder(t)
	default:
		return unsupportedTypeEncoder
	}
}

func invalidValueEncoder(e *encodeState, v reflect.Value, _ encOpts) {
	e.WriteByte(typeNull)
}

func marshalerEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	if v.Kind() == reflect.Pointer && v.IsNil() {
		e.WriteByte(typeNull)
		return
	}
	m, ok := v.Interface().(Marshaler)
	if !ok {
		e.WriteByte(typeNull)
		return
	}
	b, err := m.MarshalBONJSON()
	if err != nil {
		e.error(&MarshalerError{v.Type(), err, "MarshalBONJSON"})
	}
	// Just write the raw bytes - caller is responsible for valid BONJSON
	e.Write(b)
}

func addrMarshalerEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	va := v.Addr()
	if va.IsNil() {
		e.WriteByte(typeNull)
		return
	}
	m := va.Interface().(Marshaler)
	b, err := m.MarshalBONJSON()
	if err != nil {
		e.error(&MarshalerError{v.Type(), err, "MarshalBONJSON"})
	}
	e.Write(b)
}

func textMarshalerEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	if v.Kind() == reflect.Pointer && v.IsNil() {
		e.WriteByte(typeNull)
		return
	}
	m, ok := v.Interface().(encoding.TextMarshaler)
	if !ok {
		e.WriteByte(typeNull)
		return
	}
	b, err := m.MarshalText()
	if err != nil {
		e.error(&MarshalerError{v.Type(), err, "MarshalText"})
	}
	e.writeString(string(b))
}

func addrTextMarshalerEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	va := v.Addr()
	if va.IsNil() {
		e.WriteByte(typeNull)
		return
	}
	m := va.Interface().(encoding.TextMarshaler)
	b, err := m.MarshalText()
	if err != nil {
		e.error(&MarshalerError{v.Type(), err, "MarshalText"})
	}
	e.writeString(string(b))
}

// bigIntEncoder encodes *big.Int as a BONJSON BigNumber.
// checkBigNumberLimits checks if a BigNumber exceeds configured magnitude or exponent limits.
func (e *encodeState) checkBigNumberLimits(bn *BigNumber) {
	if e.maxBigNumMagnitude > 0 && bn.Significand != nil {
		magLen := len(bn.Significand.Bytes())
		if magLen > e.maxBigNumMagnitude {
			e.error(&MaxBigNumberMagnitudeError{
				Length: magLen,
				Max:    e.maxBigNumMagnitude,
				Offset: int64(e.Len()),
			})
		}
	}
	if e.maxBigNumExponent > 0 && bn.Exponent != nil && bn.Exponent.IsInt64() {
		exp := bn.Exponent.Int64()
		if exp < 0 {
			exp = -exp
		}
		if exp > int64(e.maxBigNumExponent) {
			e.error(&MaxBigNumberExponentError{
				Exponent: bn.Exponent.Int64(),
				Max:      e.maxBigNumExponent,
				Offset:   int64(e.Len()),
			})
		}
	}
	if e.maxBigNumExponent > 0 && bn.Exponent != nil && !bn.Exponent.IsInt64() {
		e.error(&MaxBigNumberExponentError{
			Exponent: 0,
			Max:      e.maxBigNumExponent,
			Offset:   int64(e.Len()),
		})
	}
}

func bigIntEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	if v.IsNil() {
		e.WriteByte(typeNull)
		return
	}
	bi := v.Interface().(*big.Int)
	if opts.quoted {
		e.writeString(bi.String())
		return
	}
	bn := bigIntToBigNumber(bi)
	e.checkBigNumberLimits(bn)
	n := encodeBigNumber(e.scratch[:], bn)
	e.Write(e.scratch[:n])
}

// bigFloatEncoder encodes *big.Float as a BONJSON BigNumber.
func bigFloatEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	if v.IsNil() {
		e.WriteByte(typeNull)
		return
	}
	bf := v.Interface().(*big.Float)
	if opts.quoted {
		e.writeString(bf.Text('g', -1))
		return
	}
	// Check for special values (infinity)
	// BigNumber has no special encoding for infinity
	if bf.IsInf() {
		switch e.nanInfMode {
		case NaNInfReject:
			e.error(&UnsupportedValueError{v, "infinity"})
			return
		case NaNInfStringify:
			if bf.Sign() < 0 {
				e.writeString("-Infinity")
			} else {
				e.writeString("Infinity")
			}
			return
		case NaNInfAllow:
			// Encode as float64 infinity
			if bf.Sign() < 0 {
				n := encodeFloat64(e.scratch[:], math.Inf(-1))
				e.Write(e.scratch[:n])
			} else {
				n := encodeFloat64(e.scratch[:], math.Inf(1))
				e.Write(e.scratch[:n])
			}
			return
		}
	}
	bn := bigFloatToBigNumber(bf)
	e.checkBigNumberLimits(bn)
	n := encodeBigNumber(e.scratch[:], bn)
	e.Write(e.scratch[:n])
}

// bigIntToBigNumber converts a *big.Int to a BigNumber.
func bigIntToBigNumber(bi *big.Int) *BigNumber {
	if bi.Sign() == 0 {
		return &BigNumber{
			Significand: new(big.Int),
			Exponent:    new(big.Int),
		}
	}

	// Try to factor out trailing decimal zeros to reduce significand size
	absVal := new(big.Int).Abs(bi)
	decStr := absVal.String()
	origLen := len(decStr)
	trimmed := strings.TrimRight(decStr, "0")
	trailingZeros := origLen - len(trimmed)

	sig := new(big.Int).Set(bi)
	exp := new(big.Int)

	if trailingZeros > 0 {
		// Divide significand by 10^trailingZeros
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(trailingZeros)), nil)
		sig.Div(sig, divisor)
		exp.SetInt64(int64(trailingZeros))
	}

	return &BigNumber{
		Significand: sig,
		Exponent:    exp,
	}
}

// bigFloatToBigNumber converts a *big.Float to a BigNumber.
// This converts the float to a decimal representation with significand x 10^exponent.
func bigFloatToBigNumber(bf *big.Float) *BigNumber {
	if bf.Sign() == 0 {
		return &BigNumber{
			Significand: new(big.Int),
			Exponent:    new(big.Int),
		}
	}

	negative := bf.Sign() < 0

	// Use Text to get a decimal string representation, then parse it
	text := bf.Text('e', -1) // Use scientific notation

	// Parse the scientific notation: [+-]d.dddde[+-]dd
	var significandStr string
	var exponent int64

	// Find 'e' or 'E'
	eIdx := strings.IndexAny(text, "eE")

	if eIdx >= 0 {
		significandStr = text[:eIdx]
		expStr := text[eIdx+1:]
		exponent, _ = strconv.ParseInt(expStr, 10, 64)
	} else {
		significandStr = text
		exponent = 0
	}

	// Remove sign from significand string
	if len(significandStr) > 0 && (significandStr[0] == '-' || significandStr[0] == '+') {
		significandStr = significandStr[1:]
	}

	// Remove decimal point and adjust exponent
	dotIdx := strings.IndexByte(significandStr, '.')

	if dotIdx >= 0 {
		fracDigits := len(significandStr) - dotIdx - 1
		exponent -= int64(fracDigits)
		significandStr = significandStr[:dotIdx] + significandStr[dotIdx+1:]
	}

	// Remove leading zeros from significand
	significandStr = strings.TrimLeft(significandStr, "0")
	if significandStr == "" {
		return &BigNumber{
			Significand: new(big.Int),
			Exponent:    new(big.Int),
		}
	}

	// Remove trailing zeros and adjust exponent
	origLen := len(significandStr)
	significandStr = strings.TrimRight(significandStr, "0")
	trailingZeros := origLen - len(significandStr)
	exponent += int64(trailingZeros)

	// Convert significand string to big.Int (signed)
	sigInt := new(big.Int)
	sigInt.SetString(significandStr, 10)
	if negative {
		sigInt.Neg(sigInt)
	}

	return &BigNumber{
		Significand: sigInt,
		Exponent:    big.NewInt(exponent),
	}
}

func boolEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	if opts.quoted {
		// Encode as string
		if v.Bool() {
			e.writeString("true")
		} else {
			e.writeString("false")
		}
		return
	}
	n := encodeBool(e.scratch[:], v.Bool())
	e.Write(e.scratch[:n])
}

func intEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	if opts.quoted {
		e.writeString(strconv.FormatInt(v.Int(), 10))
		return
	}
	n := encodeSignedInt(e.scratch[:], v.Int())
	e.Write(e.scratch[:n])
}

func uintEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	if opts.quoted {
		e.writeString(strconv.FormatUint(v.Uint(), 10))
		return
	}
	n := encodeUnsignedInt(e.scratch[:], v.Uint())
	e.Write(e.scratch[:n])
}

func float32Encoder(e *encodeState, v reflect.Value, opts encOpts) {
	f := v.Float()
	if math.IsInf(f, 0) || math.IsNaN(f) {
		switch e.nanInfMode {
		case NaNInfReject:
			e.error(&UnsupportedValueError{v, strconv.FormatFloat(f, 'g', -1, 32)})
		case NaNInfStringify:
			var s string
			if math.IsNaN(f) {
				s = "NaN"
			} else if math.IsInf(f, 1) {
				s = "Infinity"
			} else {
				s = "-Infinity"
			}
			e.writeString(s)
			return
		case NaNInfAllow:
			// Encode directly as float32, bypassing encodeNumber's NaN/Inf check
			n := encodeFloat32(e.scratch[:], float32(f))
			e.Write(e.scratch[:n])
			return
		}
	}
	if opts.quoted {
		e.writeString(strconv.FormatFloat(f, 'g', -1, 32))
		return
	}
	n, err := encodeNumber(e.scratch[:], f)
	if err != nil {
		e.error(err)
	}
	e.Write(e.scratch[:n])
}

func float64Encoder(e *encodeState, v reflect.Value, opts encOpts) {
	f := v.Float()
	if math.IsInf(f, 0) || math.IsNaN(f) {
		switch e.nanInfMode {
		case NaNInfReject:
			e.error(&UnsupportedValueError{v, strconv.FormatFloat(f, 'g', -1, 64)})
		case NaNInfStringify:
			var s string
			if math.IsNaN(f) {
				s = "NaN"
			} else if math.IsInf(f, 1) {
				s = "Infinity"
			} else {
				s = "-Infinity"
			}
			e.writeString(s)
			return
		case NaNInfAllow:
			// Encode directly as float64, bypassing encodeNumber's NaN/Inf check
			n := encodeFloat64(e.scratch[:], f)
			e.Write(e.scratch[:n])
			return
		}
	}
	if opts.quoted {
		e.writeString(strconv.FormatFloat(f, 'g', -1, 64))
		return
	}
	n, err := encodeNumber(e.scratch[:], f)
	if err != nil {
		e.error(err)
	}
	e.Write(e.scratch[:n])
}

func stringEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	s := v.String()
	// Validate UTF-8 - BONJSON doesn't allow invalid UTF-8
	if !utf8.ValidString(s) {
		e.error(&InvalidUTF8Error{Offset: 0})
	}
	// Check for NUL characters - strings.IndexByte uses SIMD-optimized assembly
	if !e.allowNUL {
		if i := strings.IndexByte(s, 0); i >= 0 {
			e.error(&NullInStringError{Offset: int64(i)})
		}
	}
	e.writeString(s)
}

// writeString writes a string to the encoder
func (e *encodeState) writeString(s string) {
	n := len(s)
	if n <= maxShortStringLen {
		// Short string: 1 byte type code + data
		e.WriteByte(typeShortStringBase + byte(n))
		e.WriteString(s)
	} else {
		// Long string: 0xFF + data + 0xFF
		e.WriteByte(typeLongString)
		e.WriteString(s)
		e.WriteByte(typeLongString)
	}
}

// writeSmallInt writes a signed integer to the encoder (fast path helper)
func (e *encodeState) writeSmallInt(v int64) {
	n := encodeSignedInt(e.scratch[:], v)
	e.Write(e.scratch[:n])
}

// writeFloat64 writes a float64 to the encoder (fast path helper)
func (e *encodeState) writeFloat64(f float64) {
	n, err := encodeNumber(e.scratch[:], f)
	if err != nil {
		e.error(err)
	}
	e.Write(e.scratch[:n])
}

func interfaceEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	if v.IsNil() {
		e.WriteByte(typeNull)
		return
	}
	e.reflectValue(v.Elem(), opts)
}

func unsupportedTypeEncoder(e *encodeState, v reflect.Value, _ encOpts) {
	e.error(&UnsupportedTypeError{v.Type()})
}

type structEncoder struct {
	fields structFields
	typ    reflect.Type
}

func (se structEncoder) encode(e *encodeState, v reflect.Value, opts encOpts) {
	// Check if this struct type should be encoded as a record instance
	if e.recordDefs != nil {
		if defIdx, ok := e.recordDefs.typeToIndex[se.typ]; ok {
			se.encodeRecordInstance(e, v, opts, defIdx)
			return
		}
	}

	e.enterContainer()
	// Write object type code
	e.WriteByte(typeObject)

	// Write fields
	for i := range se.fields.list {
		f := &se.fields.list[i]

		// Find the nested struct field by following f.index.
		fv := v
		skip := false
		for _, idx := range f.index {
			if fv.Kind() == reflect.Pointer {
				if fv.IsNil() {
					skip = true
					break
				}
				fv = fv.Elem()
			}
			fv = fv.Field(idx)
		}
		if skip {
			continue
		}

		if (f.omitEmpty && isEmptyValue(fv)) ||
			(f.omitZero && (f.isZero == nil && fv.IsZero() || (f.isZero != nil && f.isZero(fv)))) {
			continue
		}
		// Write field name
		e.writeString(f.name)
		opts.quoted = f.quoted
		f.encoder(e, fv, opts)
	}

	// Write container end marker
	e.WriteByte(typeContainerEnd)
	e.exitContainer()
}

// encodeRecordInstance writes a record instance: 0xBA + LEB128(defIndex) + values + 0xB6.
// Values are positional, matching the field order from the record definition.
// Trailing null values (from omitted fields) are elided.
func (se structEncoder) encodeRecordInstance(e *encodeState, v reflect.Value, opts encOpts, defIdx int) {
	e.enterContainer()
	// Write record instance type code
	e.WriteByte(typeRecordInst)

	// Write definition index as LEB128
	var leb [10]byte
	lebN := encodeLEB128(leb[:], uint64(defIdx))
	e.Write(leb[:lebN])

	// Determine field values and find last non-empty field
	type fieldValue struct {
		fv    reflect.Value
		f     *field
		empty bool // field is omitted (nil pointer chain or omitempty/omitzero)
	}

	fvs := make([]fieldValue, len(se.fields.list))
	lastNonEmpty := -1
	for i := range se.fields.list {
		f := &se.fields.list[i]
		fv := v
		skip := false
		for _, idx := range f.index {
			if fv.Kind() == reflect.Pointer {
				if fv.IsNil() {
					skip = true
					break
				}
				fv = fv.Elem()
			}
			fv = fv.Field(idx)
		}
		empty := skip ||
			(f.omitEmpty && isEmptyValue(fv)) ||
			(f.omitZero && (f.isZero == nil && fv.IsZero() || (f.isZero != nil && f.isZero(fv))))
		fvs[i] = fieldValue{fv: fv, f: f, empty: empty}
		if !empty {
			lastNonEmpty = i
		}
	}

	// Write values up to and including lastNonEmpty
	for i := 0; i <= lastNonEmpty; i++ {
		if fvs[i].empty {
			e.WriteByte(typeNull)
		} else {
			opts.quoted = fvs[i].f.quoted
			fvs[i].f.encoder(e, fvs[i].fv, opts)
		}
	}

	// Write container end marker
	e.WriteByte(typeContainerEnd)
	e.exitContainer()
}

func newStructEncoder(t reflect.Type) encoderFunc {
	se := structEncoder{fields: cachedTypeFields(t), typ: t}
	return se.encode
}

type mapEncoder struct {
	elemEnc encoderFunc
}

func (me mapEncoder) encode(e *encodeState, v reflect.Value, opts encOpts) {
	if v.IsNil() {
		e.WriteByte(typeNull)
		return
	}
	if e.ptrLevel++; e.ptrLevel > startDetectingCyclesAfter {
		ptr := v.UnsafePointer()
		if _, ok := e.ptrSeen[ptr]; ok {
			e.error(&UnsupportedValueError{v, fmt.Sprintf("encountered a cycle via %s", v.Type())})
		}
		e.ptrSeen[ptr] = struct{}{}
		defer delete(e.ptrSeen, ptr)
	}

	// Fast path for common map types to avoid reflect.MapRange overhead
	switch m := v.Interface().(type) {
	case map[string]string:
		me.encodeMapStringString(e, m)
		e.ptrLevel--
		return
	case map[string]any:
		me.encodeMapStringAny(e, m, opts)
		e.ptrLevel--
		return
	case map[string]int:
		me.encodeMapStringInt(e, m)
		e.ptrLevel--
		return
	case map[string]int64:
		me.encodeMapStringInt64(e, m)
		e.ptrLevel--
		return
	case map[string]float64:
		me.encodeMapStringFloat64(e, m)
		e.ptrLevel--
		return
	case map[string]bool:
		me.encodeMapStringBool(e, m)
		e.ptrLevel--
		return
	}

	// Slow path: use reflect.MapRange for other map types
	e.enterContainer()
	// Write object type code
	e.WriteByte(typeObject)

	// Extract and sort the keys.
	var (
		sv  = make([]reflectWithString, v.Len())
		mi  = v.MapRange()
		err error
	)
	for i := 0; mi.Next(); i++ {
		if sv[i].ks, err = resolveKeyName(mi.Key()); err != nil {
			e.error(fmt.Errorf("bonjson: encoding error for type %q: %q", v.Type().String(), err.Error()))
		}
		sv[i].v = mi.Value()
	}
	slices.SortFunc(sv, func(i, j reflectWithString) int {
		return strings.Compare(i.ks, j.ks)
	})

	for _, kv := range sv {
		e.writeString(kv.ks)
		me.elemEnc(e, kv.v, opts)
	}
	e.WriteByte(typeContainerEnd)
	e.exitContainer()
	e.ptrLevel--
}

// Fast path encoders for common map types

func (me mapEncoder) encodeMapStringString(e *encodeState, m map[string]string) {
	e.enterContainer()
	e.WriteByte(typeObject)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		e.writeString(k)
		e.writeString(m[k])
	}
	e.WriteByte(typeContainerEnd)
	e.exitContainer()
}

func (me mapEncoder) encodeMapStringAny(e *encodeState, m map[string]any, opts encOpts) {
	e.enterContainer()
	e.WriteByte(typeObject)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		e.writeString(k)
		e.reflectValue(reflect.ValueOf(m[k]), opts)
	}
	e.WriteByte(typeContainerEnd)
	e.exitContainer()
}

func (me mapEncoder) encodeMapStringInt(e *encodeState, m map[string]int) {
	e.enterContainer()
	e.WriteByte(typeObject)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		e.writeString(k)
		e.writeSmallInt(int64(m[k]))
	}
	e.WriteByte(typeContainerEnd)
	e.exitContainer()
}

func (me mapEncoder) encodeMapStringInt64(e *encodeState, m map[string]int64) {
	e.enterContainer()
	e.WriteByte(typeObject)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		e.writeString(k)
		e.writeSmallInt(m[k])
	}
	e.WriteByte(typeContainerEnd)
	e.exitContainer()
}

func (me mapEncoder) encodeMapStringFloat64(e *encodeState, m map[string]float64) {
	e.enterContainer()
	e.WriteByte(typeObject)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		e.writeString(k)
		e.writeFloat64(m[k])
	}
	e.WriteByte(typeContainerEnd)
	e.exitContainer()
}

func (me mapEncoder) encodeMapStringBool(e *encodeState, m map[string]bool) {
	e.enterContainer()
	e.WriteByte(typeObject)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		e.writeString(k)
		if m[k] {
			e.WriteByte(typeTrue)
		} else {
			e.WriteByte(typeFalse)
		}
	}
	e.WriteByte(typeContainerEnd)
	e.exitContainer()
}

func newMapEncoder(t reflect.Type) encoderFunc {
	switch t.Key().Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
	default:
		if !t.Key().Implements(textMarshalerType) {
			return unsupportedTypeEncoder
		}
	}
	me := mapEncoder{typeEncoder(t.Elem())}
	return me.encode
}

func encodeByteSlice(e *encodeState, v reflect.Value, _ encOpts) {
	if v.IsNil() {
		e.WriteByte(typeNull)
		return
	}

	s := v.Bytes()
	// Base64 encode
	encoded := base64.StdEncoding.EncodeToString(s)
	e.writeString(encoded)
}

// typedArrayTypeCode returns the typed array type code and element byte size for a Go kind.
// Returns (0, 0) if the kind is not eligible for typed array encoding.
func typedArrayTypeCode(k reflect.Kind) (typeCode byte, elemSize int) {
	switch k {
	case reflect.Float64:
		return typeTypedFloat64Array, 8
	case reflect.Float32:
		return typeTypedFloat32Array, 4
	case reflect.Int64:
		return typeTypedInt64Array, 8
	case reflect.Int32:
		return typeTypedInt32Array, 4
	case reflect.Int16:
		return typeTypedInt16Array, 2
	case reflect.Int8:
		return typeTypedInt8Array, 1
	case reflect.Uint64:
		return typeTypedUint64Array, 8
	case reflect.Uint32:
		return typeTypedUint32Array, 4
	case reflect.Uint16:
		return typeTypedUint16Array, 2
	case reflect.Uint8:
		return typeTypedUint8Array, 1
	case reflect.Int:
		return typeTypedInt64Array, 8
	case reflect.Uint:
		return typeTypedUint64Array, 8
	default:
		return 0, 0
	}
}

// encodeTypedArray writes a typed array: type_code + LEB128(count) + packed LE data.
// typeCode is the typed array type code, elemSize is bytes per element.
func encodeTypedArray(e *encodeState, v reflect.Value, typeCode byte, elemSize int) {
	n := v.Len()

	// Write type code
	e.WriteByte(typeCode)

	// Write element count as LEB128
	var leb [10]byte
	lebN := encodeLEB128(leb[:], uint64(n))
	e.Write(leb[:lebN])

	if n == 0 {
		return
	}

	// Write packed element data in little-endian order
	// Use a buffer to batch writes
	var buf [8]byte
	for i := 0; i < n; i++ {
		elem := v.Index(i)
		switch elemSize {
		case 1:
			switch typeCode {
			case typeTypedInt8Array:
				e.WriteByte(byte(int8(elem.Int())))
			default: // uint8
				e.WriteByte(byte(elem.Uint()))
			}
		case 2:
			switch typeCode {
			case typeTypedInt16Array:
				binary.LittleEndian.PutUint16(buf[:2], uint16(int16(elem.Int())))
			default: // uint16
				binary.LittleEndian.PutUint16(buf[:2], uint16(elem.Uint()))
			}
			e.Write(buf[:2])
		case 4:
			switch typeCode {
			case typeTypedFloat32Array:
				binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(float32(elem.Float())))
			case typeTypedInt32Array:
				binary.LittleEndian.PutUint32(buf[:4], uint32(int32(elem.Int())))
			default: // uint32
				binary.LittleEndian.PutUint32(buf[:4], uint32(elem.Uint()))
			}
			e.Write(buf[:4])
		case 8:
			switch typeCode {
			case typeTypedFloat64Array:
				binary.LittleEndian.PutUint64(buf[:8], math.Float64bits(elem.Float()))
			case typeTypedInt64Array:
				binary.LittleEndian.PutUint64(buf[:8], uint64(elem.Int()))
			default: // uint64
				binary.LittleEndian.PutUint64(buf[:8], elem.Uint())
			}
			e.Write(buf[:8])
		}
	}
}

func newTypedArrayEncoder(typeCode byte, elemSize int) encoderFunc {
	return func(e *encodeState, v reflect.Value, opts encOpts) {
		encodeTypedArray(e, v, typeCode, elemSize)
	}
}

type sliceEncoder struct {
	arrayEnc encoderFunc
}

func (se sliceEncoder) encode(e *encodeState, v reflect.Value, opts encOpts) {
	if v.IsNil() {
		e.WriteByte(typeNull)
		return
	}
	if e.ptrLevel++; e.ptrLevel > startDetectingCyclesAfter {
		ptr := struct {
			ptr any
			len int
		}{v.UnsafePointer(), v.Len()}
		if _, ok := e.ptrSeen[ptr]; ok {
			e.error(&UnsupportedValueError{v, fmt.Sprintf("encountered a cycle via %s", v.Type())})
		}
		e.ptrSeen[ptr] = struct{}{}
		defer delete(e.ptrSeen, ptr)
	}
	se.arrayEnc(e, v, opts)
	e.ptrLevel--
}

func newSliceEncoder(t reflect.Type) encoderFunc {
	// Byte slices get special treatment; arrays don't.
	if t.Elem().Kind() == reflect.Uint8 {
		p := reflect.PointerTo(t.Elem())
		if !p.Implements(marshalerType) && !p.Implements(textMarshalerType) {
			return encodeByteSlice
		}
	}
	// Check for typed array eligibility (numeric element types)
	elemKind := t.Elem().Kind()
	if typeCode, elemSize := typedArrayTypeCode(elemKind); typeCode != 0 {
		// Skip if element type has custom marshaling
		elemType := t.Elem()
		p := reflect.PointerTo(elemType)
		if !elemType.Implements(marshalerType) && !p.Implements(marshalerType) &&
			!elemType.Implements(textMarshalerType) && !p.Implements(textMarshalerType) {
			enc := sliceEncoder{newTypedArrayEncoder(typeCode, elemSize)}
			return enc.encode
		}
	}
	enc := sliceEncoder{newArrayEncoder(t)}
	return enc.encode
}

type arrayEncoder struct {
	elemEnc encoderFunc
}

func (ae arrayEncoder) encode(e *encodeState, v reflect.Value, opts encOpts) {
	e.enterContainer()
	n := v.Len()
	// Write array type code, elements, and container end marker
	e.WriteByte(typeArray)
	for i := 0; i < n; i++ {
		ae.elemEnc(e, v.Index(i), opts)
	}
	e.WriteByte(typeContainerEnd)
	e.exitContainer()
}

func newArrayEncoder(t reflect.Type) encoderFunc {
	// Check for typed array eligibility (numeric element types)
	elemKind := t.Elem().Kind()
	if typeCode, elemSize := typedArrayTypeCode(elemKind); typeCode != 0 {
		// Skip if element type has custom marshaling
		elemType := t.Elem()
		p := reflect.PointerTo(elemType)
		if !elemType.Implements(marshalerType) && !p.Implements(marshalerType) &&
			!elemType.Implements(textMarshalerType) && !p.Implements(textMarshalerType) {
			return newTypedArrayEncoder(typeCode, elemSize)
		}
	}
	enc := arrayEncoder{typeEncoder(t.Elem())}
	return enc.encode
}

type ptrEncoder struct {
	elemEnc encoderFunc
}

func (pe ptrEncoder) encode(e *encodeState, v reflect.Value, opts encOpts) {
	if v.IsNil() {
		e.WriteByte(typeNull)
		return
	}
	if e.ptrLevel++; e.ptrLevel > startDetectingCyclesAfter {
		ptr := v.Interface()
		if _, ok := e.ptrSeen[ptr]; ok {
			e.error(&UnsupportedValueError{v, fmt.Sprintf("encountered a cycle via %s", v.Type())})
		}
		e.ptrSeen[ptr] = struct{}{}
		defer delete(e.ptrSeen, ptr)
	}
	pe.elemEnc(e, v.Elem(), opts)
	e.ptrLevel--
}

func newPtrEncoder(t reflect.Type) encoderFunc {
	enc := ptrEncoder{typeEncoder(t.Elem())}
	return enc.encode
}

type condAddrEncoder struct {
	canAddrEnc, elseEnc encoderFunc
}

func (ce condAddrEncoder) encode(e *encodeState, v reflect.Value, opts encOpts) {
	if v.CanAddr() {
		ce.canAddrEnc(e, v, opts)
	} else {
		ce.elseEnc(e, v, opts)
	}
}

func newCondAddrEncoder(canAddrEnc, elseEnc encoderFunc) encoderFunc {
	enc := condAddrEncoder{canAddrEnc: canAddrEnc, elseEnc: elseEnc}
	return enc.encode
}

type reflectWithString struct {
	v  reflect.Value
	ks string
}

func resolveKeyName(k reflect.Value) (string, error) {
	if k.Kind() == reflect.String {
		return k.String(), nil
	}
	if tm, ok := k.Interface().(encoding.TextMarshaler); ok {
		if k.Kind() == reflect.Pointer && k.IsNil() {
			return "", nil
		}
		buf, err := tm.MarshalText()
		return string(buf), err
	}
	switch k.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(k.Uint(), 10), nil
	}
	panic("unexpected map key type")
}

// ============================================================================
// Record Analysis
// ============================================================================

// recordDef represents a single record definition (a struct type eligible for record encoding).
type recordDef struct {
	structType reflect.Type
}

// recordAnalysis holds the mapping from struct types to record definition indices.
type recordAnalysis struct {
	defs        []recordDef            // ordered list of definitions
	typeToIndex map[reflect.Type]int   // struct type → definition index
}

var recordAnalysisCache sync.Map // map[reflect.Type]*recordAnalysis

// analyzeTypeForRecords performs a DFS walk of the type tree to find struct types
// that are elements of slices or arrays. These struct types become record candidates,
// since encoding them as record instances avoids repeating key strings.
// Returns nil if no record candidates are found.
func analyzeTypeForRecords(t reflect.Type) *recordAnalysis {
	if ra, ok := recordAnalysisCache.Load(t); ok {
		return ra.(*recordAnalysis)
	}

	var defs []recordDef
	typeToIndex := make(map[reflect.Type]int)
	visited := make(map[reflect.Type]bool)

	var walk func(t reflect.Type, isContainerElem bool)
	walk = func(t reflect.Type, isContainerElem bool) {
		// Dereference pointers
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}

		if visited[t] {
			return
		}
		visited[t] = true

		switch t.Kind() {
		case reflect.Struct:
			// Check if this struct type has custom marshaling (skip if so)
			if t.Implements(marshalerType) || reflect.PointerTo(t).Implements(marshalerType) ||
				t.Implements(textMarshalerType) || reflect.PointerTo(t).Implements(textMarshalerType) {
				return
			}
			// Skip big.Int and big.Float which have special encoding
			if t == bigIntType.Elem() || t == bigFloatType.Elem() {
				return
			}

			fields := cachedTypeFields(t)
			if len(fields.list) == 0 {
				return
			}

			// If this struct is an element of a container (slice/array/map), register as record candidate
			if isContainerElem {
				if _, exists := typeToIndex[t]; !exists {
					typeToIndex[t] = len(defs)
					defs = append(defs, recordDef{structType: t})
				}
			}

			// Walk struct fields to find nested record candidates
			for i := range fields.list {
				f := &fields.list[i]
				walk(f.typ, false)
			}

		case reflect.Slice, reflect.Array:
			elemType := t.Elem()
			// Dereference pointer element
			for elemType.Kind() == reflect.Pointer {
				elemType = elemType.Elem()
			}
			// Walk the element type as a slice element
			walk(elemType, true)

		case reflect.Map:
			walk(t.Elem(), true)
		}
	}

	walk(t, false)

	var result *recordAnalysis
	if len(defs) > 0 {
		result = &recordAnalysis{
			defs:        defs,
			typeToIndex: typeToIndex,
		}
	}

	recordAnalysisCache.Store(t, result)
	return result
}
