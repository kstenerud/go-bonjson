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

func valueEncoder(v reflect.Value) encoderFunc {
	if !v.IsValid() {
		return invalidValueEncoder
	}
	return typeEncoder(v.Type())
}

func typeEncoder(t reflect.Type) encoderFunc {
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
	// Check for NUL characters - disallowed by default
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			e.error(&NullInStringError{Offset: int64(i)})
		}
	}
	e.writeString(s)
}

// writeString writes a string to the encoder
func (e *encodeState) writeString(s string) {
	size := stringEncodedSize(s)
	e.Grow(size)
	buf := e.AvailableBuffer()
	n := encodeString(buf[:size], s)
	e.Write(buf[:n])
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
