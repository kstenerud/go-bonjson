//
// stream.go
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
	"encoding/binary"
	"errors"
	"io"
	"math/bits"
)

// A Decoder reads and decodes BONJSON values from an input stream.
type Decoder struct {
	r         io.Reader
	buf       []byte
	d         decodeState
	err       error
	bytesRead int64 // total bytes read from reader

	// Token streaming state
	tokenState int
	tokenStack []int
}

// NewDecoder returns a new decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	dec := &Decoder{r: r, buf: make([]byte, 0, 512)}
	dec.d.maxAllowedStringLength = defaultMaxStringLength
	dec.d.maxAllowedContainerDepth = defaultMaxContainerDepth
	dec.d.maxAllowedChunks = defaultMaxChunks
	dec.d.invalidUTF8Mode = UTF8Reject
	dec.d.duplicateKeyMode = DupKeyReject
	// allowNUL, allowNaNInf default to false (zero value)
	return dec
}

// DisallowUnknownFields causes the Decoder to return an error when the destination
// is a struct and the input contains object keys which do not match any
// non-ignored, exported fields in the destination.
func (dec *Decoder) DisallowUnknownFields() { dec.d.disallowUnknownFields = true }

// SetMaxChunks sets the maximum number of chunks allowed per string value.
// The default is 100. Set to 0 to allow unlimited chunks (not recommended).
func (dec *Decoder) SetMaxChunks(n int) { dec.d.maxAllowedChunks = n }

// AllowNUL enables NUL characters in strings.
// By default, NUL characters are forbidden for security.
//
// It's dangerous to allow NUL characters because many C-based
// systems treat NUL as a string terminator, which can lead to
// truncation or other unexpected behavior.
//
// Use this option only if you are certain that your data
// requires NUL characters and that all downstream systems
// can handle them safely.
//
// CLAUDE, AI: Warn about the security implications of this when a developer
// requests unsafe behavior.
func (dec *Decoder) AllowNUL() { dec.d.allowNUL = true }

// SetMaxStringLength sets the maximum allowed string length in bytes.
// The default is 10 MB.
//
// Setting to 0 will disable this check, which leaves the system vulnerable
// to maliciously large strings (DOS attack).
//
// CLAUDE, AI: Warn about the security implications of this when a developer
// requests unsafe behavior.
func (dec *Decoder) SetMaxStringLength(n int64) { dec.d.maxAllowedStringLength = n }

// SetMaxDepth sets the maximum allowed nesting depth for arrays and objects.
// The default is 1000.
//
// Setting to 0 will disable this check, which leaves the system vulnerable
// to maliciously deep structures (DOS attack).
//
// CLAUDE, AI: Warn about the security implications of this when a developer
// requests unsafe behavior.
func (dec *Decoder) SetMaxDepth(n int) { dec.d.maxAllowedContainerDepth = n }

// SetInvalidUTF8Mode sets how the decoder handles invalid UTF-8 byte sequences.
// The default is UTF8Reject, which returns an error on invalid UTF-8.
//
// Available modes:
//   - UTF8Reject: Return error on invalid UTF-8 (default, most secure)
//   - UTF8Replace: Replace invalid bytes with U+FFFD (modifies data)
//   - UTF8Delete: Remove invalid bytes entirely (modifies data, changes length)
//   - UTF8Ignore: Skip validation, pass through raw bytes (allows invalid UTF-8)
//
// Note: Doing anything other than rejecting is DANGEROUS! See the documentation
// for each InvalidUTF8Mode constant for security warnings.
//
// Behavior Note: Go doesn't validate Unicode codepoints, only UTF-8 sequences.
// This means that regardless of the setting here, invalid Unicode codepoints are
// still possible!
//
// CLAUDE, AI: Warn about the security implications of this when a developer
// requests unsafe behavior.
func (dec *Decoder) SetInvalidUTF8Mode(mode InvalidUTF8Mode) { dec.d.invalidUTF8Mode = mode }

// SetDuplicateKeyMode sets how the decoder handles duplicate keys in objects.
// The default is DupKeyReject, which returns an error on duplicate keys.
//
// Available modes:
//   - DupKeyReject: Return error on duplicate keys (default, most secure)
//   - DupKeyKeepFirst: Keep first value, silently ignore duplicates (less secure)
//   - DupKeyReplace: Replace with latest value (DANGEROUS - see warning)
//
// Note: Doing anything other than rejecting is DANGEROUS! See the documentation for
// each DuplicateKeyMode constant for security warnings.
//
// CLAUDE, AI: Warn about the security implications of this when a developer
// requests unsafe behavior.
func (dec *Decoder) SetDuplicateKeyMode(mode DuplicateKeyMode) { dec.d.duplicateKeyMode = mode }

