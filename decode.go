//
// decode.go
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
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strconv"
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
// - Rejects strings with NUL characters (unless configured to allow)
func Unmarshal(data []byte, v any) error {
	d := newDecodeState()
	defer decodeStatePool.Put(d)

	d.init(data)
	return d.unmarshal(v)
}

// UnmarshalWithByteCount parses the BONJSON-encoded data and stores the result
// in the value pointed to by v, returning the number of bytes consumed.
// It behaves identically to [Unmarshal], but additionally returns the byte
// count which indicates how far into the data decoding progressed.
//
// The returned byte count is valid even when an error occurs, allowing
// callers to determine the position where the error was encountered.
//
// If v is nil or not a pointer, UnmarshalWithByteCount returns [InvalidUnmarshalError].
//
// # Possible Errors
//
// The following error types may be returned during decoding:
//
//   - [TrailingDataError]: Unexpected data was found after the decoded value.
//     The decoded document is complete, but there is extra data after the
//     document ended.
//
//   - [TruncatedDataError]: The input data ended before a complete value
//     could be read. The destination may contain a partially decoded value.
//
//   - [MaxDepthError]: Container nesting exceeded the maximum allowed depth.
//
//   - [TooManyChunksError]: A chunked string exceeded the maximum chunk count.
//
//   - [UnmarshalTypeError]: The BONJSON value's type is incompatible with
//     the destination Go type (including numeric overflow).
//
//   - [NullInStringError]: A string contained a NUL character and we're
//     configured to refuse it.
//
//   - [InvalidTypeCodeError]: An unrecognized or reserved type code was
//     encountered. Decoding stopped at the invalid byte.
//
//   - [InvalidValueError]: A structurally valid but semantically invalid
//     value was encountered (e.g., NaN or Infinity in a big number).
//
//   - [InvalidUTF8Error]: A string contained invalid UTF-8 sequences.
//
//   - [DuplicateKeyError]: An object contained duplicate keys.
//
//   - [SyntaxError]: Other structural errors in the BONJSON data (e.g.,
//     non-string object keys).
func UnmarshalWithByteCount(data []byte, v any) (int, error) {
	d := newDecodeState()
	defer decodeStatePool.Put(d)

	d.init(data)
	err := d.unmarshal(v)
	return d.offsetIntoData, err
}

// Unmarshaler is the interface implemented by types
// that can unmarshal a BONJSON description of themselves.
type Unmarshaler interface {
	UnmarshalBONJSON([]byte) error
}

// decodeState represents the state while decoding a BONJSON value.
type decodeState struct {
	data           []byte
	offsetIntoData int
	containerDepth int

	disallowUnknownFields    bool
	allowNUL                 bool
	allowNaNInf              bool
	invalidUTF8Mode          InvalidUTF8Mode
	duplicateKeyMode         DuplicateKeyMode
	maxAllowedChunks         int
	maxAllowedStringLength   int64
	maxAllowedContainerDepth int

	// Stack of boolean slices for duplicate key detection in nested structs.
	// Using field indices is more efficient than map lookups for structs.
	seenStructFieldsStack [][]bool
	seenStructFieldsDepth int

	savedError error
}

var decodeStatePool = sync.Pool{
	New: func() any {
		return new(decodeState)
	},
}

func newDecodeState() *decodeState {
	d := decodeStatePool.Get().(*decodeState)
	d.savedError = nil
	d.disallowUnknownFields = false
	d.maxAllowedChunks = defaultMaxChunks
	d.allowNUL = false
	d.allowNaNInf = false
	d.invalidUTF8Mode = UTF8Reject
	d.duplicateKeyMode = DupKeyReject
	d.maxAllowedStringLength = defaultMaxStringLength
	d.maxAllowedContainerDepth = defaultMaxContainerDepth
	d.containerDepth = 0
	d.seenStructFieldsDepth = 0
	return d
}

// pushSeenStructFields returns a cleared boolean slice for duplicate field detection in structs.
// The slice is reused across calls to avoid allocations.
func (d *decodeState) pushSeenStructFields(fieldCount int) []bool {
	if d.seenStructFieldsDepth >= len(d.seenStructFieldsStack) {
		// Need to grow the stack
		d.seenStructFieldsStack = append(d.seenStructFieldsStack, make([]bool, fieldCount))
	} else {
		// Reuse existing slice if large enough, otherwise allocate new one
		if cap(d.seenStructFieldsStack[d.seenStructFieldsDepth]) >= fieldCount {
			d.seenStructFieldsStack[d.seenStructFieldsDepth] = d.seenStructFieldsStack[d.seenStructFieldsDepth][:fieldCount]
			clear(d.seenStructFieldsStack[d.seenStructFieldsDepth])
		} else {
			d.seenStructFieldsStack[d.seenStructFieldsDepth] = make([]bool, fieldCount)
		}
	}
	s := d.seenStructFieldsStack[d.seenStructFieldsDepth]
	d.seenStructFieldsDepth++
	return s
}

