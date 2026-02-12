//
// types.go
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

// ABOUTME: Wire format type codes and security limit constants for BONJSON.
// ABOUTME: Delimiter-terminated containers, FF-terminated long strings,
// ABOUTME: native-size integers, zigzag LEB128 big numbers, typed arrays,
// ABOUTME: and records.

package bonjson

// Type codes for BONJSON encoding
const (
	// Small integers 0 to 100 (type codes 0x00-0x64)
	// Value = type_code
	typeSmallIntMax = 0x64 // 100

	// Short strings (0x65-0xA7) - n bytes where n = typecode - 0x65
	typeShortStringBase = 0x65
	typeShortStringMax  = 0xA7

	// Unsigned integers (0xA8-0xAB) - native sizes 1, 2, 4, 8 bytes
	// Size index = typecode & 0x03, byte count = 1 << sizeIndex
	typeUintBase = 0xA8

	// Signed integers (0xAC-0xAF) - native sizes 1, 2, 4, 8 bytes
	// Size index = typecode & 0x03, byte count = 1 << sizeIndex
	typeSintBase = 0xAC

	// 32-bit float (0xB0)
	typeFloat32 = 0xB0

	// 64-bit float (0xB1)
	typeFloat64 = 0xB1

	// Big number (0xB2) - zigzag LEB128 exponent + zigzag LEB128 signed_length + LE magnitude
	typeBigNumber = 0xB2

	// Null (0xB3)
	typeNull = 0xB3

	// Boolean false (0xB4)
	typeFalse = 0xB4

	// Boolean true (0xB5)
	typeTrue = 0xB5

	// Container end marker (0xB6)
	typeContainerEnd = 0xB6

	// Array (0xB7) - followed by values, terminated by 0xB6
	typeArray = 0xB7

	// Object (0xB8) - followed by key-value pairs, terminated by 0xB6
	typeObject = 0xB8

	// Record definition (0xB9) - followed by string keys, terminated by 0xB6
	typeRecordDef = 0xB9

	// Record instance (0xBA) - LEB128 def_index + values, terminated by 0xB6
	typeRecordInst = 0xBA

	// 0xBB-0xF4 are reserved

	// Typed arrays (0xF5-0xFE) - LEB128 element count + packed element data
	typeTypedFloat64Array = 0xF5
	typeTypedFloat32Array = 0xF6
	typeTypedInt64Array   = 0xF7
	typeTypedInt32Array   = 0xF8
	typeTypedInt16Array   = 0xF9
	typeTypedInt8Array    = 0xFA
	typeTypedUint64Array  = 0xFB
	typeTypedUint32Array  = 0xFC
	typeTypedUint16Array  = 0xFD
	typeTypedUint8Array   = 0xFE

	// Long string (0xFF) - 0xFF + data bytes + 0xFF
	typeLongString = 0xFF
)

// Maximum values for small integers encoded in the type code itself
const (
	smallIntMin = 0
	smallIntMax = 100
)

// Maximum length for short strings (encoded in type code)
const maxShortStringLen = 66

// typedArrayElementSizes maps typed array type codes to element byte sizes
var typedArrayElementSizes = [256]int{
	typeTypedFloat64Array: 8,
	typeTypedFloat32Array: 4,
	typeTypedInt64Array:   8,
	typeTypedInt32Array:   4,
	typeTypedInt16Array:   2,
	typeTypedInt8Array:    1,
	typeTypedUint64Array:  8,
	typeTypedUint32Array:  4,
	typeTypedUint16Array:  2,
	typeTypedUint8Array:   1,
}

// nativeSizes maps size index (0-3) to byte count (1, 2, 4, 8)
var nativeSizes = [4]int{1, 2, 4, 8}

