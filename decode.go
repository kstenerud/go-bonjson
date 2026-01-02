// Copyright 2024 Karl Stenerud. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Unmarshal parses the BONJSON-encoded data and stores the result
// in the value pointed to by v. If v is nil or not a pointer,
// Unmarshal returns an InvalidUnmarshalError.
//
// Unmarshal uses the inverse of the encodings that Marshal uses,
// allocating maps, slices, and pointers as necessary.
//
// To unmarshal BONJSON into a pointer, Unmarshal first handles the case of
// the BONJSON being the null value. In that case, Unmarshal sets
// the pointer to nil. Otherwise, Unmarshal unmarshals the BONJSON into
// the value pointed at by the pointer.
//
// To unmarshal BONJSON into a value implementing Unmarshaler,
// Unmarshal calls that value's UnmarshalBONJSON method.
//
// To unmarshal BONJSON into a struct, Unmarshal matches incoming object keys
// to the keys used by Marshal (either the struct field name or its tag),
// preferring an exact match but also accepting a case-insensitive match.
//
// Unlike JSON's Unmarshal, BONJSON's Unmarshal:
// - Rejects documents with duplicate object keys
// - Rejects strings with invalid UTF-8
// - Rejects strings with NUL characters (by default)
// - Does not allow chunking (by default)
func Unmarshal(data []byte, v any) error {
	d := newDecodeState()
	defer decodeStatePool.Put(d)

	d.init(data)
	return d.unmarshal(v)
}

// Unmarshaler is the interface implemented by types
// that can unmarshal a BONJSON description of themselves.
type Unmarshaler interface {
	UnmarshalBONJSON([]byte) error
}

// A Number represents a BONJSON number literal.
type Number string

// String returns the literal text of the number.
func (n Number) String() string { return string(n) }

// Float64 returns the number as a float64.
func (n Number) Float64() (float64, error) {
	return strconv.ParseFloat(string(n), 64)
}

// Int64 returns the number as an int64.
func (n Number) Int64() (int64, error) {
	return strconv.ParseInt(string(n), 10, 64)
}

// decodeState represents the state while decoding a BONJSON value.
type decodeState struct {
	data   []byte
	off    int  // next read offset in data
	opcode byte // last read type code

	savedError            error
	useNumber             bool
	disallowUnknownFields bool
	allowChunking         bool
	allowNUL              bool
	maxStringLength       int64
	maxDepth              int
	currentDepth          int

	// Stack of maps for duplicate key detection in nested objects.
	// We reuse these maps to avoid allocations.
	seenKeysStack []map[string]struct{}
	seenKeysDepth int

	errorContext *errorContext
}

type errorContext struct {
	Struct     reflect.Type
	FieldStack []string
}

var decodeStatePool = sync.Pool{
	New: func() any {
		return new(decodeState)
	},
}

func newDecodeState() *decodeState {
	d := decodeStatePool.Get().(*decodeState)
	d.savedError = nil
	d.useNumber = false
	d.disallowUnknownFields = false
	d.allowChunking = false
	d.allowNUL = false
	d.maxStringLength = defaultMaxStringLength
	d.maxDepth = defaultMaxContainerDepth
	d.currentDepth = 0
	d.seenKeysDepth = 0
	return d
}

// pushSeenKeys returns a cleared map for duplicate key detection at the current nesting level.
// The map is reused across calls to avoid allocations.
func (d *decodeState) pushSeenKeys() map[string]struct{} {
	if d.seenKeysDepth >= len(d.seenKeysStack) {
		// Need to grow the stack
		d.seenKeysStack = append(d.seenKeysStack, make(map[string]struct{}))
	} else {
		// Reuse existing map, just clear it
		clear(d.seenKeysStack[d.seenKeysDepth])
	}
	m := d.seenKeysStack[d.seenKeysDepth]
	d.seenKeysDepth++
	return m
}

// popSeenKeys releases the current seenKeys map back to the stack.
func (d *decodeState) popSeenKeys() {
	d.seenKeysDepth--
}

func (d *decodeState) init(data []byte) {
	d.data = data
	d.off = 0
	d.savedError = nil
	if d.errorContext != nil {
		d.errorContext.Struct = nil
		d.errorContext.FieldStack = d.errorContext.FieldStack[:0]
	}
}

func (d *decodeState) saveError(err error) {
	if d.savedError == nil {
		d.savedError = d.addErrorContext(err)
	}
}