// popSeenStructFields releases the current seenStructFields slice back to the stack.
func (d *decodeState) popSeenStructFields() {
	d.seenStructFieldsDepth--
}

// enterContainer increments depth and returns an error if max depth exceeded.
func (d *decodeState) enterContainer() error {
	d.containerDepth++
	if d.containerDepth > d.maxAllowedContainerDepth {
		return &MaxDepthError{Depth: d.maxAllowedContainerDepth, Offset: int64(d.offsetIntoData)}
	}
	return nil
}

// exitContainer decrements the container depth.
func (d *decodeState) exitContainer() {
	d.containerDepth--
}

func (d *decodeState) init(data []byte) {
	d.data = data
	d.offsetIntoData = 0
	d.savedError = nil
}

// checkNaNInf returns an error if the value is NaN or Infinity and
// the decoder is not configured to allow them.
func (d *decodeState) checkNaNInf(f float64) error {
	if d.allowNaNInf {
		return nil
	}
	if math.IsNaN(f) {
		return &InvalidValueError{Value: "NaN", Offset: int64(d.offsetIntoData)}
	}
	if math.IsInf(f, 0) {
		return &InvalidValueError{Value: "Infinity", Offset: int64(d.offsetIntoData)}
	}
	return nil
}

func (d *decodeState) saveError(err error) {
	if d.savedError == nil {
		d.savedError = err
	}
}

func (d *decodeState) unmarshal(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &InvalidUnmarshalError{reflect.TypeOf(v)}
	}

	err := d.decodeValue(rv)
	if err != nil {
		return err
	}

	if d.offsetIntoData < len(d.data) {
		return &TrailingDataError{Offset: int64(d.offsetIntoData)}
	}

	return d.savedError
}

// readByte reads a single byte from the input
func (d *decodeState) readByte() (byte, error) {
	if d.offsetIntoData >= len(d.data) {
		return 0, &TruncatedDataError{Expected: 1, Got: 0, Offset: int64(d.offsetIntoData)}
	}
	b := d.data[d.offsetIntoData]
	d.offsetIntoData++
	return b, nil
}

// peekByte peeks at the next byte without consuming it
func (d *decodeState) peekByte() (byte, error) {
	if d.offsetIntoData >= len(d.data) {
		return 0, &TruncatedDataError{Expected: 1, Got: 0, Offset: int64(d.offsetIntoData)}
	}
	return d.data[d.offsetIntoData], nil
}

