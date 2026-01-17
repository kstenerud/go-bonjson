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

package bonjson

import (
	"encoding/binary"
	"math"
	"math/bits"
	"unicode/utf8"
)

// wire.go contains low-level binary encoding and decoding functions for BONJSON.
// These functions operate directly on byte slices without allocations.

// ============================================================================
// Common Helper Functions
// ============================================================================

// readLittleEndianUint64 reads n bytes (1-8) from src as a little-endian uint64.
// For aligned sizes (1, 2, 4, 8), uses direct binary.LittleEndian reads.
// For unaligned sizes (3, 5, 6, 7), copies to a temp buffer first.
// Caller must ensure len(src) >= n.
func readLittleEndianUint64(src []byte, n int) uint64 {
	switch n {
	case 1:
		return uint64(src[0])
	case 2:
		return uint64(binary.LittleEndian.Uint16(src))
	case 4:
		return uint64(binary.LittleEndian.Uint32(src))
	case 8:
		return binary.LittleEndian.Uint64(src)
	default: // 3, 5, 6, 7 bytes
		var buf [8]byte
		copy(buf[:], src[:n])
		return binary.LittleEndian.Uint64(buf[:])
	}
}

// signExtend sign-extends an n-byte unsigned value to int64.
// For n < 8, if the sign bit is set, the upper bits are filled with 1s.
func signExtend(val uint64, n int) int64 {
	if n < 8 {
		signBit := uint64(1) << (n*8 - 1)
		if val&signBit != 0 {
			mask := ^uint64(0) << (n * 8)
			val |= mask
		}
	}
	return int64(val)
}

// ============================================================================
// Length Field Encoding/Decoding
// ============================================================================

// encodeLengthField encodes a length value with continuation bit into dst.
// Returns the number of bytes written.
// The payload format is: (length << 1) | continuation_bit
func encodeLengthField(dst []byte, length uint64, continuation bool) int {
	payload := length << 1
	if continuation {
		payload |= 1
	}
	return encodeLengthPayload(dst, payload)
}

// encodeLengthPayload encodes a length field payload into dst.
// Returns the number of bytes written.
// The length field uses trailing 1s terminated by a 0 bit, allowing length 0
// with continuation 0 to encode as byte 0x00 (enabling zero-copy NUL-termination).
func encodeLengthPayload(dst []byte, payload uint64) int {
	// Fast path for small payloads (very common case)
	// Payload fits in 7 bits -> 1 byte encoding
	if payload <= 0x7f {
		dst[0] = byte(payload << 1)
		return 1
	}

	// Calculate extra bytes needed: (significant_bits - 1) / 7
	// Using bits.Len64 which compiles to a single CPU instruction on most architectures
	sigBits := bits.Len64(payload)
	extraBytes := (sigBits - 1) / 7

	if extraBytes < 8 {
		// Encode: shift payload left by (extraBytes+1), then set trailing 1 bits
		// The terminating 0 bit is already in place from the shift
		count := extraBytes + 1
		encoded := (payload << count) | ((1 << extraBytes) - 1)

		// Write as little-endian
		// Use binary.LittleEndian.PutUint64 for efficiency - the compiler
		// optimizes this to a single store on little-endian machines
		binary.LittleEndian.PutUint64(dst, encoded)
		return count
	}

	// 9-byte encoding (header byte 0xff)
	dst[0] = 0xff
	binary.LittleEndian.PutUint64(dst[1:], payload)
	return 9
}

// decodeLengthField decodes a length field from src.
// Returns the length, continuation bit, bytes consumed, and any error.
func decodeLengthField(src []byte) (length uint64, continuation bool, n int, err error) {
	payload, n, err := decodeLengthPayload(src)
	if err != nil {
		return 0, false, 0, err
	}
	continuation = (payload & 1) != 0
	length = payload >> 1
	return length, continuation, n, nil
}