func (d *decodeState) addErrorContext(err error) error {
	if d.errorContext != nil && (d.errorContext.Struct != nil || len(d.errorContext.FieldStack) > 0) {
		switch err := err.(type) {
		case *UnmarshalTypeError:
			err.Struct = d.errorContext.Struct.Name()
			fieldStack := d.errorContext.FieldStack
			if err.Field != "" {
				fieldStack = append(fieldStack, err.Field)
			}
			err.Field = strings.Join(fieldStack, ".")
		}
	}
	return err
}

func (d *decodeState) unmarshal(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &InvalidUnmarshalError{reflect.TypeOf(v)}
	}

	err := d.value(rv)
	if err != nil {
		return d.addErrorContext(err)
	}

	// Check for trailing data
	if d.off < len(d.data) {
		return &SyntaxError{msg: "trailing data after value", Offset: int64(d.off)}
	}

	return d.savedError
}

// readByte reads a single byte from the input
func (d *decodeState) readByte() (byte, error) {
	if d.off >= len(d.data) {
		return 0, &TruncatedDataError{Expected: 1, Got: 0, Offset: int64(d.off)}
	}
	b := d.data[d.off]
	d.off++
	return b, nil
}

// peekByte peeks at the next byte without consuming it
func (d *decodeState) peekByte() (byte, error) {
	if d.off >= len(d.data) {
		return 0, &TruncatedDataError{Expected: 1, Got: 0, Offset: int64(d.off)}
	}
	return d.data[d.off], nil
}

// value decodes a BONJSON value into v
func (d *decodeState) value(v reflect.Value) error {
	tc, err := d.readByte()
	if err != nil {
		return err
	}
	d.opcode = tc
	return d.decodeValue(tc, v)
}

// decodeValue decodes a value given its type code
func (d *decodeState) decodeValue(tc byte, v reflect.Value) error {
	// If v is not valid, just skip the value
	if !v.IsValid() {
		return d.skipValue(tc)
	}

	// Check for unmarshaler first
	u, ut, pv := indirect(v, tc == typeNull)
	if u != nil {
		// Need to read the entire value for the unmarshaler
		start := d.off - 1
		if err := d.skipValue(tc); err != nil {
			return err
		}
		return u.UnmarshalBONJSON(d.data[start:d.off])
	}

	switch {
	case tc <= typeSmallIntMax:
		// Small positive integer (0-100)
		return d.storeInt(int64(tc), pv, ut)

	case tc >= typeSmallNegIntMin:
		// Small negative integer (-100 to -1)
		return d.storeInt(int64(int8(tc)), pv, ut)

	case tc >= typeUintBase && tc <= typeUintBase+7:
		// Unsigned integer
		n := int(tc&0x07) + 1
		if d.off+n > len(d.data) {
			return &TruncatedDataError{Expected: n, Got: len(d.data) - d.off, Offset: int64(d.off)}
		}
		var val uint64
		for i := 0; i < n; i++ {
			val |= uint64(d.data[d.off+i]) << (i * 8)
		}
		d.off += n
		return d.storeUint(val, pv, ut)

	case tc >= typeSintBase && tc <= typeSintBase+7:
		// Signed integer
		n := int(tc&0x07) + 1
		if d.off+n > len(d.data) {
			return &TruncatedDataError{Expected: n, Got: len(d.data) - d.off, Offset: int64(d.off)}
		}
		var val uint64
		for i := 0; i < n; i++ {
			val |= uint64(d.data[d.off+i]) << (i * 8)
		}
		d.off += n
		// Sign extend
		signedVal := int64(val)
		if n < 8 {
			signBit := uint64(1) << (n*8 - 1)
			if val&signBit != 0 {
				mask := ^uint64(0) << (n * 8)
				signedVal = int64(val | mask)
			}
		}
		return d.storeInt(signedVal, pv, ut)

	case tc == typeFloat16:
		f, err := decodeFloat16(d.data[d.off:])
		if err != nil {
			return err
		}
		d.off += 2
		return d.storeFloat(f, pv, ut)

	case tc == typeFloat32:
		f, err := decodeFloat32(d.data[d.off:])
		if err != nil {
			return err
		}
		d.off += 4
		return d.storeFloat(f, pv, ut)

	case tc == typeFloat64:
		f, err := decodeFloat64(d.data[d.off:])
		if err != nil {
			return err
		}
		d.off += 8
		return d.storeFloat(f, pv, ut)

	case tc == typeBigNumber:
		bn, n, err := decodeBigNumber(d.data[d.off:])
		if err != nil {
			return err
		}
		d.off += n
		// For *big.Int and *big.Float, use the original value v to preserve type info
		// since indirect() returns an invalid pv when TextUnmarshaler is found
		return d.storeBigNumber(bn, v, pv, ut)

	case tc >= typeShortStringBase && tc <= typeShortStringBase+0x0f:
		// Short string
		length := int(tc & 0x0f)
		if d.off+length > len(d.data) {
			return &TruncatedDataError{Expected: length, Got: len(d.data) - d.off, Offset: int64(d.off)}
		}
		s := d.data[d.off : d.off+length]
		d.off += length
		return d.storeString(s, pv, ut)

	case tc == typeLongString:
		s, n, err := decodeLongString(d.data[d.off:], d.allowChunking, d.maxStringLength)
		if err != nil {
			return err
		}
		d.off += n
		return d.storeString(s, pv, ut)

	case tc == typeNull:
		return d.storeNull(pv, v)

	case tc == typeFalse:
		return d.storeBool(false, pv, ut)

	case tc == typeTrue:
		return d.storeBool(true, pv, ut)

	case tc == typeArrayStart:
		return d.decodeArray(pv, v)

	case tc == typeObjectStart:
		return d.decodeObject(pv, v)

	default:
		return &InvalidTypeCodeError{TypeCode: tc, Offset: int64(d.off - 1)}
	}
}