// decodeValue decodes a BONJSON decodeValue into v
func (d *decodeState) decodeValue(v reflect.Value) error {
	tc, err := d.readByte()
	if err != nil {
		return err
	}

	if !v.IsValid() {
		return d.skipValue(tc)
	}

	// Check for unmarshaler first
	u, ut, pv := dereferenceAndGetUnmarshaler(v, tc == typeNull)
	if u != nil {
		// Need to read the entire value for the unmarshaler
		start := d.offsetIntoData - 1
		if err := d.skipValue(tc); err != nil {
			return err
		}
		return u.UnmarshalBONJSON(d.data[start:d.offsetIntoData])
	}

	switch {
	case tc <= typeSmallIntMax:
		// Small positive integer (0-100)
		return d.storeInt(int64(tc), pv, ut)

	case tc >= typeSmallNegIntMin:
		// Small negative integer (-100 to -1)
		return d.storeInt(int64(int8(tc)), pv, ut)

	case tc >= typeUintBase && tc <= typeUintBase+7:
		// Unsigned integer (1-8 bytes, little-endian)
		n := int(tc&0x07) + 1
		if d.offsetIntoData+n > len(d.data) {
			return &TruncatedDataError{Expected: n, Got: len(d.data) - d.offsetIntoData, Offset: int64(d.offsetIntoData)}
		}
		val := readLittleEndianUint64(d.data[d.offsetIntoData:], n)
		d.offsetIntoData += n
		return d.storeUint(val, pv, ut)

	case tc >= typeSintBase && tc <= typeSintBase+7:
		// Signed integer (1-8 bytes, little-endian)
		n := int(tc&0x07) + 1
		if d.offsetIntoData+n > len(d.data) {
			return &TruncatedDataError{Expected: n, Got: len(d.data) - d.offsetIntoData, Offset: int64(d.offsetIntoData)}
		}
		val := readLittleEndianUint64(d.data[d.offsetIntoData:], n)
		d.offsetIntoData += n
		return d.storeInt(signExtend(val, n), pv, ut)

	case tc == typeFloat16:
		f, err := decodeFloat16(d.data[d.offsetIntoData:])
		if err != nil {
			return err
		}
		d.offsetIntoData += 2
		if err := d.checkNaNInf(f); err != nil {
			return err
		}
		return d.storeFloat(f, pv, ut)

	case tc == typeFloat32:
		f, err := decodeFloat32(d.data[d.offsetIntoData:])
		if err != nil {
			return err
		}
		d.offsetIntoData += 4
		if err := d.checkNaNInf(f); err != nil {
			return err
		}
		return d.storeFloat(f, pv, ut)

	case tc == typeFloat64:
		f, err := decodeFloat64(d.data[d.offsetIntoData:])
		if err != nil {
			return err
		}
		d.offsetIntoData += 8
		if err := d.checkNaNInf(f); err != nil {
			return err
		}
		return d.storeFloat(f, pv, ut)

	case tc == typeBigNumber:
		bn, special, n, err := decodeBigNumber(d.data[d.offsetIntoData:])
		if err != nil {
			return err
		}
		d.offsetIntoData += n
		// Check for special values (NaN, Infinity)
		if special != BigNumNormal {
			if !d.allowNaNInf {
				switch special {
				case BigNumInf:
					return &InvalidValueError{Value: "Infinity", Offset: int64(d.offsetIntoData)}
				case BigNumQNaN:
					return &InvalidValueError{Value: "NaN", Offset: int64(d.offsetIntoData)}
				case BigNumSNaN:
					return &InvalidValueError{Value: "signaling NaN", Offset: int64(d.offsetIntoData)}
				}
			}
			// If allowed, store as special float value
			var f float64
			switch special {
			case BigNumInf:
				if bn.Negative {
					f = math.Inf(-1)
				} else {
					f = math.Inf(1)
				}
			case BigNumQNaN, BigNumSNaN:
				f = math.NaN()
			}
			return d.storeFloat(f, pv, ut)
		}
		// For *big.Int and *big.Float, use the original value v to preserve type info
		// since indirect() returns an invalid pv when TextUnmarshaler is found
		return d.storeBigNumber(bn, v, pv, ut)

	case tc >= typeShortStringBase && tc <= typeShortStringBase+0x0f:
		// Short string
		length := int(tc & 0x0f)
		if d.offsetIntoData+length > len(d.data) {
			return &TruncatedDataError{Expected: length, Got: len(d.data) - d.offsetIntoData, Offset: int64(d.offsetIntoData)}
		}
		s := d.data[d.offsetIntoData : d.offsetIntoData+length]
		d.offsetIntoData += length
		return d.storeString(s, pv, ut)

	case tc == typeLongString:
		s, n, err := decodeLongString(d.data[d.offsetIntoData:], d.maxAllowedChunks, d.maxAllowedStringLength)
		if err != nil {
			return err
		}
		d.offsetIntoData += n
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
		return &InvalidTypeCodeError{TypeCode: tc, Offset: int64(d.offsetIntoData - 1)}
	}
}

