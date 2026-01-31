//
// wire.go
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

// ABOUTME: Low-level binary encoding and decoding for the BONJSON wire format.
// ABOUTME: Handles integers (native sizes), floats, strings (short/long),
// ABOUTME: big numbers (zigzag LEB128), and booleans.

package bonjson

import (
	"encoding/binary"
	"math"
	"math/big"
	"math/bits"
)

// ============================================================================
// Common Helper Functions
// ============================================================================

// readNativeSizeUint64 reads n bytes (1, 2, 4, or 8) from src as a
// little-endian uint64 using direct binary reads for each native size.
// Caller must ensure len(src) >= n.
func readNativeSizeUint64(src []byte, n int) uint64 {
	switch n {
	case 1:
		return uint64(src[0])
	case 2:
		return uint64(binary.LittleEndian.Uint16(src))
	case 4:
		return uint64(binary.LittleEndian.Uint32(src))
	default: // 8
		return binary.LittleEndian.Uint64(src)
	}
}

// signExtendNative sign-extends an n-byte (1, 2, 4, or 8) unsigned value to int64.
func signExtendNative(val uint64, n int) int64 {
	switch n {
	case 1:
		return int64(int8(val))
	case 2:
		return int64(int16(val))
	case 4:
		return int64(int32(val))
	default: // 8
		return int64(val)
	}
}

// nativeSizeIndex returns the size index (0-3) for the smallest native size
// (1, 2, 4, 8 bytes) that can hold the given number of significant bytes.
func nativeSizeIndex(neededBytes int) int {
	switch {
	case neededBytes <= 1:
		return 0
	case neededBytes <= 2:
		return 1
	case neededBytes <= 4:
		return 2
	default:
		return 3
	}
}

// ============================================================================
// Integer Encoding/Decoding
// ============================================================================

// encodeSignedInt encodes a signed integer using the smallest native size.
// Returns the number of bytes written (1 for type code + native size for value).
func encodeSignedInt(dst []byte, v int64) int {
	// Try small int first (type code = value + 100)
	if v >= smallIntMin && v <= smallIntMax {
		dst[0] = byte(v + 100)
		return 1
	}

	// Calculate required bytes using leading sign bits count.
	// For signed integers, we count leading redundant sign bits.
	u := uint64(v)
	signExtended := uint64(v >> 63) // All 1s if negative, all 0s if positive
	effective := u ^ signExtended

	var neededBytes int
	if effective == 0 {
		neededBytes = 1
	} else {
		// +8 instead of +7 ensures room for the sign bit
		neededBytes = (bits.Len64(effective) + 8) / 8
	}

	// Round up to native size
	idx := nativeSizeIndex(neededBytes)
	n := nativeSizes[idx]

	dst[0] = typeSintBase | byte(idx)
	binary.LittleEndian.PutUint64(dst[1:], u)
	return 1 + n
}

// encodeUnsignedInt encodes an unsigned integer using the smallest native size.
// Per BONJSON spec: if both signed and unsigned require the same number of bytes,
// prefer signed.
// Returns the number of bytes written (1 for type code + native size for value).
func encodeUnsignedInt(dst []byte, v uint64) int {
	// Try small int first (0-100, type code = value + 100)
	if v <= 100 {
		dst[0] = byte(v + 100)
		return 1
	}

	// Calculate required bytes for unsigned: ceil(bits / 8)
	unsignedBytes := (bits.Len64(v) + 7) / 8

	// Calculate required bytes for signed: need the high bit to be 0
	signedBytes := (bits.Len64(v) + 8) / 8

	// Round both up to native sizes
	unsignedIdx := nativeSizeIndex(unsignedBytes)
	signedIdx := nativeSizeIndex(signedBytes)

	// Prefer signed when both use the same native size and the value fits
	if signedBytes <= 8 && nativeSizes[signedIdx] <= nativeSizes[unsignedIdx] {
		dst[0] = typeSintBase | byte(signedIdx)
		binary.LittleEndian.PutUint64(dst[1:], v)
		return 1 + nativeSizes[signedIdx]
	}

	dst[0] = typeUintBase | byte(unsignedIdx)
	binary.LittleEndian.PutUint64(dst[1:], v)
	return 1 + nativeSizes[unsignedIdx]
}