// decodeLengthPayload decodes a length field payload from src.
// Returns the payload value, bytes consumed, and any error.
// The length field uses trailing 1s terminated by a 0 bit.
// Returns NonCanonicalLengthError if the encoding uses more bytes than necessary.
func decodeLengthPayload(src []byte) (payload uint64, n int, err error) {
	if len(src) == 0 {
		return 0, 0, &TruncatedDataError{Expected: 1, Got: 0, Offset: 0}
	}

	header := src[0]
	if header == 0xff {
		// 9-byte encoding
		if len(src) < 9 {
			return 0, 0, &TruncatedDataError{Expected: 9, Got: len(src), Offset: 0}
		}
		payload = binary.LittleEndian.Uint64(src[1:9])
		// Check for non-canonical: 9-byte encoding is only needed if payload > max for 8 bytes
		// Max payload for 8 bytes (count=8) is (1 << 56) - 1
		if payload <= (1<<56)-1 {
			return 0, 0, &NonCanonicalLengthError{Offset: 0}
		}
		return payload, 9, nil
	}

	// Invert header, then count trailing zeros + 1 gives us the byte count
	// bits.TrailingZeros8 compiles to a single CPU instruction on most architectures
	count := bits.TrailingZeros8(^header) + 1
	if len(src) < count {
		return 0, 0, &TruncatedDataError{Expected: count, Got: len(src), Offset: 0}
	}

	payload = readLittleEndianUint64(src, count) >> count

	// Check for non-canonical encoding: payload should require all 'count' bytes
	// For count > 1, verify payload > max value for (count-1) bytes
	// Max payload for (count-1) bytes = (1 << (7*(count-1))) - 1
	if count > 1 {
		maxForFewerBytes := uint64((1 << (7 * (count - 1))) - 1)
		if payload <= maxForFewerBytes {
			return 0, 0, &NonCanonicalLengthError{Offset: 0}
		}
	}

	return payload, count, nil
}

// ============================================================================
// Integer Encoding/Decoding
// ============================================================================

// encodeSignedInt encodes a signed integer using the minimum bytes needed.
// Returns the number of bytes written (1 for type code + n for value).
func encodeSignedInt(dst []byte, v int64) int {
	// Try small int first
	if v >= -100 && v <= 100 {
		dst[0] = byte(int8(v))
		return 1
	}

	// Calculate required bytes using leading sign bits count
	// For signed integers, we count leading redundant sign bits
	// Use xor with sign-extended value to find significant bits
	u := uint64(v)
	signExtended := uint64(v >> 63) // All 1s if negative, all 0s if positive
	effective := u ^ signExtended   // For negative: inverts bits; for positive: same

	// bits.Len64 gives position of highest set bit (1-64), or 0 if value is 0
	// We need ceiling division by 8, plus one extra bit for the sign
	// Using + 8 instead of + 7 ensures we have room for the sign bit
	var n int
	if effective == 0 {
		n = 1
	} else {
		n = (bits.Len64(effective) + 8) / 8
	}

	dst[0] = typeSintBase | byte(n-1)
	// Write as little-endian using single store
	binary.LittleEndian.PutUint64(dst[1:], u)
	return 1 + n
}

// encodeUnsignedInt encodes an unsigned integer using the minimum bytes needed.
// Returns the number of bytes written (1 for type code + n for value).
func encodeUnsignedInt(dst []byte, v uint64) int {
	// Try small int first (0-100)
	if v <= 100 {
		dst[0] = byte(v)
		return 1
	}

	// Calculate required bytes: (position of highest bit + 7) / 8
	// bits.Len64 returns bit length (1-64), divide by 8 rounding up
	n := (bits.Len64(v) + 7) / 8

	dst[0] = typeUintBase | byte(n-1)
	// Write as little-endian using single store
	binary.LittleEndian.PutUint64(dst[1:], v)
	return 1 + n
}

