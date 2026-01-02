// Copyright 2024 Karl Stenerud. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bonjson

import (
	"bytes"
	"encoding"
	"encoding/base64"
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
// Floating point, integer, and Number values encode as BONJSON numbers.
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
		return e
	}
	return &encodeState{ptrSeen: make(map[any]struct{})}
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
	e.reflectValue(reflect.ValueOf(v), opts)
	return nil
}

// error aborts the encoding by panicking with err wrapped in jsonError.
func (e *encodeState) error(err error) {
	panic(jsonError{err})
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
	reflect.Bool:    reflect.TypeFor[bool](),
	reflect.Int:     reflect.TypeFor[int](),
	reflect.Int8:    reflect.TypeFor[int8](),
	reflect.Int16:   reflect.TypeFor[int16](),
	reflect.Int32:   reflect.TypeFor[int32](),
	reflect.Int64:   reflect.TypeFor[int64](),
	reflect.Uint:    reflect.TypeFor[uint](),
	reflect.Uint8:   reflect.TypeFor[uint8](),
	reflect.Uint16:  reflect.TypeFor[uint16](),
	reflect.Uint32:  reflect.TypeFor[uint32](),
	reflect.Uint64:  reflect.TypeFor[uint64](),
	reflect.Uintptr: reflect.TypeFor[uintptr](),
	reflect.Float32: reflect.TypeFor[float32](),
	reflect.Float64: reflect.TypeFor[float64](),
	reflect.String:  reflect.TypeFor[string](),
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
	marshalerType     = reflect.TypeFor[Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
	bigIntType        = reflect.TypeFor[*big.Int]()
	bigFloatType      = reflect.TypeFor[*big.Float]()
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
	m, ok := reflect.TypeAssert[Marshaler](v)
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
	m, _ := reflect.TypeAssert[Marshaler](va)
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
	m, ok := reflect.TypeAssert[encoding.TextMarshaler](v)
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
	m, _ := reflect.TypeAssert[encoding.TextMarshaler](va)
	b, err := m.MarshalText()
	if err != nil {
		e.error(&MarshalerError{v.Type(), err, "MarshalText"})
	}
	e.writeString(string(b))
}

// bigIntEncoder encodes *big.Int as a BONJSON BigNumber.
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
	if bn == nil {
		// Number too large for BigNumber format, fall back to string encoding
		e.writeString(bi.String())
		return
	}
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
	if bf.IsInf() {
		e.error(&UnsupportedValueError{v, "infinity"})
		return
	}
	bn := bigFloatToBigNumber(bf)
	n := encodeBigNumber(e.scratch[:], bn)
	e.Write(e.scratch[:n])
}

// bigIntToBigNumber converts a *big.Int to a BigNumber.
// Returns nil if the number is too large to represent exactly in BigNumber format.
func bigIntToBigNumber(bi *big.Int) *BigNumber {
	if bi.Sign() == 0 {
		return &BigNumber{Negative: false}
	}

	negative := bi.Sign() < 0

	// Get absolute value - use Abs on a new big.Int to avoid modifying original
	absVal := new(big.Int).Abs(bi)
	absBytes := absVal.Bytes() // big-endian

	// If the number fits in 31 bytes, encode directly with exponent 0
	if len(absBytes) <= 31 {
		// Convert to little-endian
		significand := make([]byte, len(absBytes))
		for i, b := range absBytes {
			significand[len(absBytes)-1-i] = b
		}

		return &BigNumber{
			Significand: significand,
			Exponent:    0,
			Negative:    negative,
		}
	}

	// For larger numbers, try to use decimal representation with trailing zeros removed
	decStr := absVal.String()

	// Remove trailing zeros and count them as exponent
	origLen := len(decStr)
	decStr = strings.TrimRight(decStr, "0")
	exponent := int32(origLen - len(decStr))

	// Parse the trimmed string back to a big.Int
	sigInt := new(big.Int)
	sigInt.SetString(decStr, 10)
	sigBytes := sigInt.Bytes() // big-endian

	// If still too large, return nil to indicate we should fall back to string encoding
	if len(sigBytes) > 31 {
		return nil
	}

	// Convert to little-endian
	significand := make([]byte, len(sigBytes))
	for i, b := range sigBytes {
		significand[len(sigBytes)-1-i] = b
	}

	return &BigNumber{
		Significand: significand,
		Exponent:    exponent,
		Negative:    negative,
	}
}