// decodeInteger decodes an integer value given its type code.
// Returns the signed and unsigned values, bytes consumed (excluding type code), and any error.
func decodeInteger(src []byte, typeCode byte) (signedVal int64, unsignedVal uint64, n int, err error) {
	switch {
	case typeCode <= typeSmallIntMax:
		// Small integer (-100 to 100): value = type_code - 100
		val := int64(typeCode) - 100
		if val >= 0 {
			return val, uint64(val), 0, nil
		}
		return val, 0, 0, nil

	case typeCode >= typeUintBase && typeCode <= typeUintBase+3:
		// Unsigned integer (native sizes: 1, 2, 4, 8 bytes)
		idx := int(typeCode & 0x03)
		n = nativeSizes[idx]
		if len(src) < n {
			return 0, 0, 0, &TruncatedDataError{Expected: n, Got: len(src), Offset: 0}
		}
		unsignedVal = readNativeSizeUint64(src, n)
		return int64(unsignedVal), unsignedVal, n, nil

	case typeCode >= typeSintBase && typeCode <= typeSintBase+3:
		// Signed integer (native sizes: 1, 2, 4, 8 bytes)
		idx := int(typeCode & 0x03)
		n = nativeSizes[idx]
		if len(src) < n {
			return 0, 0, 0, &TruncatedDataError{Expected: n, Got: len(src), Offset: 0}
		}
		unsignedVal = readNativeSizeUint64(src, n)
		signedVal = signExtendNative(unsignedVal, n)
		return signedVal, unsignedVal, n, nil

	default:
		return 0, 0, 0, &InvalidTypeCodeError{TypeCode: typeCode, Offset: 0}
	}
}

// ============================================================================
// Float Encoding/Decoding
// ============================================================================

// encodeFloat32 encodes a 32-bit float. Returns bytes written.
func encodeFloat32(dst []byte, v float32) int {
	dst[0] = typeFloat32
	binary.LittleEndian.PutUint32(dst[1:], math.Float32bits(v))
	return 5
}

// encodeFloat64 encodes a 64-bit float. Returns bytes written.
func encodeFloat64(dst []byte, v float64) int {
	dst[0] = typeFloat64
	binary.LittleEndian.PutUint64(dst[1:], math.Float64bits(v))
	return 9
}

// decodeFloat32 decodes a 32-bit float.
func decodeFloat32(src []byte) (float64, error) {
	if len(src) < 4 {
		return 0, &TruncatedDataError{Expected: 4, Got: len(src), Offset: 0}
	}
	bits := binary.LittleEndian.Uint32(src)
	f := math.Float32frombits(bits)
	return float64(f), nil
}

// decodeFloat64 decodes a 64-bit float.
func decodeFloat64(src []byte) (float64, error) {
	if len(src) < 8 {
		return 0, &TruncatedDataError{Expected: 8, Got: len(src), Offset: 0}
	}
	bits := binary.LittleEndian.Uint64(src)
	f := math.Float64frombits(bits)
	return f, nil
}

// ============================================================================
// String Encoding/Decoding
// ============================================================================

// encodeShortString encodes a short string (0-15 bytes). Returns bytes written.
func encodeShortString(dst []byte, s string) int {
	n := len(s)
	dst[0] = typeShortStringBase | byte(n)
	copy(dst[1:], s)
	return 1 + n
}

// encodeLongString encodes a long string (0xFF + data + 0xFF). Returns bytes written.
func encodeLongString(dst []byte, s string) int {
	dst[0] = typeLongString
	copy(dst[1:], s)
	dst[1+len(s)] = typeLongString
	return 2 + len(s)
}

// encodeString encodes a string using the most compact representation.
// Returns the number of bytes written.
func encodeString(dst []byte, s string) int {
	if len(s) <= maxShortStringLen {
		return encodeShortString(dst, s)
	}
	return encodeLongString(dst, s)
}

// decodeLongString decodes a 0xFF-terminated long string from src.
// src should point to the data after the opening 0xFF type code.
// Returns the string bytes, bytes consumed, and any error.
func decodeLongString(src []byte, maxLength int64) ([]byte, int, error) {
	// Scan for the terminating 0xFF byte.
	// This is safe because 0xFF cannot appear in valid UTF-8.
	for i := 0; i < len(src); i++ {
		if src[i] == typeLongString {
			// Check max string length
			if maxLength > 0 && int64(i) > maxLength {
				return nil, 0, &MaxStringLengthError{Length: int64(i), Max: maxLength, Offset: 0}
			}
			return src[:i], i + 1, nil // +1 to consume the terminating 0xFF
		}
	}
	// No terminating 0xFF found
	return nil, 0, &TruncatedDataError{Expected: 1, Got: 0, Offset: int64(len(src))}
}