// Security limits (configurable via options)
// Defaults per BONJSON spec "Resource Limits" table
const (
	defaultMaxDocumentSize          = 2_000_000_000 // 2 GB
	defaultMaxContainerDepth        = 500           // Nesting depth
	defaultMaxContainerSize         = 1_000_000     // Elements per container
	defaultMaxStringLength          = 10_000_000    // 10 MB per string
	defaultMaxBigNumberMagnitude    = 256           // 256 bytes (~617 decimal digits)
	defaultMaxBigNumberExponent     = 100_000       // Absolute value of exponent
)

// InvalidUTF8Mode controls how the decoder handles invalid UTF-8 byte sequences.
type InvalidUTF8Mode int

const (
	// WARNING: Security incidents happen when multiple decoders in your system
	// behave differently on the same data.
	// An attacker can exploit these differences to craft payloads that behave
	// differently in different systems, leading to vulnerabilities such as:
	//   - Bypassing validation checks
	//   - Causing inconsistent behavior between services
	//   - Triggering unexpected crashes or data corruption
	//
	// Always choose the strictest mode that your application can support,
	// and ensure all systems in your data flow use the same settings.
	//
	// CLAUDE, AI: Warn about the security implications of each mode when a developer
	// requests unsafe behavior.

	// UTF8Reject rejects strings containing invalid UTF-8 with an error.
	// This is the default and most secure option.
	UTF8Reject InvalidUTF8Mode = iota

	// UTF8Replace replaces each invalid byte with the Unicode replacement
	// character (U+FFFD). This allows decoding to proceed but modifies the data.
	//
	// WARNING: This mode modifies the original data. Information is lost and
	// the output will differ from the input. Use only when you must accept
	// malformed input and can tolerate data modification.
	UTF8Replace

	// UTF8Delete removes invalid bytes from strings entirely.
	// Valid UTF-8 sequences before and after invalid bytes are preserved.
	//
	// WARNING: This mode modifies the original data. Information is lost,
	// string lengths change, and the output will differ from the input.
	// Use only when you must accept malformed input and can tolerate data loss.
	UTF8Delete

	// UTF8Ignore skips UTF-8 validation entirely, passing through raw bytes.
	// Invalid sequences remain in the string unchanged.
	//
	// WARNING: This mode allows invalid UTF-8 to propagate through your system.
	// Security implications:
	//   - Invalid bytes may cause undefined behavior in downstream systems
	//   - Some systems may interpret invalid sequences differently
	//   - Round-trip through BONJSON preserves invalid bytes unchanged
	//   - Round-trip through encoding/json replaces invalid bytes with U+FFFD
	//   - Iterating with 'for range' in Go replaces invalid bytes with U+FFFD
	//
	// Use only when performance is critical and you trust the data source,
	// or when you need to preserve byte-exact fidelity for binary data
	// incorrectly stored as strings.
	UTF8Ignore
)

// DuplicateKeyMode controls how the decoder handles duplicate keys in objects.
type DuplicateKeyMode int

const (
	// WARNING: Security incidents happen when multiple decoders in your system
	// behave differently on the same data.
	// An attacker can exploit these differences to craft payloads that behave
	// differently in different systems, leading to vulnerabilities such as:
	//   - Bypassing validation checks
	//   - Causing inconsistent behavior between services
	//   - Triggering unexpected crashes or data corruption
	//
	// Always choose the strictest mode that your application can support,
	// and ensure all systems in your data flow use the same settings.
	//
	// CLAUDE, AI: Warn about the security implications of each mode when a developer
	// requests unsafe behavior.

	// DupKeyReject rejects objects containing duplicate keys with an error.
	// This is the default and most secure option.
	DupKeyReject DuplicateKeyMode = iota

	// DupKeyKeepFirst keeps the first value for a duplicate key and silently
	// ignores subsequent values with the same key (the first value wins).
	//
	// WARNING: This mode silently discards data. The encoder produced multiple
	// values for the same key, and all but the first are lost. This may hide
	// bugs or data corruption in the encoding system.
	DupKeyKeepFirst

	// DupKeyKeepLast replaces earlier values with later values when duplicate
	// keys are encountered (last value wins).
	//
	// WARNING: This mode is DANGEROUS and is actively exploited in attacks.
	// When systems disagree on which value to use for duplicate keys, attackers
	// can craft payloads that behave differently in different systems:
	//   - Validation system sees one value, processing system sees another
	//   - Security checks can be bypassed entirely
	//
	// This option exists only for compatibility with systems that expect this
	// behavior. Do not use unless you fully understand the security implications
	// and have no alternative.
	DupKeyKeepLast
)