// indirect walks down v allocating pointers as needed,
// until it gets to a non-pointer.
func indirect(v reflect.Value, decodingNull bool) (Unmarshaler, encoding.TextUnmarshaler, reflect.Value) {
	v0 := v
	haveAddr := false

	if v.Kind() != reflect.Pointer && v.Type().Name() != "" && v.CanAddr() {
		haveAddr = true
		v = v.Addr()
	}
	for {
		if v.Kind() == reflect.Interface && !v.IsNil() {
			e := v.Elem()
			if e.Kind() == reflect.Pointer && !e.IsNil() && (!decodingNull || e.Elem().Kind() == reflect.Pointer) {
				haveAddr = false
				v = e
				continue
			}
		}

		if v.Kind() != reflect.Pointer {
			break
		}

		if decodingNull && v.CanSet() {
			break
		}

		if v.Elem().Kind() == reflect.Interface && v.Elem().Elem().Equal(v) {
			v = v.Elem()
			break
		}
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		if v.Type().NumMethod() > 0 && v.CanInterface() {
			if u, ok := reflect.TypeAssert[Unmarshaler](v); ok {
				return u, nil, reflect.Value{}
			}
			if !decodingNull {
				if u, ok := reflect.TypeAssert[encoding.TextUnmarshaler](v); ok {
					return nil, u, reflect.Value{}
				}
			}
		}

		if haveAddr {
			v = v0
			haveAddr = false
		} else {
			v = v.Elem()
		}
	}
	return nil, nil, v
}

func (d *decodeState) storeInt(val int64, v reflect.Value, ut encoding.TextUnmarshaler) error {
	if ut != nil {
		return ut.UnmarshalText([]byte(strconv.FormatInt(val, 10)))
	}

	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.NumMethod() == 0 {
			if d.useNumber {
				v.Set(reflect.ValueOf(Number(strconv.FormatInt(val, 10))))
			} else {
				v.Set(reflect.ValueOf(float64(val)))
			}
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.off)})
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.OverflowInt(val) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatInt(val, 10), Type: v.Type(), Offset: int64(d.off)})
			return nil
		}
		v.SetInt(val)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if val < 0 {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatInt(val, 10), Type: v.Type(), Offset: int64(d.off)})
			return nil
		}
		if v.OverflowUint(uint64(val)) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatInt(val, 10), Type: v.Type(), Offset: int64(d.off)})
			return nil
		}
		v.SetUint(uint64(val))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(val))
	case reflect.String:
		if v.Type() == numberType {
			v.SetString(strconv.FormatInt(val, 10))
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.off)})
	default:
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.off)})
	}
	return nil
}