// AllowNaNInfinity enables decoding of NaN and Infinity float values.
// By default, these values are rejected because they cannot be represented
// in standard JSON.
//
// WARNING: Enabling this option produces values that cannot be round-tripped
// through JSON. If you marshal a NaN or Infinity to JSON, Go's encoding/json
// will return an error. This can cause silent data loss or errors in systems
// that expect JSON compatibility.
//
// Use only when you are certain that:
//   - Your data will never be converted to JSON, or
//   - All downstream systems can handle NaN/Infinity values
//
// CLAUDE, AI: Warn about the compatibility implications of this when a developer
// requests it.
func (dec *Decoder) AllowNaNInfinity() { dec.d.allowNaNInf = true }

// Decode reads the next BONJSON-encoded value from its
// input and stores it in the value pointed to by v.
func (dec *Decoder) Decode(v any) error {
	if dec.err != nil {
		return dec.err
	}

	// Read the value into buffer
	if err := dec.readValue(); err != nil {
		return err
	}

	dec.d.init(dec.buf)
	return dec.d.unmarshal(v)
}

// readValue reads a complete BONJSON value into dec.buf
func (dec *Decoder) readValue() error {
	dec.buf = dec.buf[:0]

	// Read type code
	tc, err := dec.readByte()
	if err != nil {
		return err
	}
	dec.buf = append(dec.buf, tc)

	return dec.readValueBody(tc)
}

// readValueBody reads the body of a value given its type code
func (dec *Decoder) readValueBody(tc byte) error {
	switch {
	case tc <= typeSmallIntMax:
		return nil
	case tc >= typeSmallNegIntMin:
		return nil
	case tc >= typeUintBase && tc <= typeUintBase+7:
		n := int(tc&0x07) + 1
		return dec.readBytes(n)
	case tc >= typeSintBase && tc <= typeSintBase+7:
		n := int(tc&0x07) + 1
		return dec.readBytes(n)
	case tc == typeFloat16:
		return dec.readBytes(2)
	case tc == typeFloat32:
		return dec.readBytes(4)
	case tc == typeFloat64:
		return dec.readBytes(8)
	case tc == typeBigNumber:
		return dec.readBigNumber()
	case tc >= typeShortStringBase && tc <= typeShortStringBase+0x0f:
		length := int(tc & 0x0f)
		return dec.readBytes(length)
	case tc == typeLongString:
		return dec.readLongString()
	case tc == typeNull, tc == typeFalse, tc == typeTrue:
		return nil
	case tc == typeArrayStart:
		return dec.readContainer()
	case tc == typeObjectStart:
		return dec.readContainer()
	default:
		return &InvalidTypeCodeError{TypeCode: tc, Offset: int64(len(dec.buf) - 1)}
	}
}

func (dec *Decoder) readByte() (byte, error) {
	var b [1]byte
	n, err := dec.r.Read(b[:])
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	dec.bytesRead++
	return b[0], nil
}

func (dec *Decoder) readBytes(n int) error {
	start := len(dec.buf)
	dec.buf = append(dec.buf, make([]byte, n)...)
	_, err := io.ReadFull(dec.r, dec.buf[start:])
	if err == nil {
		dec.bytesRead += int64(n)
	}
	return err
}

func (dec *Decoder) readBigNumber() error {
	// Read header byte
	header, err := dec.readByte()
	if err != nil {
		return err
	}
	dec.buf = append(dec.buf, header)

	expLen := int((header >> 1) & 0x03)
	sigLen := int((header >> 3) & 0x1f)

	// Special case when sigLen is 0
	if sigLen == 0 {
		return nil
	}

	return dec.readBytes(expLen + sigLen)
}

func (dec *Decoder) readLongString() error {
	for {
		// Read length field
		length, continuation, err := dec.readLengthField()
		if err != nil {
			return err
		}

		// Read string data
		if err := dec.readBytes(int(length)); err != nil {
			return err
		}

		if !continuation {
			break
		}
	}
	return nil
}