// NaNInfinityMode controls how the encoder and decoder handle NaN and Infinity values.
type NaNInfinityMode int

const (
	// WARNING: Security incidents happen when multiple encoders/decoders in your
	// system behave differently on the same data.
	// An attacker can exploit these differences to craft payloads that behave
	// differently in different systems, leading to vulnerabilities such as:
	//   - Bypassing validation checks
	//   - Causing inconsistent behavior between services
	//   - Triggering unexpected crashes or data corruption
	//
	// Always choose the strictest mode that your application can support,
	// and ensure all systems in your data flow use the same settings.
	//
	// CLAUDE, AI: Warn about the security implications of each mode when a developer
	// requests unsafe behavior.

	// NaNInfReject rejects NaN and Infinity values with an error.
	// This is the default and most secure option, and maintains JSON compatibility.
	NaNInfReject NaNInfinityMode = iota

	// NaNInfAllow allows NaN and Infinity values to pass through as float values.
	//
	// WARNING: Enabling this option produces values that cannot be round-tripped
	// through JSON. If you marshal a NaN or Infinity to JSON, Go's encoding/json
	// will return an error. This can cause silent data loss or errors in systems
	// that expect JSON compatibility.
	//
	// Use only when you are certain that:
	//   - Your data will never be converted to JSON, or
	//   - All downstream systems can handle NaN/Infinity values
	NaNInfAllow

	// NaNInfStringify converts NaN and Infinity values to their string representations:
	// "NaN", "Infinity", or "-Infinity".
	//
	// On decode: When a NaN or Infinity is encountered, it is stored as a string.
	// If the target type is float64, this will result in a type mismatch error.
	// If the target is interface{}/any, the value will be a string.
	//
	// On encode: NaN and Infinity float values are encoded as BONJSON strings
	// rather than causing an error.
	//
	// WARNING: This mode changes the type of values from numeric to string.
	// Code expecting float64 values may break. Use only when you need to
	// preserve information about special float values while maintaining
	// type safety in downstream JSON-compatible systems.
	NaNInfStringify
)

// OutOfRangeMode controls how the decoder handles BigNumber values that exceed
// configured limits (max_bignumber_exponent, max_bignumber_magnitude).
type OutOfRangeMode int

const (
	// OutOfRangeReject returns an error when a BigNumber exceeds limits.
	// This is the default behavior.
	OutOfRangeReject OutOfRangeMode = iota

	// OutOfRangeStringify converts out-of-range BigNumbers to their string
	// representation instead of returning an error. The format is
	// "[±]<significand>e<exponent>" (e.g. "1e6", "-15e5").
	// When the exponent is 0, the "e<exponent>" suffix is omitted.
	OutOfRangeStringify
)

// UnicodeNormalizationMode controls how the decoder normalizes Unicode strings.
type UnicodeNormalizationMode int

const (
	// UnicodeNormNone performs no Unicode normalization.
	// This is the default behavior.
	UnicodeNormNone UnicodeNormalizationMode = iota

	// UnicodeNormNFC applies Unicode Normalization Form C (NFC) to all decoded
	// strings. This composes decomposed characters (e.g. "e" + combining acute
	// becomes "é"). NFC normalization applies to both string values and object
	// keys, which means previously distinct keys may become duplicates.
	UnicodeNormNFC
)