func (d *decodeState) storeUint(val uint64, v reflect.Value, ut encoding.TextUnmarshaler) error {
	if ut != nil {
		return ut.UnmarshalText([]byte(strconv.FormatUint(val, 10)))
	}

	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.NumMethod() == 0 {
			if d.useNumber {
				v.Set(reflect.ValueOf(Number(strconv.FormatUint(val, 10))))
			} else {
				v.Set(reflect.ValueOf(float64(val)))
			}
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.off)})
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if val > uint64(^uint(0)>>1) || v.OverflowInt(int64(val)) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatUint(val, 10), Type: v.Type(), Offset: int64(d.off)})
			return nil
		}
		v.SetInt(int64(val))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if v.OverflowUint(val) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatUint(val, 10), Type: v.Type(), Offset: int64(d.off)})
			return nil
		}
		v.SetUint(val)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(val))
	case reflect.String:
		if v.Type() == numberType {
			v.SetString(strconv.FormatUint(val, 10))
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.off)})
	default:
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.off)})
	}
	return nil
}

func (d *decodeState) storeFloat(val float64, v reflect.Value, ut encoding.TextUnmarshaler) error {
	if ut != nil {
		return ut.UnmarshalText([]byte(strconv.FormatFloat(val, 'g', -1, 64)))
	}

	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.NumMethod() == 0 {
			if d.useNumber {
				v.Set(reflect.ValueOf(Number(strconv.FormatFloat(val, 'g', -1, 64))))
			} else {
				v.Set(reflect.ValueOf(val))
			}
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.off)})
	case reflect.Float32, reflect.Float64:
		if v.OverflowFloat(val) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatFloat(val, 'g', -1, 64), Type: v.Type(), Offset: int64(d.off)})
			return nil
		}
		v.SetFloat(val)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i := int64(val)
		if float64(i) != val || v.OverflowInt(i) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatFloat(val, 'g', -1, 64), Type: v.Type(), Offset: int64(d.off)})
			return nil
		}
		v.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := uint64(val)
		if float64(u) != val || v.OverflowUint(u) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatFloat(val, 'g', -1, 64), Type: v.Type(), Offset: int64(d.off)})
			return nil
		}
		v.SetUint(u)
	case reflect.String:
		if v.Type() == numberType {
			v.SetString(strconv.FormatFloat(val, 'g', -1, 64))
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.off)})
	default:
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.off)})
	}
	return nil
}

func (d *decodeState) storeBigNumber(bn *BigNumber, origV reflect.Value, pv reflect.Value, ut encoding.TextUnmarshaler) error {
	// Check for *big.Int target first - preserve exact integer value
	// Use origV since pv may be invalid when TextUnmarshaler is found
	if origV.IsValid() && origV.Kind() == reflect.Pointer && origV.Type().Elem() == reflect.TypeFor[big.Int]() {
		return d.storeBigNumberToBigInt(bn, origV)
	}
	// Check for *big.Float target - preserve arbitrary precision
	if origV.IsValid() && origV.Kind() == reflect.Pointer && origV.Type().Elem() == reflect.TypeFor[big.Float]() {
		return d.storeBigNumberToBigFloat(bn, origV)
	}

	// Use pv for generic handling
	v := pv

	// Convert BigNumber to string representation and parse
	// This is a simplification - a full implementation would handle this more efficiently
	if len(bn.Significand) == 0 {
		// Zero
		return d.storeInt(0, v, ut)
	}

	// Build the significand value using binary.LittleEndian for efficiency
	var sig uint64
	switch len(bn.Significand) {
	case 1:
		sig = uint64(bn.Significand[0])
	case 2:
		sig = uint64(binary.LittleEndian.Uint16(bn.Significand))
	case 3:
		sig = uint64(bn.Significand[0]) | uint64(bn.Significand[1])<<8 | uint64(bn.Significand[2])<<16
	case 4:
		sig = uint64(binary.LittleEndian.Uint32(bn.Significand))
	default:
		// For 5-8 bytes, read as uint64 (padded with zeros)
		var buf [8]byte
		copy(buf[:], bn.Significand)
		sig = binary.LittleEndian.Uint64(buf[:])
	}

	// For now, convert to float64 (a proper implementation would handle arbitrary precision)
	f := float64(sig)
	if bn.Negative {
		f = -f
	}
	if bn.Exponent != 0 {
		// Apply base-10 exponent using math.Pow10 (much faster than loop)
		f *= math.Pow10(int(bn.Exponent))
	}
	return d.storeFloat(f, v, ut)
}

