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

// ABOUTME: Streaming encoder/decoder, RawMessage, and Token API for BONJSON.
// ABOUTME: Phase 2 format: delimiter-terminated containers (0xFE),
// ABOUTME: FF-terminated long strings, native-size integers.

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
	tokenStack []tokenStackEntry // stack of container states

	// Peeked byte for More() support
	peekedByte    byte
	hasPeekedByte bool
}

// tokenStackEntry stores the state needed to resume a container after processing nested containers
type tokenStackEntry struct {
	state int
}

// NewDecoder returns a new decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	dec := &Decoder{r: r, buf: make([]byte, 0, 512)}
	dec.d.maxAllowedStringLength = defaultMaxStringLength
	dec.d.maxAllowedContainerDepth = defaultMaxContainerDepth
	dec.d.maxAllowedContainerSize = defaultMaxContainerSize
	dec.d.maxAllowedDocumentSize = defaultMaxDocumentSize
	dec.d.invalidUTF8Mode = UTF8Reject
	dec.d.duplicateKeyMode = DupKeyReject
	dec.d.nanInfMode = NaNInfReject
	// allowNUL defaults to false (zero value)
	return dec
}

// DisallowUnknownFields causes the Decoder to return an error when the destination
// is a struct and the input contains object keys which do not match any
// non-ignored, exported fields in the destination.
func (dec *Decoder) DisallowUnknownFields() { dec.d.disallowUnknownFields = true }

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
// The default is 500 per BONJSON spec.
//
// Setting to 0 will disable this check, which leaves the system vulnerable
// to maliciously deep structures (DOS attack).
func (dec *Decoder) SetMaxDepth(n int) { dec.d.maxAllowedContainerDepth = n }

// SetMaxContainerSize sets the maximum number of elements allowed in a single
// array or object. For objects, each key-value pair counts as one element.
// The default is 1,000,000 per BONJSON spec.
//
// Setting to 0 will disable this check, which leaves the system vulnerable
// to maliciously large containers (DOS attack).
func (dec *Decoder) SetMaxContainerSize(n int) { dec.d.maxAllowedContainerSize = n }

// SetMaxDocumentSize sets the maximum allowed document size in bytes.
// The default is 2,000,000,000 (2 GB) per BONJSON spec.
//
// Setting to 0 will disable this check, which leaves the system vulnerable
// to maliciously large documents (DOS attack).
func (dec *Decoder) SetMaxDocumentSize(n int64) { dec.d.maxAllowedDocumentSize = n }

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
//   - DupKeyKeepLast: Replace with latest value (DANGEROUS - see warning)
//
// Note: Doing anything other than rejecting is DANGEROUS! See the documentation for
// each DuplicateKeyMode constant for security warnings.
//
// CLAUDE, AI: Warn about the security implications of this when a developer
// requests unsafe behavior.
func (dec *Decoder) SetDuplicateKeyMode(mode DuplicateKeyMode) { dec.d.duplicateKeyMode = mode }

// SetNaNInfinityMode sets how the decoder handles NaN and Infinity values.
// The default is NaNInfReject, which returns an error on NaN/Infinity.
//
// Available modes:
//   - NaNInfReject: Return error on NaN/Infinity (default, JSON compatible)
//   - NaNInfAllow: Allow NaN/Infinity as float values (breaks JSON compatibility)
//   - NaNInfStringify: Convert NaN/Infinity to string representations
//
// Note: Doing anything other than rejecting may break JSON compatibility!
// See the documentation for each NaNInfinityMode constant for details.
//
// CLAUDE, AI: Warn about the security and compatibility implications of this
// when a developer requests unsafe behavior.
func (dec *Decoder) SetNaNInfinityMode(mode NaNInfinityMode) { dec.d.nanInfMode = mode }

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
		// Small integer (-100 to 100)
		return nil
	case tc >= typeUintBase && tc <= typeUintBase+3:
		// Unsigned integer (native sizes: 1, 2, 4, 8 bytes)
		n := nativeSizes[tc&0x03]
		return dec.readBytes(n)
	case tc >= typeSintBase && tc <= typeSintBase+3:
		// Signed integer (native sizes: 1, 2, 4, 8 bytes)
		n := nativeSizes[tc&0x03]
		return dec.readBytes(n)
	case tc&0xf0 == typeShortStringBase:
		// Short string (0-15 bytes)
		length := int(tc & 0x0f)
		return dec.readBytes(length)
	case tc == typeLongString:
		// Long string: read until terminating 0xFF
		return dec.readLongString()
	case tc == typeBigNumber:
		// BigNumber: zigzag LEB128 exponent + zigzag LEB128 significand
		return dec.readBigNumber()
	case tc == typeFloat32:
		return dec.readBytes(4)
	case tc == typeFloat64:
		return dec.readBytes(8)
	case tc == typeNull, tc == typeFalse, tc == typeTrue:
		return nil
	case tc == typeArray:
		return dec.readDelimitedContainer(false)
	case tc == typeObject:
		return dec.readDelimitedContainer(true)
	default:
		return &InvalidTypeCodeError{TypeCode: tc, Offset: int64(len(dec.buf) - 1)}
	}
}