// dereferenceAndGetUnmarshaler walks down v allocating pointers as needed, until it gets to a non-pointer.
func dereferenceAndGetUnmarshaler(v reflect.Value, decodingNull bool) (Unmarshaler, encoding.TextUnmarshaler, reflect.Value) {
	haveAddr := false
	v0 := v
	v0Type := v0.Type()
	v0Kind := v0.Kind()

	// Cache type and kind to avoid repeated reflect calls
	vType := v0Type
	vKind := v0Kind

	// Decide if we should take the address of v to check for pointer receiver methods.
	// Only do this for named types (Name() != "") that are addressable.
	if vKind != reflect.Pointer && v.CanAddr() {
		// Fast path: for basic types without methods on either value or pointer receiver,
		// we can skip the Name() check entirely.
		if vType.NumMethod() > 0 || reflect.PointerTo(vType).NumMethod() > 0 {
			// Type has methods, need to check if it's named to take address
			if vType.Name() != "" {
				haveAddr = true
				v = v.Addr()
				vType = v.Type()
				vKind = reflect.Pointer
			}
		}
	}

	for {
		if vKind == reflect.Interface && !v.IsNil() {
			e := v.Elem()
			eType := e.Type()
			eKind := eType.Kind()
			if eKind == reflect.Pointer && !e.IsNil() && (!decodingNull || e.Elem().Type().Kind() == reflect.Pointer) {
				haveAddr = false
				v = e
				vType = eType
				vKind = eKind
				continue
			}
		}

		if vKind != reflect.Pointer {
			break
		}

		if decodingNull && v.CanSet() {
			break
		}

		// Get elem type from the pointer type (safe even if pointer is nil)
		eType := vType.Elem()
		eKind := eType.Kind()

		// Check for interface cycle before getting actual elem value
		if eKind == reflect.Interface {
			e := v.Elem()
			if e.IsValid() && e.Elem().Equal(v) {
				v = e
				vType = eType
				vKind = eKind
				break
			}
		}

		if v.IsNil() {
			v.Set(reflect.New(eType))
		}
		if vType.NumMethod() > 0 && v.CanInterface() {
			if u, ok := v.Interface().(Unmarshaler); ok {
				return u, nil, reflect.Value{}
			}
			if !decodingNull {
				if u, ok := v.Interface().(encoding.TextUnmarshaler); ok {
					return nil, u, reflect.Value{}
				}
			}
		}

		if haveAddr {
			v = v0
			vType = v0Type
			vKind = v0Kind
			haveAddr = false
		} else {
			v = v.Elem()
			vType = eType
			vKind = eKind
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
			// BONJSON preserves integer types natively, unlike JSON which uses float64
			v.Set(reflect.ValueOf(val))
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.offsetIntoData)})
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.OverflowInt(val) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatInt(val, 10), Type: v.Type(), Offset: int64(d.offsetIntoData)})
			return nil
		}
		v.SetInt(val)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if val < 0 {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatInt(val, 10), Type: v.Type(), Offset: int64(d.offsetIntoData)})
			return nil
		}
		if v.OverflowUint(uint64(val)) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatInt(val, 10), Type: v.Type(), Offset: int64(d.offsetIntoData)})
			return nil
		}
		v.SetUint(uint64(val))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(val))
	default:
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.offsetIntoData)})
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
			// BONJSON preserves integer types natively, unlike JSON which uses float64
			v.Set(reflect.ValueOf(val))
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.offsetIntoData)})
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if val > uint64(^uint(0)>>1) || v.OverflowInt(int64(val)) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatUint(val, 10), Type: v.Type(), Offset: int64(d.offsetIntoData)})
			return nil
		}
		v.SetInt(int64(val))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if v.OverflowUint(val) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatUint(val, 10), Type: v.Type(), Offset: int64(d.offsetIntoData)})
			return nil
		}
		v.SetUint(val)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(val))
	default:
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.offsetIntoData)})
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
			v.Set(reflect.ValueOf(val))
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.offsetIntoData)})
	case reflect.Float32, reflect.Float64:
		if v.OverflowFloat(val) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatFloat(val, 'g', -1, 64), Type: v.Type(), Offset: int64(d.offsetIntoData)})
			return nil
		}
		v.SetFloat(val)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i := int64(val)
		if float64(i) != val || v.OverflowInt(i) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatFloat(val, 'g', -1, 64), Type: v.Type(), Offset: int64(d.offsetIntoData)})
			return nil
		}
		v.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := uint64(val)
		if float64(u) != val || v.OverflowUint(u) {
			d.saveError(&UnmarshalTypeError{Value: "number " + strconv.FormatFloat(val, 'g', -1, 64), Type: v.Type(), Offset: int64(d.offsetIntoData)})
			return nil
		}
		v.SetUint(u)
	default:
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.offsetIntoData)})
	}
	return nil
}