// decodeInteger decodes an integer value given its type code.
// Returns the value as int64 and uint64, bytes consumed (excluding type code), and any error.
func decodeInteger(src []byte, typeCode byte) (signedVal int64, unsignedVal uint64, n int, err error) {
	switch {
	case typeCode <= typeSmallIntMax:
		// Small positive integer (0-100)
		return int64(typeCode), uint64(typeCode), 0, nil

	case typeCode >= typeSmallNegIntMin:
		// Small negative integer (-100 to -1)
		return int64(int8(typeCode)), 0, 0, nil

	case typeCode >= typeUintBase && typeCode <= typeUintBase+7:
		// Unsigned integer
		n = int(typeCode&0x07) + 1
		if len(src) < n {
			return 0, 0, 0, &TruncatedDataError{Expected: n, Got: len(src), Offset: 0}
		}
		unsignedVal = readLittleEndianUint64(src, n)
		return int64(unsignedVal), unsignedVal, n, nil

	case typeCode >= typeSintBase && typeCode <= typeSintBase+7:
		// Signed integer
		n = int(typeCode&0x07) + 1
		if len(src) < n {
			return 0, 0, 0, &TruncatedDataError{Expected: n, Got: len(src), Offset: 0}
		}
		unsignedVal = readLittleEndianUint64(src, n)
		signedVal = signExtend(unsignedVal, n)
		return signedVal, unsignedVal, n, nil

	default:
		return 0, 0, 0, &InvalidTypeCodeError{TypeCode: typeCode, Offset: 0}
	}
}

// ============================================================================
// Float Encoding/Decoding
// ============================================================================

// encodeFloat32 encodes a 32-bit float.
// Returns the number of bytes written.
func encodeFloat32(dst []byte, v float32) int {
	dst[0] = typeFloat32
	binary.LittleEndian.PutUint32(dst[1:], math.Float32bits(v))
	return 5
}

// encodeFloat64 encodes a 64-bit float.
// Returns the number of bytes written.
func encodeFloat64(dst []byte, v float64) int {
	dst[0] = typeFloat64
	binary.LittleEndian.PutUint64(dst[1:], math.Float64bits(v))
	return 9
}

// encodeFloat16 encodes a bfloat16 value.
// Returns the number of bytes written.
func encodeFloat16(dst []byte, v float32) int {
	// bfloat16 is the upper 16 bits of a float32
	bits := math.Float32bits(v)
	bf16 := uint16(bits >> 16)
	dst[0] = typeFloat16
	binary.LittleEndian.PutUint16(dst[1:], bf16)
	return 3
}

// decodeFloat16 decodes a bfloat16 value.
// Note: NaN/Infinity checking is done by the caller based on configuration.
func decodeFloat16(src []byte) (float64, error) {
	if len(src) < 2 {
		return 0, &TruncatedDataError{Expected: 2, Got: len(src), Offset: 0}
	}
	bf16 := binary.LittleEndian.Uint16(src)
	// Expand bfloat16 to float32 by shifting to upper 16 bits
	bits := uint32(bf16) << 16
	f := math.Float32frombits(bits)
	return float64(f), nil
}

// decodeFloat32 decodes a 32-bit float.
// Note: NaN/Infinity checking is done by the caller based on configuration.
func decodeFloat32(src []byte) (float64, error) {
	if len(src) < 4 {
		return 0, &TruncatedDataError{Expected: 4, Got: len(src), Offset: 0}
	}
	bits := binary.LittleEndian.Uint32(src)
	f := math.Float32frombits(bits)
	return float64(f), nil
}

// decodeFloat64 decodes a 64-bit float.
// Note: NaN/Infinity checking is done by the caller based on configuration.
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

// encodeShortString encodes a short string (0-15 bytes).
// Returns the number of bytes written.
func encodeShortString(dst []byte, s string) int {
	n := len(s)
	dst[0] = typeShortStringBase | byte(n)
	copy(dst[1:], s)
	return 1 + n
}