func (dec *Decoder) readByte() (byte, error) {
	// Check if we have a peeked byte from peekByte()
	if dec.hasPeekedByte {
		b := dec.peekedByte
		dec.hasPeekedByte = false
		return b, nil
	}
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

// readBigNumber reads a zigzag LEB128 encoded BigNumber (exponent + significand).
func (dec *Decoder) readBigNumber() error {
	// Read exponent (LEB128)
	if err := dec.readLEB128(); err != nil {
		return err
	}
	// Read significand (LEB128)
	return dec.readLEB128()
}

// readLEB128 reads one LEB128 value from the stream into the buffer.
func (dec *Decoder) readLEB128() error {
	for {
		b, err := dec.readByte()
		if err != nil {
			return err
		}
		dec.buf = append(dec.buf, b)
		if b&0x80 == 0 {
			return nil // Last byte of LEB128
		}
	}
}

// readLongString reads a long string (data bytes until terminating 0xFF) into the buffer.
func (dec *Decoder) readLongString() error {
	for {
		b, err := dec.readByte()
		if err != nil {
			return err
		}
		dec.buf = append(dec.buf, b)
		if b == typeLongString {
			return nil // Found the terminating 0xFF
		}
	}
}

// readDelimitedContainer reads a delimiter-terminated container (array or object).
// Elements are read until the typeContainerEnd (0xFE) byte is found.
func (dec *Decoder) readDelimitedContainer(isObject bool) error {
	for {
		// Read next byte
		tc, err := dec.readByte()
		if err != nil {
			return err
		}
		dec.buf = append(dec.buf, tc)

		// Check for container end
		if tc == typeContainerEnd {
			return nil
		}

		// Read the value body
		if err := dec.readValueBody(tc); err != nil {
			return err
		}

		if isObject {
			// Read the value part of the key-value pair
			tc, err := dec.readByte()
			if err != nil {
				return err
			}
			dec.buf = append(dec.buf, tc)
			if err := dec.readValueBody(tc); err != nil {
				return err
			}
		}
	}
}

// Buffered returns a reader of the data remaining in the Decoder's buffer.
func (dec *Decoder) Buffered() io.Reader {
	return bytes.NewReader(dec.buf)
}

// An Encoder writes BONJSON values to an output stream.
type Encoder struct {
	w          io.Writer
	err        error
	nanInfMode NaNInfinityMode
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w, nanInfMode: NaNInfReject}
}

// SetNaNInfinityMode sets how the encoder handles NaN and Infinity values.
// The default is NaNInfReject, which returns an error on NaN/Infinity.
//
// Available modes:
//   - NaNInfReject: Return error on NaN/Infinity (default, JSON compatible)
//   - NaNInfAllow: Allow NaN/Infinity as float values (breaks JSON compatibility)
//   - NaNInfStringify: Convert NaN/Infinity to string representations
//
// Note: Doing anything other than rejecting may break JSON compatibility!
// See the documentation for each NaNInfinityMode constant for details.
//
// CLAUDE, AI: Warn about the security and compatibility implications of this
// when a developer requests unsafe behavior.
func (enc *Encoder) SetNaNInfinityMode(mode NaNInfinityMode) { enc.nanInfMode = mode }