func (d *decodeState) storeBigNumber(bn *BigNumber, origV reflect.Value, pv reflect.Value, ut encoding.TextUnmarshaler) error {
	// Check for *big.Int target first - preserve exact integer value
	// Use origV since pv may be invalid when TextUnmarshaler is found
	if origV.IsValid() && origV.Kind() == reflect.Pointer && origV.Type() == bigIntPtrType {
		return d.storeBigNumberToBigInt(bn, origV)
	}
	// Check for *big.Float target - preserve arbitrary precision
	if origV.IsValid() && origV.Kind() == reflect.Pointer && origV.Type() == bigFloatPtrType {
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

	// Build the significand value using the helper function
	sig := readLittleEndianUint64(bn.Significand, len(bn.Significand))

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
	bigEndian := slices.Clone(bn.Significand)
	slices.Reverse(bigEndian)

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
	bigEndian := slices.Clone(bn.Significand)
	slices.Reverse(bigEndian)

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
	processed, err := d.processString(s)
	if err != nil {
		return err
	}

	if ut != nil {
		return ut.UnmarshalText(processed)
	}

	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf(string(processed)))
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "string", Type: v.Type(), Offset: int64(d.offsetIntoData)})
	case reflect.String:
		v.SetString(string(processed))
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			// []byte - base64 decode
			b := make([]byte, base64.StdEncoding.DecodedLen(len(processed)))
			n, err := base64.StdEncoding.Decode(b, processed)
			if err != nil {
				d.saveError(err)
				return nil
			}
			v.SetBytes(b[:n])
			return nil
		}
		d.saveError(&UnmarshalTypeError{Value: "string", Type: v.Type(), Offset: int64(d.offsetIntoData)})
	default:
		d.saveError(&UnmarshalTypeError{Value: "string", Type: v.Type(), Offset: int64(d.offsetIntoData)})
	}
	return nil
}

// processString validates and potentially transforms a string based on the
// configured InvalidUTF8Mode. It also checks for NUL bytes if configured.
// Returns the (potentially modified) string and any error.
func (d *decodeState) processString(s []byte) ([]byte, error) {
	baseOff := d.offsetIntoData - len(s)

	// Fast path: UTF8Ignore skips all validation
	if d.invalidUTF8Mode == UTF8Ignore {
		if !d.allowNUL {
			if zeroIdx := bytes.IndexByte(s, 0); zeroIdx >= 0 {
				return nil, &NullInStringError{Offset: int64(baseOff + zeroIdx)}
			}
		}
		return s, nil
	}

	// Fast path: valid UTF-8, just check for NUL
	if utf8.Valid(s) {
		if !d.allowNUL {
			if zeroIdx := bytes.IndexByte(s, 0); zeroIdx >= 0 {
				return nil, &NullInStringError{Offset: int64(baseOff + zeroIdx)}
			}
		}
		return s, nil
	}

	// Invalid UTF-8 detected - handle based on mode
	switch d.invalidUTF8Mode {
	case UTF8Reject:
		// Find exact position for error reporting
		for i := 0; i < len(s); {
			if s[i] < utf8.RuneSelf {
				i++
				continue
			}
			_, size := utf8.DecodeRune(s[i:])
			if size == 1 {
				return nil, &InvalidUTF8Error{Offset: int64(baseOff + i)}
			}
			i += size
		}
		// Shouldn't reach here, but return original if we do
		return s, nil

	case UTF8Replace:
		return d.replaceInvalidUTF8(s, baseOff)

	case UTF8Delete:
		return d.deleteInvalidUTF8(s, baseOff)

	default:
		// Unknown mode, fall back to reject
		return nil, &InvalidUTF8Error{Offset: int64(baseOff)}
	}
}

// replaceInvalidUTF8 replaces invalid UTF-8 bytes with U+FFFD.
// Also checks for NUL bytes if configured to reject them.
func (d *decodeState) replaceInvalidUTF8(s []byte, baseOff int) ([]byte, error) {
	// Estimate output size: same as input plus some extra for replacements
	// (U+FFFD is 3 bytes, replacing 1 invalid byte)
	out := make([]byte, 0, len(s)+len(s)/4)

	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			// ASCII byte
			if b == 0 && !d.allowNUL {
				return nil, &NullInStringError{Offset: int64(baseOff + i)}
			}
			out = append(out, b)
			i++
			continue
		}

		r, size := utf8.DecodeRune(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid byte - replace with U+FFFD (3 bytes: 0xef 0xbf 0xbd)
			out = append(out, 0xef, 0xbf, 0xbd)
			i++
		} else {
			// Valid multi-byte rune
			out = append(out, s[i:i+size]...)
			i += size
		}
	}

	return out, nil
}

// deleteInvalidUTF8 removes invalid UTF-8 bytes from the string.
// Also checks for NUL bytes if configured to reject them.
func (d *decodeState) deleteInvalidUTF8(s []byte, baseOff int) ([]byte, error) {
	out := make([]byte, 0, len(s))

	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			// ASCII byte
			if b == 0 && !d.allowNUL {
				return nil, &NullInStringError{Offset: int64(baseOff + i)}
			}
			out = append(out, b)
			i++
			continue
		}

		r, size := utf8.DecodeRune(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid byte - skip it (delete)
			i++
		} else {
			// Valid multi-byte rune
			out = append(out, s[i:i+size]...)
			i += size
		}
	}

	return out, nil
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
		d.saveError(&UnmarshalTypeError{Value: "bool", Type: v.Type(), Offset: int64(d.offsetIntoData)})
	case reflect.Bool:
		v.SetBool(val)
	default:
		d.saveError(&UnmarshalTypeError{Value: "bool", Type: v.Type(), Offset: int64(d.offsetIntoData)})
	}
	return nil
}