// storeBigNumberToBigInt converts a BigNumber to a *big.Int with full precision.
func (d *decodeState) storeBigNumberToBigInt(bn *BigNumber, v reflect.Value) error {
	if len(bn.Significand) == 0 {
		// Zero
		if v.IsNil() {
			v.Set(reflect.ValueOf(new(big.Int)))
		}
		v.Elem().Set(reflect.ValueOf(*big.NewInt(0)))
		return nil
	}

	// Convert significand from little-endian to big.Int
	// big.Int.SetBytes expects big-endian
	bigEndian := make([]byte, len(bn.Significand))
	for i, b := range bn.Significand {
		bigEndian[len(bn.Significand)-1-i] = b
	}

	sigInt := new(big.Int).SetBytes(bigEndian)

	// Apply exponent if any (multiply by 10^exponent)
	if bn.Exponent != 0 {
		if bn.Exponent > 0 {
			// Multiply by 10^exponent
			multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(bn.Exponent)), nil)
			sigInt.Mul(sigInt, multiplier)
		} else {
			// Divide by 10^(-exponent) - this may lose precision for non-integer results
			divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-bn.Exponent)), nil)
			sigInt.Div(sigInt, divisor)
		}
	}

	// Apply sign
	if bn.Negative {
		sigInt.Neg(sigInt)
	}

	if v.IsNil() {
		v.Set(reflect.ValueOf(new(big.Int)))
	}
	v.Elem().Set(reflect.ValueOf(*sigInt))
	return nil
}

// storeBigNumberToBigFloat converts a BigNumber to a *big.Float with full precision.
func (d *decodeState) storeBigNumberToBigFloat(bn *BigNumber, v reflect.Value) error {
	if len(bn.Significand) == 0 {
		// Zero
		if v.IsNil() {
			v.Set(reflect.ValueOf(new(big.Float)))
		}
		result := new(big.Float)
		if bn.Negative {
			result.Neg(result) // -0
		}
		v.Elem().Set(reflect.ValueOf(*result))
		return nil
	}

	// Convert significand from little-endian to big.Int
	bigEndian := make([]byte, len(bn.Significand))
	for i, b := range bn.Significand {
		bigEndian[len(bn.Significand)-1-i] = b
	}

	sigInt := new(big.Int).SetBytes(bigEndian)

	// Convert to big.Float
	result := new(big.Float).SetInt(sigInt)

	// Apply exponent if any
	if bn.Exponent != 0 {
		if bn.Exponent > 0 {
			// Multiply by 10^exponent
			multiplier := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(bn.Exponent)), nil))
			result.Mul(result, multiplier)
		} else {
			// Divide by 10^(-exponent)
			divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-bn.Exponent)), nil))
			result.Quo(result, divisor)
		}
	}

	// Apply sign
	if bn.Negative {
		result.Neg(result)
	}

	if v.IsNil() {
		v.Set(reflect.ValueOf(new(big.Float)))
	}
	v.Elem().Set(reflect.ValueOf(*result))
	return nil
}

func (d *decodeState) storeString(s []byte, v reflect.Value, ut encoding.TextUnmarshaler) error {
	// Validate UTF-8
	if !utf8.Valid(s) {
		return &InvalidUTF8Error{Offset: int64(d.off - len(s))}
	}

	// Check for NUL - bytes.IndexByte uses SIMD-optimized assembly
	if !d.allowNUL {
		if i := bytes.IndexByte(s, 0); i >= 0 {
			return &NullInStringError{Offset: int64(d.off - len(s) + i)}
		}
	}

	if ut != nil {
		return ut.UnmarshalText(s)
	}

	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf(string(s)))
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "string", Type: v.Type(), Offset: int64(d.off)})
	case reflect.String:
		v.SetString(string(s))
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			// []byte - base64 decode
			b := make([]byte, base64.StdEncoding.DecodedLen(len(s)))
			n, err := base64.StdEncoding.Decode(b, s)
			if err != nil {
				d.saveError(err)
				return nil
			}
			v.SetBytes(b[:n])
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "string", Type: v.Type(), Offset: int64(d.off)})
	default:
		d.saveError(&UnmarshalTypeError{Value: "string", Type: v.Type(), Offset: int64(d.off)})
	}
	return nil
}