// encodeLongString encodes a long string.
// Returns the number of bytes written.
func encodeLongString(dst []byte, s string) int {
	dst[0] = typeLongString
	n := 1
	// Single chunk, no continuation
	n += encodeLengthField(dst[1:], uint64(len(s)), false)
	copy(dst[n:], s)
	return n + len(s)
}

// encodeString encodes a string using the most compact representation.
// Returns the number of bytes written.
func encodeString(dst []byte, s string) int {
	if len(s) <= maxShortStringLen {
		return encodeShortString(dst, s)
	}
	return encodeLongString(dst, s)
}

// decodeLongString decodes a long string (potentially chunked).
// Returns the string bytes, bytes consumed, and any error.
// If maxChunks > 0, returns TooManyChunksError if chunk count exceeds the limit.
func decodeLongString(src []byte, maxChunks int, maxLength int64) ([]byte, int, error) {
	var result []byte
	offset := 0
	totalLength := int64(0)
	chunkCount := 0

	for {
		// Safety check: reject zero-length chunks with continuation bit set.
		// This is byte 0x02 (length=0, continuation=1) and serves no valid purpose.
		if len(src) > offset && src[offset] == 0x02 {
			return nil, 0, &EmptyChunkContinuationError{Offset: int64(offset)}
		}

		length, continuation, n, err := decodeLengthField(src[offset:])
		if err != nil {
			return nil, 0, err
		}
		offset += n
		chunkCount++

		if maxChunks > 0 && chunkCount > maxChunks {
			return nil, 0, &TooManyChunksError{Count: chunkCount, Max: maxChunks, Offset: int64(offset)}
		}

		totalLength += int64(length)
		if maxLength > 0 && totalLength > maxLength {
			return nil, 0, &MaxStringLengthError{Length: totalLength, Max: maxLength, Offset: int64(offset)}
		}

		if offset+int(length) > len(src) {
			return nil, 0, &TruncatedDataError{Expected: int(length), Got: len(src) - offset, Offset: int64(offset)}
		}

		chunk := src[offset : offset+int(length)]
		offset += int(length)

		// Validate UTF-8 for each chunk
		if !utf8.Valid(chunk) {
			return nil, 0, &InvalidUTF8Error{Offset: int64(offset - int(length))}
		}

		if result == nil && !continuation {
			// Single chunk, no allocation needed - just return the slice
			return chunk, offset, nil
		}

		// Multiple chunks - need to allocate
		if result == nil {
			result = make([]byte, 0, totalLength)
		}
		result = append(result, chunk...)

		if !continuation {
			break
		}
	}

	return result, offset, nil
}

// ============================================================================
// Big Number Encoding/Decoding
// ============================================================================

// BigNumber represents a BONJSON big number.
type BigNumber struct {
	Significand []byte // Unsigned integer bytes (little-endian)
	Exponent    int32  // Base-10 exponent
	Negative    bool   // Sign of significand
}

// encodeBigNumber encodes a big number.
// Returns the number of bytes written.
func encodeBigNumber(dst []byte, bn *BigNumber) int {
	dst[0] = typeBigNumber

	sigLen := len(bn.Significand)

	// Determine exponent bytes needed
	var expLen int
	exp := bn.Exponent
	switch {
	case exp == 0:
		expLen = 0
	case exp >= -128 && exp <= 127:
		expLen = 1
	case exp >= -32768 && exp <= 32767:
		expLen = 2
	default:
		expLen = 3
	}

	// Build header byte: [sig_len:5][exp_len:2][negative:1]
	header := byte(sigLen<<3) | byte(expLen<<1)
	if bn.Negative {
		header |= 1
	}
	dst[1] = header

	offset := 2

	// Write exponent bytes using binary.LittleEndian for efficiency
	if expLen > 0 {
		binary.LittleEndian.PutUint32(dst[offset:], uint32(exp))
		offset += expLen
	}

	// Write significand bytes
	copy(dst[offset:], bn.Significand)
	offset += sigLen

	return offset
}