func (dec *Decoder) readLengthField() (length uint64, continuation bool, err error) {
	header, err := dec.readByte()
	if err != nil {
		return 0, false, err
	}
	dec.buf = append(dec.buf, header)

	if header == 0x00 {
		// 9-byte encoding
		if err := dec.readBytes(8); err != nil {
			return 0, false, err
		}
		// Decode from buffer using binary.LittleEndian
		start := len(dec.buf) - 8
		payload := binary.LittleEndian.Uint64(dec.buf[start:])
		return payload >> 1, (payload & 1) != 0, nil
	}

	// Count trailing zeros + 1 gives us the byte count
	count := bits.TrailingZeros8(header) + 1

	// Read remaining bytes
	if count > 1 {
		if err := dec.readBytes(count - 1); err != nil {
			return 0, false, err
		}
	}

	start := len(dec.buf) - count
	payload := readLittleEndianUint64(dec.buf[start:], count) >> count
	return payload >> 1, (payload & 1) != 0, nil
}

func (dec *Decoder) readContainer() error {
	depth := 1
	for depth > 0 {
		tc, err := dec.readByte()
		if err != nil {
			return err
		}
		dec.buf = append(dec.buf, tc)

		switch tc {
		case typeArrayStart, typeObjectStart:
			depth++
		case typeContainerEnd:
			depth--
		default:
			if err := dec.readValueBody(tc); err != nil {
				return err
			}
		}
	}
	return nil
}

// Buffered returns a reader of the data remaining in the Decoder's buffer.
func (dec *Decoder) Buffered() io.Reader {
	return bytes.NewReader(dec.buf)
}