// ============================================================================
// Zigzag LEB128 Encoding/Decoding
// ============================================================================

// zigzagEncode converts a signed int64 to an unsigned uint64 using zigzag encoding.
// Mapping: 0→0, -1→1, 1→2, -2→3, 2→4, ...
func zigzagEncode(v int64) uint64 {
	return uint64((v << 1) ^ (v >> 63))
}

// zigzagDecode converts an unsigned uint64 back to a signed int64.
func zigzagDecode(v uint64) int64 {
	return int64(v>>1) ^ -int64(v&1)
}

// zigzagEncodeBigInt converts a signed *big.Int to an unsigned *big.Int using zigzag encoding.
func zigzagEncodeBigInt(v *big.Int) *big.Int {
	if v.Sign() >= 0 {
		// v >= 0: result = v * 2
		result := new(big.Int).Lsh(v, 1)
		return result
	}
	// v < 0: result = -v * 2 - 1
	result := new(big.Int).Neg(v)
	result.Lsh(result, 1)
	result.Sub(result, big.NewInt(1))
	return result
}

// zigzagDecodeBigInt converts an unsigned *big.Int back to a signed *big.Int.
func zigzagDecodeBigInt(v *big.Int) *big.Int {
	// Check if odd (v & 1)
	isOdd := v.Bit(0) == 1
	result := new(big.Int).Rsh(v, 1)
	if isOdd {
		// result = -(result + 1)
		result.Add(result, big.NewInt(1))
		result.Neg(result)
	}
	return result
}

// encodeLEB128 encodes an unsigned uint64 as LEB128 into dst.
// Returns the number of bytes written.
func encodeLEB128(dst []byte, v uint64) int {
	i := 0
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80 // more bytes follow
		}
		dst[i] = b
		i++
		if v == 0 {
			break
		}
	}
	return i
}

// encodeLEB128BigInt encodes an unsigned *big.Int as LEB128 into dst.
// Returns the number of bytes written.
func encodeLEB128BigInt(dst []byte, v *big.Int) int {
	if v.Sign() == 0 {
		dst[0] = 0
		return 1
	}
	// Work on a copy to avoid modifying the original
	tmp := new(big.Int).Set(v)
	i := 0
	for tmp.Sign() > 0 {
		b := byte(tmp.Uint64() & 0x7F)
		tmp.Rsh(tmp, 7)
		if tmp.Sign() > 0 {
			b |= 0x80
		}
		dst[i] = b
		i++
	}
	return i
}

// decodeLEB128 decodes an unsigned LEB128 value from src.
// Returns the value, bytes consumed, and any error.
func decodeLEB128(src []byte) (uint64, int, error) {
	var result uint64
	var shift uint
	for i := 0; i < len(src); i++ {
		b := src[i]
		result |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, i + 1, nil
		}
		shift += 7
		if shift >= 64 {
			// Overflow for uint64 - need big.Int
			return 0, 0, &ValueRangeError{Value: "LEB128 overflow", Offset: int64(i)}
		}
	}
	return 0, 0, &TruncatedDataError{Expected: 1, Got: 0, Offset: int64(len(src))}
}

// decodeLEB128BigInt decodes an unsigned LEB128 value from src as a *big.Int.
// Returns the value, bytes consumed, and any error.
func decodeLEB128BigInt(src []byte) (*big.Int, int, error) {
	result := new(big.Int)
	shift := uint(0)
	for i := 0; i < len(src); i++ {
		b := src[i]
		chunk := new(big.Int).SetUint64(uint64(b & 0x7F))
		chunk.Lsh(chunk, shift)
		result.Or(result, chunk)
		if b&0x80 == 0 {
			return result, i + 1, nil
		}
		shift += 7
	}
	return nil, 0, &TruncatedDataError{Expected: 1, Got: 0, Offset: int64(len(src))}
}

// ============================================================================
// Big Number Encoding/Decoding
// ============================================================================