// bigFloatToBigNumber converts a *big.Float to a BigNumber.
// This converts the float to a decimal representation with significand × 10^exponent.
func bigFloatToBigNumber(bf *big.Float) *BigNumber {
	if bf.Sign() == 0 {
		return &BigNumber{Negative: bf.Signbit()}
	}

	negative := bf.Sign() < 0

	// Use Text to get a decimal string representation, then parse it
	text := bf.Text('e', -1) // Use scientific notation

	// Parse the scientific notation: [+-]d.dddde[+-]dd
	var significandStr string
	var exponent int32

	// Find 'e' or 'E'
	eIdx := -1
	for i, c := range text {
		if c == 'e' || c == 'E' {
			eIdx = i
			break
		}
	}

	if eIdx >= 0 {
		significandStr = text[:eIdx]
		expStr := text[eIdx+1:]
		exp, _ := strconv.ParseInt(expStr, 10, 32)
		exponent = int32(exp)
	} else {
		significandStr = text
		exponent = 0
	}

	// Remove sign from significand string
	if len(significandStr) > 0 && (significandStr[0] == '-' || significandStr[0] == '+') {
		significandStr = significandStr[1:]
	}

	// Remove decimal point and adjust exponent
	dotIdx := -1
	for i, c := range significandStr {
		if c == '.' {
			dotIdx = i
			break
		}
	}

	if dotIdx >= 0 {
		// Number of digits after decimal point
		fracDigits := len(significandStr) - dotIdx - 1
		exponent -= int32(fracDigits)
		significandStr = significandStr[:dotIdx] + significandStr[dotIdx+1:]
	}

	// Remove leading zeros from significand
	significandStr = strings.TrimLeft(significandStr, "0")
	if significandStr == "" {
		return &BigNumber{Negative: negative}
	}

	// Remove trailing zeros and adjust exponent
	origLen := len(significandStr)
	significandStr = strings.TrimRight(significandStr, "0")
	trailingZeros := origLen - len(significandStr)
	exponent += int32(trailingZeros)

	// Convert significand string to big.Int then to bytes
	sigInt := new(big.Int)
	sigInt.SetString(significandStr, 10)

	// Get bytes in big-endian
	sigBytes := sigInt.Bytes()

	// Convert to little-endian
	significand := make([]byte, len(sigBytes))
	for i, b := range sigBytes {
		significand[len(sigBytes)-1-i] = b
	}

	return &BigNumber{
		Significand: significand,
		Exponent:    exponent,
		Negative:    negative,
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
		e.error(&UnsupportedValueError{v, strconv.FormatFloat(f, 'g', -1, 32)})
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
		e.error(&UnsupportedValueError{v, strconv.FormatFloat(f, 'g', -1, 64)})
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

var numberType = reflect.TypeFor[Number]()

func stringEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	if v.Type() == numberType {
		numStr := v.String()
		if numStr == "" {
			numStr = "0"
		}
		if !isValidNumber(numStr) {
			e.error(fmt.Errorf("bonjson: invalid number literal %q", numStr))
		}
		// Parse and encode as a proper number
		if strings.ContainsAny(numStr, ".eE") {
			f, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				e.error(err)
			}
			n, err := encodeNumber(e.scratch[:], f)
			if err != nil {
				e.error(err)
			}
			e.Write(e.scratch[:n])
		} else if numStr[0] == '-' {
			i, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				e.error(err)
			}
			n := encodeSignedInt(e.scratch[:], i)
			e.Write(e.scratch[:n])
		} else {
			u, err := strconv.ParseUint(numStr, 10, 64)
			if err != nil {
				e.error(err)
			}
			n := encodeUnsignedInt(e.scratch[:], u)
			e.Write(e.scratch[:n])
		}
		return
	}
	s := v.String()
	// Validate UTF-8 - BONJSON doesn't allow invalid UTF-8
	if !utf8.ValidString(s) {
		e.error(&InvalidUTF8Error{Offset: 0})
	}
	// Check for NUL characters - strings.IndexByte uses SIMD-optimized assembly
	if i := strings.IndexByte(s, 0); i >= 0 {
		e.error(&NullInStringError{Offset: int64(i)})
	}
	e.writeString(s)
}

