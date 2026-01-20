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

package bonjson

// Type codes for BONJSON encoding
const (
	// Small integers -100 to 100 (type codes 0x00-0xc8)
	// Value = type_code - 100
	typeSmallIntMin = 0x00 // -100
	typeSmallIntMax = 0xc8 // 100

	// Reserved (0xc9-0xcf)

	// Unsigned integers (0xd0-0xd7) - n bytes where n = (typecode & 0x07) + 1
	typeUintBase = 0xd0

	// Signed integers (0xd8-0xdf) - n bytes where n = (typecode & 0x07) + 1
	typeSintBase = 0xd8

	// Short strings (0xe0-0xef) - n bytes where n = (typecode & 0x0f)
	typeShortStringBase = 0xe0

	// Long string (0xf0)
	typeLongString = 0xf0

	// Big number (0xf1)
	typeBigNumber = 0xf1

	// 16-bit bfloat16 (0xf2)
	typeFloat16 = 0xf2

	// 32-bit float (0xf3)
	typeFloat32 = 0xf3

	// 64-bit float (0xf4)
	typeFloat64 = 0xf4

	// Null (0xf5)
	typeNull = 0xf5

	// Boolean false (0xf6)
	typeFalse = 0xf6

	// Boolean true (0xf7)
	typeTrue = 0xf7

	// Array (0xf8) - followed by element chunks
	typeArray = 0xf8

	// Object (0xf9) - followed by pair chunks
	typeObject = 0xf9

	// Reserved (0xfa-0xff)
)

// Maximum values for small integers encoded in the type code itself
const (
	smallIntMin = -100
	smallIntMax = 100
)

// Maximum length for short strings (encoded in type code)
const maxShortStringLen = 15

// Big number special values (when significand length is 0)
const (
	bigNumZero         = 0x00 // 0
	bigNumInfinity     = 0x02 // infinity (invalid in BONJSON)
	bigNumNaNQuiet     = 0x04 // quiet NaN (invalid in BONJSON)
	bigNumNaNSignaling = 0x06 // signaling NaN (invalid in BONJSON)
	bigNumNegative     = 0x01 // negative bit
)

// Security limits (configurable via options)
// Defaults per BONJSON spec "Resource Limits" table
const (
	defaultMaxDocumentSize   = 2_000_000_000     // 2 GB
	defaultMaxContainerDepth = 512              // Nesting depth
	defaultMaxContainerSize  = 1_000_000        // Elements per container
	defaultMaxStringLength   = 10_000_000       // 10 MB per string
	defaultMaxChunks         = 100              // Chunks per string
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