func (d *decodeState) storeBool(val bool, v reflect.Value, ut encoding.TextUnmarshaler) error {
	if ut != nil {
		if val {
			return ut.UnmarshalText([]byte("true"))
		}
		return ut.UnmarshalText([]byte("false"))
	}

	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf(val))
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "bool", Type: v.Type(), Offset: int64(d.off)})
	case reflect.Bool:
		v.SetBool(val)
	default:
		d.saveError(&UnmarshalTypeError{Value: "bool", Type: v.Type(), Offset: int64(d.off)})
	}
	return nil
}

func (d *decodeState) storeNull(v reflect.Value, pv reflect.Value) error {
	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Map, reflect.Slice:
		v.SetZero()
	}
	return nil
}

func (d *decodeState) decodeArray(v reflect.Value, pv reflect.Value) error {
	// Check depth
	d.currentDepth++
	if d.currentDepth > d.maxDepth {
		return &MaxDepthError{Depth: d.maxDepth, Offset: int64(d.off)}
	}
	defer func() { d.currentDepth-- }()

	// Handle interface{}
	if v.Kind() == reflect.Interface && v.NumMethod() == 0 {
		ai := d.arrayInterface()
		if d.savedError != nil {
			return d.savedError
		}
		v.Set(reflect.ValueOf(ai))
		return nil
	}

	// Check type
	switch v.Kind() {
	case reflect.Array, reflect.Slice:
		// ok
	default:
		d.saveError(&UnmarshalTypeError{Value: "array", Type: v.Type(), Offset: int64(d.off)})
		return d.skipContainer()
	}

	i := 0
	for {
		tc, err := d.peekByte()
		if err != nil {
			return err
		}

		if tc == typeContainerEnd {
			d.off++ // consume the end marker
			break
		}

		// Expand slice if necessary
		if v.Kind() == reflect.Slice {
			if i >= v.Cap() {
				v.Grow(1)
			}
			if i >= v.Len() {
				v.SetLen(i + 1)
			}
		}

		if i < v.Len() {
			if err := d.value(v.Index(i)); err != nil {
				return err
			}
		} else {
			// Ran out of fixed array: skip
			if err := d.value(reflect.Value{}); err != nil {
				return err
			}
		}
		i++
	}

	if i < v.Len() {
		if v.Kind() == reflect.Array {
			for ; i < v.Len(); i++ {
				v.Index(i).SetZero()
			}
		} else {
			v.SetLen(i)
		}
	}
	if i == 0 && v.Kind() == reflect.Slice {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
	}
	return nil
}

var textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