// writeString writes a string to the encoder
func (e *encodeState) writeString(s string) {
	n := len(s)
	if n <= maxShortStringLen {
		// Short string: 1 byte type code + data
		e.WriteByte(typeShortStringBase | byte(n))
		e.WriteString(s)
	} else {
		// Long string: 1 byte type code + length field + data
		// Calculate length field size once and encode to scratch buffer
		payload := uint64(n) << 1 // continuation=false
		e.scratch[0] = typeLongString
		lfSize := encodeLengthPayload(e.scratch[1:], payload)
		e.Write(e.scratch[:1+lfSize])
		e.WriteString(s)
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
}

func (se structEncoder) encode(e *encodeState, v reflect.Value, opts encOpts) {
	e.WriteByte(typeObjectStart)
FieldLoop:
	for i := range se.fields.list {
		f := &se.fields.list[i]

		// Find the nested struct field by following f.index.
		fv := v
		for _, i := range f.index {
			if fv.Kind() == reflect.Pointer {
				if fv.IsNil() {
					continue FieldLoop
				}
				fv = fv.Elem()
			}
			fv = fv.Field(i)
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
	e.WriteByte(typeContainerEnd)
}

func newStructEncoder(t reflect.Type) encoderFunc {
	se := structEncoder{fields: cachedTypeFields(t)}
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
	e.WriteByte(typeObjectStart)

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
	e.ptrLevel--
}

// Fast path encoders for common map types

func (me mapEncoder) encodeMapStringString(e *encodeState, m map[string]string) {
	e.WriteByte(typeObjectStart)
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
}

func (me mapEncoder) encodeMapStringAny(e *encodeState, m map[string]any, opts encOpts) {
	e.WriteByte(typeObjectStart)
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
}

func (me mapEncoder) encodeMapStringInt(e *encodeState, m map[string]int) {
	e.WriteByte(typeObjectStart)
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
}

func (me mapEncoder) encodeMapStringInt64(e *encodeState, m map[string]int64) {
	e.WriteByte(typeObjectStart)
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
}

func (me mapEncoder) encodeMapStringFloat64(e *encodeState, m map[string]float64) {
	e.WriteByte(typeObjectStart)
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
}

func (me mapEncoder) encodeMapStringBool(e *encodeState, m map[string]bool) {
	e.WriteByte(typeObjectStart)
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
	enc := sliceEncoder{newArrayEncoder(t)}
	return enc.encode
}

type arrayEncoder struct {
	elemEnc encoderFunc
}

func (ae arrayEncoder) encode(e *encodeState, v reflect.Value, opts encOpts) {
	e.WriteByte(typeArrayStart)
	n := v.Len()
	for i := 0; i < n; i++ {
		ae.elemEnc(e, v.Index(i), opts)
	}
	e.WriteByte(typeContainerEnd)
}

func newArrayEncoder(t reflect.Type) encoderFunc {
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
	if tm, ok := reflect.TypeAssert[encoding.TextMarshaler](k); ok {
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

func isValidNumber(s string) bool {
	if s == "" {
		return false
	}

	// Optional -
	if s[0] == '-' {
		s = s[1:]
		if s == "" {
			return false
		}
	}

	// Digits
	switch {
	default:
		return false
	case s[0] == '0':
		s = s[1:]
	case '1' <= s[0] && s[0] <= '9':
		s = s[1:]
		for len(s) > 0 && '0' <= s[0] && s[0] <= '9' {
			s = s[1:]
		}
	}

	// . followed by 1 or more digits.
	if len(s) >= 2 && s[0] == '.' && '0' <= s[1] && s[1] <= '9' {
		s = s[2:]
		for len(s) > 0 && '0' <= s[0] && s[0] <= '9' {
			s = s[1:]
		}
	}

	// e or E followed by an optional - or + and 1 or more digits.
	if len(s) >= 2 && (s[0] == 'e' || s[0] == 'E') {
		s = s[1:]
		if s[0] == '+' || s[0] == '-' {
			s = s[1:]
			if s == "" {
				return false
			}
		}
		for len(s) > 0 && '0' <= s[0] && s[0] <= '9' {
			s = s[1:]
		}
	}

	return s == ""
}