func (d *decodeState) storeNull(v reflect.Value, _ reflect.Value) error {
	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Map, reflect.Slice:
		v.SetZero()
	}
	return nil
}

func (d *decodeState) decodeArray(v reflect.Value, _ reflect.Value) error {
	if err := d.enterContainer(); err != nil {
		return err
	}
	defer d.exitContainer()

	vType := v.Type()
	vKind := v.Kind()

	// Handle interface{}
	if vKind == reflect.Interface && v.NumMethod() == 0 {
		ai := d.arrayInterface()
		if d.savedError != nil {
			return d.savedError
		}
		v.Set(reflect.ValueOf(ai))
		return nil
	}

	// Check type
	isSlice := vKind == reflect.Slice
	if !isSlice && vKind != reflect.Array {
		d.saveError(&UnmarshalTypeError{Value: "array", Type: vType, Offset: int64(d.offsetIntoData)})
		return d.skipContainer()
	}

	vLen := v.Len()
	i := 0
	for {
		tc, err := d.peekByte()
		if err != nil {
			return err
		}

		if tc == typeContainerEnd {
			d.offsetIntoData++ // consume the end marker
			break
		}

		// Expand slice if necessary
		if isSlice {
			if i >= v.Cap() {
				v.Grow(1)
			}
			if i >= vLen {
				vLen++
				v.SetLen(vLen)
			}
		}

		if i < vLen {
			if err := d.decodeValue(v.Index(i)); err != nil {
				return err
			}
		} else {
			// Ran out of fixed array: skip
			if err := d.decodeValue(reflect.Value{}); err != nil {
				return err
			}
		}
		i++
	}

	if i < vLen {
		if isSlice {
			v.SetLen(i)
		} else {
			for ; i < vLen; i++ {
				v.Index(i).SetZero()
			}
		}
	}
	if i == 0 && isSlice {
		v.Set(reflect.MakeSlice(vType, 0, 0))
	}
	return nil
}

var (
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	bigIntPtrType       = reflect.TypeOf((*big.Int)(nil))
	bigFloatPtrType     = reflect.TypeOf((*big.Float)(nil))
)

func (d *decodeState) decodeObject(v reflect.Value, _ reflect.Value) error {
	if err := d.enterContainer(); err != nil {
		return err
	}
	defer d.exitContainer()

	vKind := v.Kind()

	// Handle interface{}
	if vKind == reflect.Interface && v.NumMethod() == 0 {
		oi := d.objectInterface()
		if d.savedError != nil {
			return d.savedError
		}
		v.Set(reflect.ValueOf(oi))
		return nil
	}

	switch vKind {
	case reflect.Map:
		return d.decodeObjectToMap(v)
	case reflect.Struct:
		return d.decodeObjectToStruct(v)
	default:
		d.saveError(&UnmarshalTypeError{Value: "object", Type: v.Type(), Offset: int64(d.offsetIntoData)})
		return d.skipContainer()
	}
}

func (d *decodeState) decodeObjectToMap(v reflect.Value) error {
	t := v.Type()
	kt := t.Key()

	// Validate key type
	switch kt.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
	default:
		if !reflect.PointerTo(kt).Implements(textUnmarshalerType) {
			d.saveError(&UnmarshalTypeError{Value: "object", Type: t, Offset: int64(d.offsetIntoData)})
			return d.skipContainer()
		}
	}

	if v.IsNil() {
		v.Set(reflect.MakeMap(t))
	}

	elemType := t.Elem()
	mapElem := reflect.New(elemType).Elem()

	for {
		tc, err := d.peekByte()
		if err != nil {
			return err
		}

		if tc == typeContainerEnd {
			d.offsetIntoData++
			break
		}

		// Read key (must be a string)
		keyStart := d.offsetIntoData
		key, err := d.readString()
		if err != nil {
			return err
		}

		// Convert key to appropriate type
		kv, err := d.convertMapKey(kt, key, keyStart)
		if err != nil {
			d.saveError(err)
			// Skip value and continue
			if err := d.decodeValue(reflect.Value{}); err != nil {
				return err
			}
			continue
		}

		// Check for duplicate key
		if kv.IsValid() && v.MapIndex(kv).IsValid() {
			switch d.duplicateKeyMode {
			case DupKeyReject:
				return &DuplicateKeyError{Key: string(key), Offset: int64(keyStart)}
			case DupKeyKeepFirst:
				// Skip this value, keep the existing one
				if err := d.decodeValue(reflect.Value{}); err != nil {
					return err
				}
				continue
			case DupKeyKeepLast:
				// Fall through to decode and replace
			}
		}

		// Reset map element for reuse
		mapElem.SetZero()

		// Read value
		if err := d.decodeValue(mapElem); err != nil {
			return err
		}

		if kv.IsValid() {
			v.SetMapIndex(kv, mapElem)
		}
	}
	return nil
}