func (d *decodeState) decodeObject(v reflect.Value, pv reflect.Value) error {
	// Check depth
	d.currentDepth++
	if d.currentDepth > d.maxDepth {
		return &MaxDepthError{Depth: d.maxDepth, Offset: int64(d.off)}
	}
	defer func() { d.currentDepth-- }()

	// Handle interface{}
	if v.Kind() == reflect.Interface && v.NumMethod() == 0 {
		oi := d.objectInterface()
		if d.savedError != nil {
			return d.savedError
		}
		v.Set(reflect.ValueOf(oi))
		return nil
	}

	t := v.Type()
	var fields structFields

	switch v.Kind() {
	case reflect.Map:
		switch t.Key().Kind() {
		case reflect.String,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		default:
			if !reflect.PointerTo(t.Key()).Implements(textUnmarshalerType) {
				d.saveError(&UnmarshalTypeError{Value: "object", Type: t, Offset: int64(d.off)})
				return d.skipContainer()
			}
		}
		if v.IsNil() {
			v.Set(reflect.MakeMap(t))
		}
	case reflect.Struct:
		fields = cachedTypeFields(t)
	default:
		d.saveError(&UnmarshalTypeError{Value: "object", Type: t, Offset: int64(d.off)})
		return d.skipContainer()
	}

	// Track keys for duplicate detection in structs only
	// For maps, we check the map itself
	var seenKeys map[string]struct{}
	if v.Kind() == reflect.Struct {
		seenKeys = d.pushSeenKeys()
		defer d.popSeenKeys()
	}
	var mapElem reflect.Value

	for {
		tc, err := d.peekByte()
		if err != nil {
			return err
		}

		if tc == typeContainerEnd {
			d.off++
			break
		}

		// Read key (must be a string)
		keyStart := d.off
		key, err := d.readString()
		if err != nil {
			return err
		}

		// For structs, check duplicate using seenKeys
		if seenKeys != nil {
			normalizedKey := string(key)
			if _, seen := seenKeys[normalizedKey]; seen {
				return &DuplicateKeyError{Key: normalizedKey, Offset: int64(keyStart)}
			}
			seenKeys[normalizedKey] = struct{}{}
		}

		// Find field for key
		var subv reflect.Value

		if v.Kind() == reflect.Map {
			elemType := t.Elem()
			if !mapElem.IsValid() {
				mapElem = reflect.New(elemType).Elem()
			} else {
				mapElem.SetZero()
			}
			subv = mapElem
		} else {
			f := fields.byExactName[string(key)]
			if f == nil {
				f = fields.byFoldedName[string(foldName(key))]
			}
			if f != nil {
				subv = v
				for _, ind := range f.index {
					if subv.Kind() == reflect.Pointer {
						if subv.IsNil() {
							if !subv.CanSet() {
								d.saveError(fmt.Errorf("bonjson: cannot set embedded pointer to unexported struct: %v", subv.Type().Elem()))
								subv = reflect.Value{}
								break
							}
							subv.Set(reflect.New(subv.Type().Elem()))
						}
						subv = subv.Elem()
					}
					subv = subv.Field(ind)
				}
			} else if d.disallowUnknownFields {
				d.saveError(fmt.Errorf("bonjson: unknown field %q", key))
			}
		}

		// Read value
		if err := d.value(subv); err != nil {
			return err
		}

		// Write to map
		if v.Kind() == reflect.Map {
			kt := t.Key()
			var kv reflect.Value
			switch kt.Kind() {
			case reflect.String:
				kv = reflect.New(kt).Elem()
				kv.SetString(string(key))
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				n, err := strconv.ParseInt(string(key), 10, 64)
				if err != nil || kt.OverflowInt(n) {
					d.saveError(&UnmarshalTypeError{Value: "number " + string(key), Type: kt, Offset: int64(keyStart)})
					continue
				}
				kv = reflect.New(kt).Elem()
				kv.SetInt(n)
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
				n, err := strconv.ParseUint(string(key), 10, 64)
				if err != nil || kt.OverflowUint(n) {
					d.saveError(&UnmarshalTypeError{Value: "number " + string(key), Type: kt, Offset: int64(keyStart)})
					continue
				}
				kv = reflect.New(kt).Elem()
				kv.SetUint(n)
			default:
				// TextUnmarshaler
				kv = reflect.New(kt)
				if err := kv.Interface().(encoding.TextUnmarshaler).UnmarshalText(key); err != nil {
					d.saveError(err)
					continue
				}
				kv = kv.Elem()
			}
			if kv.IsValid() {
				// Check for duplicate key using the map itself
				if v.MapIndex(kv).IsValid() {
					return &DuplicateKeyError{Key: string(key), Offset: int64(keyStart)}
				}
				v.SetMapIndex(kv, subv)
			}
		}
	}
	return nil
}

// readString reads a string value from the input
func (d *decodeState) readString() ([]byte, error) {
	tc, err := d.readByte()
	if err != nil {
		return nil, err
	}

	switch {
	case tc >= typeShortStringBase && tc <= typeShortStringBase+0x0f:
		length := int(tc & 0x0f)
		if d.off+length > len(d.data) {
			return nil, &TruncatedDataError{Expected: length, Got: len(d.data) - d.off, Offset: int64(d.off)}
		}
		s := d.data[d.off : d.off+length]
		d.off += length
		// Validate UTF-8
		if !utf8.Valid(s) {
			return nil, &InvalidUTF8Error{Offset: int64(d.off - length)}
		}
		// Check for NUL - bytes.IndexByte uses SIMD-optimized assembly
		if !d.allowNUL {
			if i := bytes.IndexByte(s, 0); i >= 0 {
				return nil, &NullInStringError{Offset: int64(d.off - length + i)}
			}
		}
		return s, nil

	case tc == typeLongString:
		s, n, err := decodeLongString(d.data[d.off:], d.allowChunking, d.maxStringLength)
		if err != nil {
			return nil, err
		}
		d.off += n
		// Check for NUL - bytes.IndexByte uses SIMD-optimized assembly
		if !d.allowNUL {
			if i := bytes.IndexByte(s, 0); i >= 0 {
				return nil, &NullInStringError{Offset: int64(d.off - n + i)}
			}
		}
		return s, nil

	default:
		return nil, &SyntaxError{msg: "expected string", Offset: int64(d.off - 1)}
	}
}

