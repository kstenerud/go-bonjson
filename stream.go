// Copyright 2024 Karl Stenerud. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bonjson

import (
	"bytes"
	"errors"
	"io"
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
	dec.d.maxStringLength = defaultMaxStringLength
	dec.d.maxDepth = defaultMaxContainerDepth
	return dec
}

// DisallowUnknownFields causes the Decoder to return an error when the destination
// is a struct and the input contains object keys which do not match any
// non-ignored, exported fields in the destination.
func (dec *Decoder) DisallowUnknownFields() { dec.d.disallowUnknownFields = true }

// AllowChunking enables support for chunked strings.
// By default, chunking is disabled for security.
func (dec *Decoder) AllowChunking() { dec.d.allowChunking = true }

// AllowNUL enables NUL characters in strings.
// By default, NUL characters are forbidden for security.
func (dec *Decoder) AllowNUL() { dec.d.allowNUL = true }

// SetMaxStringLength sets the maximum allowed string length.
func (dec *Decoder) SetMaxStringLength(n int64) { dec.d.maxStringLength = n }

// SetMaxDepth sets the maximum allowed nesting depth.
func (dec *Decoder) SetMaxDepth(n int) { dec.d.maxDepth = n }

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
		// Decode from buffer
		start := len(dec.buf) - 8
		var payload uint64
		for i := 0; i < 8; i++ {
			payload |= uint64(dec.buf[start+i]) << (i * 8)
		}
		return payload >> 1, (payload & 1) != 0, nil
	}

	// Count trailing zeros to determine field size
	count := 1
	for i := 0; i < 8; i++ {
		if header&(1<<i) != 0 {
			count = i + 1
			break
		}
	}

	// Read remaining bytes
	if count > 1 {
		if err := dec.readBytes(count - 1); err != nil {
			return 0, false, err
		}
	}

	// Decode
	start := len(dec.buf) - count
	var encoded uint64
	for i := 0; i < count; i++ {
		encoded |= uint64(dec.buf[start+i]) << (i * 8)
	}
	payload := encoded >> count
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
		buf := make([]byte, n)
		if _, err := io.ReadFull(dec.r, buf); err != nil {
			return nil, err
		}
		var val uint64
		for i := 0; i < n; i++ {
			val |= uint64(buf[i]) << (i * 8)
		}
		return val, nil

	case tc >= typeSintBase && tc <= typeSintBase+7:
		n := int(tc&0x07) + 1
		buf := make([]byte, n)
		if _, err := io.ReadFull(dec.r, buf); err != nil {
			return nil, err
		}
		var uval uint64
		for i := 0; i < n; i++ {
			uval |= uint64(buf[i]) << (i * 8)
		}
		val := int64(uval)
		if n < 8 {
			signBit := uint64(1) << (n*8 - 1)
			if uval&signBit != 0 {
				mask := ^uint64(0) << (n * 8)
				val = int64(uval | mask)
			}
		}
		return val, nil

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
		s, _, err := decodeLongString(dec.buf, dec.d.allowChunking, dec.d.maxStringLength)
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