func (d *decodeState) convertMapKey(kt reflect.Type, key []byte, keyStart int) (reflect.Value, error) {
	switch kt.Kind() {
	case reflect.String:
		kv := reflect.New(kt).Elem()
		kv.SetString(string(key))
		return kv, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(string(key), 10, 64)
		kv := reflect.New(kt).Elem()
		if err != nil || kv.OverflowInt(n) {
			return reflect.Value{}, &UnmarshalTypeError{Value: "number " + string(key), Type: kt, Offset: int64(keyStart)}
		}
		kv.SetInt(n)
		return kv, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n, err := strconv.ParseUint(string(key), 10, 64)
		kv := reflect.New(kt).Elem()
		if err != nil || kv.OverflowUint(n) {
			return reflect.Value{}, &UnmarshalTypeError{Value: "number " + string(key), Type: kt, Offset: int64(keyStart)}
		}
		kv.SetUint(n)
		return kv, nil
	default:
		// TextUnmarshaler
		kv := reflect.New(kt)
		if err := kv.Interface().(encoding.TextUnmarshaler).UnmarshalText(key); err != nil {
			return reflect.Value{}, err
		}
		return kv.Elem(), nil
	}
}

func (d *decodeState) decodeObjectToStruct(v reflect.Value) error {
	fields := cachedTypeFields(v.Type())
	seenFields := d.pushSeenStructFields(fields.fieldCount)
	defer d.popSeenStructFields()

	for {
		tc, err := d.peekByte()
		if err != nil {
			return err
		}

		if tc == typeContainerEnd {
			d.offsetIntoData++
			break
		}

		// Read key (must be a string)
		keyStart := d.offsetIntoData
		key, err := d.readString()
		if err != nil {
			return err
		}

		// Find field for key and check for duplicates
		fieldValue, skip := d.findStructFieldWithDupCheck(v, &fields, key, keyStart, seenFields)
		if skip {
			// Skip this value (duplicate with KeepFirst mode)
			if err := d.decodeValue(reflect.Value{}); err != nil {
				return err
			}
			continue
		}

		// Read value
		if err := d.decodeValue(fieldValue); err != nil {
			return err
		}
	}
	return nil
}