// skipValue skips over a value in the input
func (d *decodeState) skipValue(tc byte) error {
	switch {
	case tc <= typeSmallIntMax:
		return nil
	case tc >= typeSmallNegIntMin:
		return nil
	case tc >= typeUintBase && tc <= typeUintBase+7:
		n := int(tc&0x07) + 1
		if d.off+n > len(d.data) {
			return &TruncatedDataError{Expected: n, Got: len(d.data) - d.off, Offset: int64(d.off)}
		}
		d.off += n
		return nil
	case tc >= typeSintBase && tc <= typeSintBase+7:
		n := int(tc&0x07) + 1
		if d.off+n > len(d.data) {
			return &TruncatedDataError{Expected: n, Got: len(d.data) - d.off, Offset: int64(d.off)}
		}
		d.off += n
		return nil
	case tc == typeFloat16:
		if d.off+2 > len(d.data) {
			return &TruncatedDataError{Expected: 2, Got: len(d.data) - d.off, Offset: int64(d.off)}
		}
		d.off += 2
		return nil
	case tc == typeFloat32:
		if d.off+4 > len(d.data) {
			return &TruncatedDataError{Expected: 4, Got: len(d.data) - d.off, Offset: int64(d.off)}
		}
		d.off += 4
		return nil
	case tc == typeFloat64:
		if d.off+8 > len(d.data) {
			return &TruncatedDataError{Expected: 8, Got: len(d.data) - d.off, Offset: int64(d.off)}
		}
		d.off += 8
		return nil
	case tc == typeBigNumber:
		_, n, err := decodeBigNumber(d.data[d.off:])
		if err != nil {
			return err
		}
		d.off += n
		return nil
	case tc >= typeShortStringBase && tc <= typeShortStringBase+0x0f:
		length := int(tc & 0x0f)
		if d.off+length > len(d.data) {
			return &TruncatedDataError{Expected: length, Got: len(d.data) - d.off, Offset: int64(d.off)}
		}
		d.off += length
		return nil
	case tc == typeLongString:
		_, n, err := decodeLongString(d.data[d.off:], d.allowChunking, d.maxStringLength)
		if err != nil {
			return err
		}
		d.off += n
		return nil
	case tc == typeNull, tc == typeFalse, tc == typeTrue:
		return nil
	case tc == typeArrayStart, tc == typeObjectStart:
		return d.skipContainer()
	default:
		return &InvalidTypeCodeError{TypeCode: tc, Offset: int64(d.off - 1)}
	}
}

// skipContainer skips over an array or object
func (d *decodeState) skipContainer() error {
	depth := 1
	for depth > 0 {
		tc, err := d.readByte()
		if err != nil {
			return err
		}
		switch tc {
		case typeArrayStart, typeObjectStart:
			depth++
		case typeContainerEnd:
			depth--
		default:
			if err := d.skipValue(tc); err != nil {
				return err
			}
		}
	}
	return nil
}

// arrayInterface decodes an array into []any
func (d *decodeState) arrayInterface() []any {
	var v = make([]any, 0)
	for {
		tc, err := d.peekByte()
		if err != nil {
			d.saveError(err)
			return v
		}
		if tc == typeContainerEnd {
			d.off++
			break
		}

		var elem any
		ev := reflect.ValueOf(&elem).Elem()
		if err := d.value(ev); err != nil {
			d.saveError(err)
			return v
		}
		v = append(v, elem)
	}
	return v
}

// objectInterface decodes an object into map[string]any
func (d *decodeState) objectInterface() map[string]any {
	m := make(map[string]any)

	for {
		tc, err := d.peekByte()
		if err != nil {
			d.saveError(err)
			return m
		}
		if tc == typeContainerEnd {
			d.off++
			break
		}

		keyStart := d.off
		key, err := d.readString()
		if err != nil {
			d.saveError(err)
			return m
		}

		keyStr := string(key)
		// Check for duplicate key using the map itself
		if _, exists := m[keyStr]; exists {
			d.saveError(&DuplicateKeyError{Key: keyStr, Offset: int64(keyStart)})
			return m
		}

		var val any
		ev := reflect.ValueOf(&val).Elem()
		if err := d.value(ev); err != nil {
			d.saveError(err)
			return m
		}
		m[keyStr] = val
	}
	return m
}