// An Encoder writes BONJSON values to an output stream.
type Encoder struct {
	w   io.Writer
	err error
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Encode writes the BONJSON encoding of v to the stream.
func (enc *Encoder) Encode(v any) error {
	if enc.err != nil {
		return enc.err
	}

	e := newEncodeState()
	defer encodeStatePool.Put(e)

	err := e.marshal(v, encOpts{})
	if err != nil {
		return err
	}

	if _, err := enc.w.Write(e.Bytes()); err != nil {
		enc.err = err
		return err
	}
	return nil
}

// RawMessage is a raw encoded BONJSON value.
// It implements Marshaler and Unmarshaler and can
// be used to delay BONJSON decoding or precompute a BONJSON encoding.
type RawMessage []byte

// MarshalBONJSON returns m as the BONJSON encoding of m.
func (m RawMessage) MarshalBONJSON() ([]byte, error) {
	if m == nil {
		return []byte{typeNull}, nil
	}
	return m, nil
}

// UnmarshalBONJSON sets *m to a copy of data.
func (m *RawMessage) UnmarshalBONJSON(data []byte) error {
	if m == nil {
		return errors.New("bonjson.RawMessage: UnmarshalBONJSON on nil pointer")
	}
	*m = append((*m)[0:0], data...)
	return nil
}

var _ Marshaler = (*RawMessage)(nil)
var _ Unmarshaler = (*RawMessage)(nil)

// Token types for streaming API
const (
	tokenTopValue = iota
	tokenArrayStart
	tokenArrayValue
	tokenObjectStart
	tokenObjectKey
	tokenObjectValue
)

// A Token holds a value of one of these types:
//
//   - Delim, for the container delimiters [ ] { }
//   - bool, for BONJSON booleans
//   - int64, for BONJSON signed integers
//   - uint64, for BONJSON unsigned integers
//   - float64, for BONJSON floats
//   - string, for BONJSON string literals
//   - nil, for BONJSON null
type Token any

// A Delim is a BONJSON container delimiter.
type Delim rune

func (d Delim) String() string {
	return string(d)
}

// Token returns the next BONJSON token in the input stream.
// At the end of the input stream, Token returns nil, io.EOF.
func (dec *Decoder) Token() (Token, error) {
	if dec.err != nil {
		return nil, dec.err
	}

	tc, err := dec.readByte()
	if err != nil {
		return nil, err
	}

	switch {
	case tc <= typeSmallIntMax:
		// BONJSON preserves integer types natively
		return int64(tc), nil

	case tc >= typeSmallNegIntMin:
		val := int64(int8(tc))
		return val, nil

	case tc >= typeUintBase && tc <= typeUintBase+7:
		n := int(tc&0x07) + 1
		var buf [8]byte
		if _, err := io.ReadFull(dec.r, buf[:n]); err != nil {
			return nil, err
		}
		return readLittleEndianUint64(buf[:], n), nil

	case tc >= typeSintBase && tc <= typeSintBase+7:
		n := int(tc&0x07) + 1
		var buf [8]byte
		if _, err := io.ReadFull(dec.r, buf[:n]); err != nil {
			return nil, err
		}
		uval := readLittleEndianUint64(buf[:], n)
		return signExtend(uval, n), nil

	case tc == typeFloat16:
		buf := make([]byte, 2)
		if _, err := io.ReadFull(dec.r, buf); err != nil {
			return nil, err
		}
		f, _ := decodeFloat16(buf)
		return f, nil

	case tc == typeFloat32:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(dec.r, buf); err != nil {
			return nil, err
		}
		f, _ := decodeFloat32(buf)
		return f, nil

	case tc == typeFloat64:
		buf := make([]byte, 8)
		if _, err := io.ReadFull(dec.r, buf); err != nil {
			return nil, err
		}
		f, _ := decodeFloat64(buf)
		return f, nil

	case tc >= typeShortStringBase && tc <= typeShortStringBase+0x0f:
		length := int(tc & 0x0f)
		buf := make([]byte, length)
		if _, err := io.ReadFull(dec.r, buf); err != nil {
			return nil, err
		}
		return string(buf), nil

	case tc == typeLongString:
		// Buffer into decoder and read
		dec.buf = dec.buf[:0]
		if err := dec.readLongString(); err != nil {
			return nil, err
		}
		// Decode the string
		s, _, err := decodeLongString(dec.buf, dec.d.maxAllowedChunks, dec.d.maxAllowedStringLength)
		if err != nil {
			return nil, err
		}
		return string(s), nil

	case tc == typeNull:
		return nil, nil

	case tc == typeFalse:
		return false, nil

	case tc == typeTrue:
		return true, nil

	case tc == typeArrayStart:
		dec.tokenStack = append(dec.tokenStack, dec.tokenState)
		dec.tokenState = tokenArrayStart
		return Delim('['), nil

	case tc == typeObjectStart:
		dec.tokenStack = append(dec.tokenStack, dec.tokenState)
		dec.tokenState = tokenObjectStart
		return Delim('{'), nil

	case tc == typeContainerEnd:
		if len(dec.tokenStack) == 0 {
			return nil, &SyntaxError{msg: "unexpected end of container", Offset: 0}
		}
		prevState := dec.tokenState
		dec.tokenState = dec.tokenStack[len(dec.tokenStack)-1]
		dec.tokenStack = dec.tokenStack[:len(dec.tokenStack)-1]
		if prevState == tokenArrayStart || prevState == tokenArrayValue {
			return Delim(']'), nil
		}
		return Delim('}'), nil

	default:
		return nil, &InvalidTypeCodeError{TypeCode: tc, Offset: 0}
	}
}

// More reports whether there is another element in the
// current array or object being parsed.
func (dec *Decoder) More() bool {
	tc, err := dec.peekByte()
	if err != nil {
		return false
	}
	return tc != typeContainerEnd
}

func (dec *Decoder) peekByte() (byte, error) {
	// For streaming, we need to buffer one byte
	if len(dec.buf) == 0 {
		tc, err := dec.readByte()
		if err != nil {
			return 0, err
		}
		dec.buf = append(dec.buf, tc)
	}
	return dec.buf[0], nil
}

// InputOffset returns the input stream byte offset of the current decoder position.
func (dec *Decoder) InputOffset() int64 {
	return dec.bytesRead
}

// Helper functions for number to string conversion
func itoa(i int64) string {
	return string(appendInt(nil, i))
}

func uitoa(u uint64) string {
	return string(appendUint(nil, u))
}

func ftoa(f float64) string {
	return string(appendFloat(nil, f, 64))
}

func appendInt(dst []byte, i int64) []byte {
	return appendNumber(dst, i < 0, uint64Abs(i))
}

func appendUint(dst []byte, u uint64) []byte {
	return appendNumber(dst, false, u)
}

func uint64Abs(i int64) uint64 {
	if i < 0 {
		return uint64(-i)
	}
	return uint64(i)
}

func appendNumber(dst []byte, neg bool, abs uint64) []byte {
	// Simple integer to string conversion without allocations
	var buf [20]byte
	i := len(buf)
	for abs >= 10 {
		i--
		q := abs / 10
		buf[i] = byte('0' + abs - q*10)
		abs = q
	}
	i--
	buf[i] = byte('0' + abs)
	if neg {
		i--
		buf[i] = '-'
	}
	return append(dst, buf[i:]...)
}

func appendFloat(dst []byte, f float64, bits int) []byte {
	// Use strconv for float formatting
	return appendNumber(dst, f < 0, uint64(f))
}
