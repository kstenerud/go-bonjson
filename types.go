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
	// Small integers 0-100 (type codes 0x00-0x64)
	typeSmallIntMin = 0x00
	typeSmallIntMax = 0x64 // 100

	// Reserved (0x65-0x67)

	// Long string (0x68)
	typeLongString = 0x68

	// Big number (0x69)
	typeBigNumber = 0x69

	// 16-bit bfloat16 (0x6a)
	typeFloat16 = 0x6a

	// 32-bit float (0x6b)
	typeFloat32 = 0x6b

	// 64-bit float (0x6c)
	typeFloat64 = 0x6c

	// Null (0x6d)
	typeNull = 0x6d

	// Boolean false (0x6e)
	typeFalse = 0x6e

	// Boolean true (0x6f)
	typeTrue = 0x6f

	// Unsigned integers (0x70-0x77) - n bytes where n = (typecode & 0x07) + 1
	typeUintBase = 0x70

	// Signed integers (0x78-0x7f) - n bytes where n = (typecode & 0x07) + 1
	typeSintBase = 0x78

	// Short strings (0x80-0x8f) - n bytes where n = (typecode & 0x0f)
	typeShortStringBase = 0x80

	// Reserved (0x90-0x98)

	// Array start (0x99)
	typeArrayStart = 0x99

	// Object start (0x9a)
	typeObjectStart = 0x9a

	// Container end (0x9b)
	typeContainerEnd = 0x9b

	// Small negative integers -100 to -1 (type codes 0x9c-0xff)
	typeSmallNegIntMin = 0x9c // -100
	typeSmallNegIntMax = 0xff // -1
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
const (
	defaultMaxStringLength   = 10 * 1024 * 1024 // 10 MB
	defaultMaxContainerDepth = 1000
	defaultMaxObjectKeys     = 100000
	defaultMaxChunks         = 100 // Max chunks per string value
)