// BigNumber represents a BONJSON big number: significand × 10^exponent.
// In the Phase 2 format, both values are encoded as zigzag LEB128.
type BigNumber struct {
	Significand *big.Int // Signed significand
	Exponent    *big.Int // Base-10 exponent
}

// BigNumberSpecial indicates a special BigNumber value (not used in Phase 2).
// Phase 2 bignum format has no special value encoding for NaN/Infinity.
type BigNumberSpecial int

const (
	BigNumNormal BigNumberSpecial = iota
)

// encodeBigNumber encodes a big number as 0xCA + zigzag_leb128(exponent) + zigzag_leb128(significand).
// Returns the number of bytes written.
func encodeBigNumber(dst []byte, bn *BigNumber) int {
	dst[0] = typeBigNumber
	offset := 1

	// Encode exponent as zigzag LEB128
	if bn.Exponent == nil || bn.Exponent.IsInt64() {
		var exp int64
		if bn.Exponent != nil {
			exp = bn.Exponent.Int64()
		}
		offset += encodeLEB128(dst[offset:], zigzagEncode(exp))
	} else {
		zz := zigzagEncodeBigInt(bn.Exponent)
		offset += encodeLEB128BigInt(dst[offset:], zz)
	}

	// Encode significand as zigzag LEB128
	if bn.Significand == nil || (bn.Significand.IsInt64() && bn.Significand.Int64() >= math.MinInt64) {
		var sig int64
		if bn.Significand != nil && bn.Significand.IsInt64() {
			sig = bn.Significand.Int64()
		}
		offset += encodeLEB128(dst[offset:], zigzagEncode(sig))
	} else {
		zz := zigzagEncodeBigInt(bn.Significand)
		offset += encodeLEB128BigInt(dst[offset:], zz)
	}

	return offset
}

// decodeBigNumber decodes a big number from src (after the 0xCA type code).
// Returns the BigNumber, a special value indicator (always BigNumNormal in Phase 2),
// bytes consumed, and any error.
func decodeBigNumber(src []byte) (*BigNumber, BigNumberSpecial, int, error) {
	offset := 0

	// Decode exponent (zigzag LEB128)
	expUnsigned, n, err := decodeLEB128BigInt(src[offset:])
	if err != nil {
		return nil, BigNumNormal, 0, err
	}
	offset += n
	exponent := zigzagDecodeBigInt(expUnsigned)

	// Decode significand (zigzag LEB128)
	sigUnsigned, n, err := decodeLEB128BigInt(src[offset:])
	if err != nil {
		return nil, BigNumNormal, 0, err
	}
	offset += n
	significand := zigzagDecodeBigInt(sigUnsigned)

	return &BigNumber{
		Significand: significand,
		Exponent:    exponent,
	}, BigNumNormal, offset, nil
}

// ============================================================================
// Boolean Encoding
// ============================================================================

func encodeBool(dst []byte, v bool) int {
	if v {
		dst[0] = typeTrue
	} else {
		dst[0] = typeFalse
	}
	return 1
}

// ============================================================================
// Float Optimization Helpers
// ============================================================================

// canUseFloat32 checks if a float64 can be represented as float32 without loss.
func canUseFloat32(v float64) bool {
	if v == 0 {
		return true
	}
	f32 := float32(v)
	return float64(f32) == v
}

// canUseInteger checks if a float64 is actually an integer value.
// Returns false for negative zero to preserve the sign bit.
func canUseInteger(v float64) (int64, bool) {
	if v != v || math.IsInf(v, 0) { // NaN or Inf
		return 0, false
	}
	// Negative zero must be encoded as float to preserve sign
	if v == 0 && math.Signbit(v) {
		return 0, false
	}
	i := int64(v)
	if float64(i) == v {
		return i, true
	}
	return 0, false
}

// encodeNumber encodes a numeric value using the most compact representation.
// Returns the number of bytes written.
func encodeNumber(dst []byte, v float64) (int, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, &UnsupportedValueError{Str: "NaN and Inf not supported in BONJSON"}
	}

	// Try integer encoding first
	if i, ok := canUseInteger(v); ok {
		if i >= 0 {
			return encodeUnsignedInt(dst, uint64(i)), nil
		}
		return encodeSignedInt(dst, i), nil
	}

	// Try float32
	if canUseFloat32(v) {
		return encodeFloat32(dst, float32(v)), nil
	}

	// Fall back to float64
	return encodeFloat64(dst, v), nil
}