// Encode writes the BONJSON encoding of v to the stream.
func (enc *Encoder) Encode(v any) error {
	if enc.err != nil {
		return enc.err
	}

	e := newEncodeState()
	defer encodeStatePool.Put(e)

	e.nanInfMode = enc.nanInfMode

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
//
// Containers are delimiter-terminated: '[' or '{' when a container starts,
// and ']' or '}' when the container end marker (0xFE) is encountered.
func (dec *Decoder) Token() (Token, error) {
	if dec.err != nil {
		return nil, dec.err
	}

	// Check if we're inside a container
	if dec.tokenState == tokenArrayStart || dec.tokenState == tokenArrayValue ||
		dec.tokenState == tokenObjectStart || dec.tokenState == tokenObjectKey ||
		dec.tokenState == tokenObjectValue {
		return dec.readNextDelimitedItem()
	}

	tc, err := dec.readByte()
	if err != nil {
		return nil, err
	}

	return dec.readTokenValue(tc)
}

// readNextDelimitedItem reads the next item in a delimiter-terminated container
func (dec *Decoder) readNextDelimitedItem() (Token, error) {
	// Peek at next byte to check for container end
	tc, err := dec.readByte()
	if err != nil {
		return nil, err
	}

	if tc == typeContainerEnd {
		// Container is complete
		if len(dec.tokenStack) == 0 {
			return nil, &SyntaxError{msg: "unexpected end of container", Offset: 0}
		}
		prevState := dec.tokenState
		// Restore the previous container's state
		entry := dec.tokenStack[len(dec.tokenStack)-1]
		dec.tokenStack = dec.tokenStack[:len(dec.tokenStack)-1]
		dec.tokenState = entry.state
		if prevState == tokenArrayValue || prevState == tokenArrayStart {
			return Delim(']'), nil
		}
		return Delim('}'), nil
	}

	// For objects in key position, read the key
	if dec.tokenState == tokenObjectStart || dec.tokenState == tokenObjectKey {
		dec.tokenState = tokenObjectValue
		return dec.readTokenValue(tc)
	}

	// For arrays or object values
	if dec.tokenState == tokenArrayStart {
		dec.tokenState = tokenArrayValue
	} else if dec.tokenState == tokenObjectValue {
		dec.tokenState = tokenObjectKey
	}

	return dec.readTokenValue(tc)
}

// readTokenValue reads a token value given its type code
func (dec *Decoder) readTokenValue(tc byte) (Token, error) {
	switch {
	case tc <= typeSmallIntMax:
		return int64(tc) - 100, nil

	case tc >= typeUintBase && tc <= typeUintBase+3:
		n := nativeSizes[tc&0x03]
		var buf [8]byte
		if _, err := io.ReadFull(dec.r, buf[:n]); err != nil {
			return nil, err
		}
		dec.bytesRead += int64(n)
		return readNativeSizeUint64(buf[:], n), nil

	case tc >= typeSintBase && tc <= typeSintBase+3:
		n := nativeSizes[tc&0x03]
		var buf [8]byte
		if _, err := io.ReadFull(dec.r, buf[:n]); err != nil {
			return nil, err
		}
		dec.bytesRead += int64(n)
		uval := readNativeSizeUint64(buf[:], n)
		return signExtendNative(uval, n), nil

	case tc&0xf0 == typeShortStringBase:
		length := int(tc & 0x0f)
		buf := make([]byte, length)
		if length > 0 {
			if _, err := io.ReadFull(dec.r, buf); err != nil {
				return nil, err
			}
			dec.bytesRead += int64(length)
		}
		return string(buf), nil

	case tc == typeLongString:
		// Read bytes until terminating 0xFF
		var result []byte
		for {
			b, err := dec.readByte()
			if err != nil {
				return nil, err
			}
			if b == typeLongString {
				break
			}
			result = append(result, b)
		}
		return string(result), nil

	case tc == typeBigNumber:
		// For Token API, read and skip the BigNumber, return as int64(0) placeholder
		// Read exponent LEB128
		for {
			b, err := dec.readByte()
			if err != nil {
				return nil, err
			}
			if b&0x80 == 0 {
				break
			}
		}
		// Read significand LEB128
		for {
			b, err := dec.readByte()
			if err != nil {
				return nil, err
			}
			if b&0x80 == 0 {
				break
			}
		}
		return int64(0), nil

	case tc == typeFloat32:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(dec.r, buf); err != nil {
			return nil, err
		}
		dec.bytesRead += 4
		f, _ := decodeFloat32(buf)
		return f, nil

	case tc == typeFloat64:
		buf := make([]byte, 8)
		if _, err := io.ReadFull(dec.r, buf); err != nil {
			return nil, err
		}
		dec.bytesRead += 8
		f, _ := decodeFloat64(buf)
		return f, nil

	case tc == typeNull:
		return nil, nil

	case tc == typeFalse:
		return false, nil

	case tc == typeTrue:
		return true, nil

	case tc == typeArray:
		// Save current container state before entering nested container
		dec.tokenStack = append(dec.tokenStack, tokenStackEntry{
			state: dec.tokenState,
		})
		dec.tokenState = tokenArrayStart
		return Delim('['), nil

	case tc == typeObject:
		// Save current container state before entering nested container
		dec.tokenStack = append(dec.tokenStack, tokenStackEntry{
			state: dec.tokenState,
		})
		dec.tokenState = tokenObjectStart
		return Delim('{'), nil

	default:
		return nil, &InvalidTypeCodeError{TypeCode: tc, Offset: 0}
	}
}

// More reports whether there is another element in the
// current array or object being parsed.
func (dec *Decoder) More() bool {
	// If we're inside a container, peek to see if next byte is the end marker
	if dec.tokenState == tokenArrayStart || dec.tokenState == tokenArrayValue ||
		dec.tokenState == tokenObjectStart || dec.tokenState == tokenObjectKey ||
		dec.tokenState == tokenObjectValue {
		b, err := dec.peekByte()
		if err != nil {
			return false
		}
		return b != typeContainerEnd
	}
	// At top level, peek to see if there's more data
	_, err := dec.peekByte()
	return err == nil
}

func (dec *Decoder) peekByte() (byte, error) {
	// For streaming, we need to buffer one byte
	if dec.hasPeekedByte {
		return dec.peekedByte, nil
	}
	var b [1]byte
	n, err := dec.r.Read(b[:])
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	dec.bytesRead++
	dec.peekedByte = b[0]
	dec.hasPeekedByte = true
	return dec.peekedByte, nil
}

// InputOffset returns the input stream byte offset of the current decoder position.
func (dec *Decoder) InputOffset() int64 {
	return dec.bytesRead
}