// BigNumberSpecial indicates a special BigNumber value (infinity, NaN).
type BigNumberSpecial int

const (
	BigNumNormal BigNumberSpecial = iota
	BigNumInf
	BigNumQNaN
	BigNumSNaN
)

// decodeBigNumber decodes a big number.
// Returns the BigNumber, special value indicator, bytes consumed (excluding type code), and any error.
// The caller should check the special value and handle accordingly based on configuration.
func decodeBigNumber(src []byte) (*BigNumber, BigNumberSpecial, int, error) {
	if len(src) < 1 {
		return nil, BigNumNormal, 0, &TruncatedDataError{Expected: 1, Got: 0, Offset: 0}
	}

	header := src[0]
	negative := (header & 1) != 0
	expLen := int((header >> 1) & 0x03)
	sigLen := int((header >> 3) & 0x1f)

	offset := 1

	// Handle special cases when sigLen is 0
	if sigLen == 0 {
		switch expLen {
		case 0:
			// Zero
			return &BigNumber{Negative: negative}, BigNumNormal, offset, nil
		case 1:
			// Infinity
			return &BigNumber{Negative: negative}, BigNumInf, offset, nil
		case 2:
			// Quiet NaN
			return &BigNumber{Negative: negative}, BigNumQNaN, offset, nil
		case 3:
			// Signaling NaN
			return &BigNumber{Negative: negative}, BigNumSNaN, offset, nil
		}
	}

	// Read exponent
	var exp int32
	if expLen > 0 {
		if offset+expLen > len(src) {
			return nil, BigNumNormal, 0, &TruncatedDataError{Expected: expLen, Got: len(src) - offset, Offset: int64(offset)}
		}
		// Read bytes into a 4-byte buffer (zero-extended)
		var buf [4]byte
		copy(buf[:], src[offset:offset+expLen])
		expVal := int64(binary.LittleEndian.Uint32(buf[:]))
		// Sign extend based on actual byte length
		if expLen < 4 {
			signBit := int64(1) << (expLen*8 - 1)
			if expVal&signBit != 0 {
				mask := ^int64(0) << (expLen * 8)
				expVal |= mask
			}
		}
		exp = int32(expVal)
		offset += expLen
	}

	// Read significand
	if offset+sigLen > len(src) {
		return nil, BigNumNormal, 0, &TruncatedDataError{Expected: sigLen, Got: len(src) - offset, Offset: int64(offset)}
	}
	significand := make([]byte, sigLen)
	copy(significand, src[offset:offset+sigLen])
	offset += sigLen

	return &BigNumber{
		Significand: significand,
		Exponent:    exp,
		Negative:    negative,
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

// canUseBFloat16 checks if a float64 can be represented as bfloat16 without loss.
func canUseBFloat16(v float64) bool {
	if v == 0 {
		return true
	}
	f32 := float32(v)
	if float64(f32) != v {
		return false
	}
	// Check if the lower 16 bits of the float32 mantissa are zero
	bits := math.Float32bits(f32)
	return (bits & 0xFFFF) == 0
}

// canUseFloat32 checks if a float64 can be represented as float32 without loss.
func canUseFloat32(v float64) bool {
	if v == 0 {
		return true
	}
	f32 := float32(v)
	return float64(f32) == v
}

// canUseInteger checks if a float64 is actually an integer value.
func canUseInteger(v float64) (int64, bool) {
	if v != v || math.IsInf(v, 0) { // NaN or Inf
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

	// Try bfloat16
	if canUseBFloat16(v) {
		return encodeFloat16(dst, float32(v)), nil
	}

	// Try float32
	if canUseFloat32(v) {
		return encodeFloat32(dst, float32(v)), nil
	}

	// Fall back to float64
	return encodeFloat64(dst, v), nil
}