// findStructFieldWithDupCheck finds the struct field for a key and handles duplicate detection.
// Returns the field value and whether to skip decoding this value.
// If skip is true, the caller should skip decoding this value entirely.
func (d *decodeState) findStructFieldWithDupCheck(v reflect.Value, fields *structFields, key []byte, keyStart int, seenFields []bool) (reflect.Value, bool) {
	f := fields.findByExactName(key)
	if f == nil {
		f = fields.findByFoldedName(key)
	}

	if f == nil {
		if d.disallowUnknownFields {
			d.saveError(fmt.Errorf("bonjson: unknown field %q", key))
		}
		return reflect.Value{}, false
	}

	// Check for duplicate using field's seenIndex
	if seenFields[f.seenIndex] {
		switch d.duplicateKeyMode {
		case DupKeyReject:
			d.saveError(&DuplicateKeyError{Key: string(key), Offset: int64(keyStart)})
			return reflect.Value{}, false
		case DupKeyKeepFirst:
			// Skip this value, keep the existing one
			return reflect.Value{}, true
		case DupKeyKeepLast:
			// Fall through to return field for replacement
		}
	}
	seenFields[f.seenIndex] = true

	subv := v
	for _, ind := range f.index {
		if subv.Kind() == reflect.Pointer {
			if subv.IsNil() {
				if !subv.CanSet() {
					d.saveError(fmt.Errorf("bonjson: cannot set embedded pointer to unexported struct: %v", subv.Type().Elem()))
					return reflect.Value{}, false
				}
				subv.Set(reflect.New(subv.Type().Elem()))
			}
			subv = subv.Elem()
		}
		subv = subv.Field(ind)
	}
	return subv, false
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
		if d.offsetIntoData+length > len(d.data) {
			return nil, &TruncatedDataError{Expected: length, Got: len(d.data) - d.offsetIntoData, Offset: int64(d.offsetIntoData)}
		}
		s := d.data[d.offsetIntoData : d.offsetIntoData+length]
		d.offsetIntoData += length
		return d.processString(s)

	case tc == typeLongString:
		s, n, err := decodeLongString(d.data[d.offsetIntoData:], d.maxAllowedChunks, d.maxAllowedStringLength)
		if err != nil {
			return nil, err
		}
		d.offsetIntoData += n
		return d.processString(s)

	default:
		return nil, &SyntaxError{msg: "expected string", Offset: int64(d.offsetIntoData - 1)}
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
		if d.offsetIntoData+n > len(d.data) {
			return &TruncatedDataError{Expected: n, Got: len(d.data) - d.offsetIntoData, Offset: int64(d.offsetIntoData)}
		}
		d.offsetIntoData += n
		return nil
	case tc >= typeSintBase && tc <= typeSintBase+7:
		n := int(tc&0x07) + 1
		if d.offsetIntoData+n > len(d.data) {
			return &TruncatedDataError{Expected: n, Got: len(d.data) - d.offsetIntoData, Offset: int64(d.offsetIntoData)}
		}
		d.offsetIntoData += n
		return nil
	case tc == typeFloat16:
		if d.offsetIntoData+2 > len(d.data) {
			return &TruncatedDataError{Expected: 2, Got: len(d.data) - d.offsetIntoData, Offset: int64(d.offsetIntoData)}
		}
		d.offsetIntoData += 2
		return nil
	case tc == typeFloat32:
		if d.offsetIntoData+4 > len(d.data) {
			return &TruncatedDataError{Expected: 4, Got: len(d.data) - d.offsetIntoData, Offset: int64(d.offsetIntoData)}
		}
		d.offsetIntoData += 4
		return nil
	case tc == typeFloat64:
		if d.offsetIntoData+8 > len(d.data) {
			return &TruncatedDataError{Expected: 8, Got: len(d.data) - d.offsetIntoData, Offset: int64(d.offsetIntoData)}
		}
		d.offsetIntoData += 8
		return nil
	case tc == typeBigNumber:
		_, _, n, err := decodeBigNumber(d.data[d.offsetIntoData:])
		if err != nil {
			return err
		}
		d.offsetIntoData += n
		return nil
	case tc >= typeShortStringBase && tc <= typeShortStringBase+0x0f:
		length := int(tc & 0x0f)
		if d.offsetIntoData+length > len(d.data) {
			return &TruncatedDataError{Expected: length, Got: len(d.data) - d.offsetIntoData, Offset: int64(d.offsetIntoData)}
		}
		d.offsetIntoData += length
		return nil
	case tc == typeLongString:
		_, n, err := decodeLongString(d.data[d.offsetIntoData:], d.maxAllowedChunks, d.maxAllowedStringLength)
		if err != nil {
			return err
		}
		d.offsetIntoData += n
		return nil
	case tc == typeNull, tc == typeFalse, tc == typeTrue:
		return nil
	case tc == typeArrayStart, tc == typeObjectStart:
		return d.skipContainer()
	default:
		return &InvalidTypeCodeError{TypeCode: tc, Offset: int64(d.offsetIntoData - 1)}
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
			d.offsetIntoData++
			break
		}

		var elem any
		ev := reflect.ValueOf(&elem).Elem()
		if err := d.decodeValue(ev); err != nil {
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
			d.offsetIntoData++
			break
		}

		keyStart := d.offsetIntoData
		key, err := d.readString()
		if err != nil {
			d.saveError(err)
			return m
		}

		keyStr := string(key)
		// Check for duplicate key using the map itself
		if _, exists := m[keyStr]; exists {
			switch d.duplicateKeyMode {
			case DupKeyReject:
				d.saveError(&DuplicateKeyError{Key: keyStr, Offset: int64(keyStart)})
				return m
			case DupKeyKeepFirst:
				// Skip this value, keep the existing one
				if err := d.decodeValue(reflect.Value{}); err != nil {
					d.saveError(err)
					return m
				}
				continue
			case DupKeyKeepLast:
				// Fall through to decode and replace
			}
		}

		var val any
		ev := reflect.ValueOf(&val).Elem()
		if err := d.decodeValue(ev); err != nil {
			d.saveError(err)
			return m
		}
		m[keyStr] = val
	}
	return m
}
