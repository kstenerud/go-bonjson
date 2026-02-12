# CLAUDE.md - go-bonjson

## Overview

This is a Go implementation of BONJSON, a binary JSON-compatible format. It provides a drop-in replacement for `encoding/json` with better performance, smaller size, and enhanced security.

**Key advantages over JSON:**
- 1.1-8x faster encoding, 2.6-8x faster decoding
- 25-100% of JSON payload size (biggest savings on bools, ints, floats)
- Native integer type preservation (int64/uint64 instead of float64)
- Security-first defaults (duplicate key rejection, UTF-8 validation, NUL rejection)


## Build and Test Commands

```bash
go test              # Run all tests
go test -v           # Verbose output
go test -bench .     # Run benchmarks
go test -cover       # Coverage report

# Cross-implementation test runner
go run ./cmd/bonjson-test/ testdata/test-config.json
```


## Architecture

### Source Files

| File | Purpose |
|------|---------|
| `bonjson.go` | Package entry point, `Valid()` function |
| `encode.go` | `Marshal()`, `AppendMarshal()`, `Marshaler` interface |
| `decode.go` | `Unmarshal()`, `UnmarshalWithByteCount()`, `Unmarshaler` interface |
| `stream.go` | `Encoder`/`Decoder` for streaming I/O, `RawMessage`, Token API |
| `wire.go` | Low-level binary encoding: integers, floats, strings, BigNumber (signed length + LE magnitude), zigzag LEB128 |
| `types.go` | Wire format type codes and security limit constants |
| `fields.go` | Struct field caching, duplicate key detection infrastructure |
| `tags.go` | Struct tag parsing ("bonjson" tag, fallback to "json" tag) |
| `fold.go` | Case-insensitive field name matching via Unicode SimpleFold |
| `errors.go` | All error types with byte offset tracking |
| `cmd/bonjson-test/main.go` | Cross-implementation test runner CLI |

### Test Files

| File | Coverage |
|------|----------|
| `decode_test.go` | Decoding, type coercion, struct fields, errors |
| `encode_test.go` | Encoding, collections, structs, custom types, big numbers |
| `stream_test.go` | Streaming encoder/decoder, Token API, concatenated documents |
| `wire_test.go` | Integers, floats, BigNumber encoding (signed length + LE magnitude), zigzag LEB128 |
| `security_test.go` | Duplicate keys, UTF-8, NUL, NaN/Infinity, configurable limits |
| `security_attack_test.go` | DoS attack scenarios (deep nesting, large payloads, malformed data) |
| `bench_test.go` | Performance comparisons vs encoding/json |


## How Encoding Works

### Flow
1. `Marshal(v)` gets an `encodeState` from sync.Pool
2. `marshal()` analyzes type tree for record candidates, writes definitions, then uses panic/recover for error propagation
3. `reflectValue()` dispatches to type-specific encoder via `valueEncoder()`
4. Encoder cache (sync.Map) avoids repeated reflection for custom types

### Type-Specific Encoding
- **Small integers** (0 to 100): Single byte (type code = value, so 0x00-0x64)
- **Large integers**: Type code + native-size bytes (1, 2, 4, or 8) little-endian
- **Floats**: Auto-selects float32 (5 bytes) or float64 (9 bytes)
- **Short strings** (0-66 bytes): Type code encodes length (0x65-0xA7)
- **Long strings**: 0xFF + raw data bytes + 0xFF (delimiter-terminated)
- **Typed arrays**: Homogeneous numeric slices/arrays → type code + LEB128 count + packed LE data
- **Regular arrays**: Type code (0xB7) + values + 0xB6 end marker (for non-numeric or custom-marshaled elements)
- **Objects**: Type code (0xB8) + key-value pairs + 0xB6 end marker
- **Records**: Struct types in slices/arrays emit record definitions + instances (eliminates repeated keys)
- **Maps**: Sorted by key for deterministic output
- **Structs**: Uses cached field metadata, respects omitempty/omitzero

### Smart Encoding Features (automatic)

**Typed Arrays**: Slices and arrays of primitive numeric types (`int8`..`int64`, `uint16`..`uint64`, `float32`, `float64`, `int`, `uint`) are encoded as typed arrays. This eliminates per-element type codes, packing data contiguously. `[]byte` remains base64-encoded; `[N]byte` fixed arrays use typed uint8 arrays. Types with custom `Marshaler`/`TextMarshaler` fall back to regular arrays.

**Records**: When `Marshal()` or `Encoder.Encode()` encounters struct types as elements of slices/arrays, it automatically:
1. Analyzes the type tree (cached per root type) to find eligible struct types
2. Writes record definitions (key names) before the root value
3. Encodes struct instances as record instances (positional values, no repeated keys)
- Trailing null values from omitted fields are elided
- Structs with custom `Marshaler`/`TextMarshaler` are excluded
- Top-level structs (not in slices/arrays) remain regular objects

### Custom Type Priority
1. `Marshaler` interface (`MarshalBONJSON() ([]byte, error)`)
2. `encoding.TextMarshaler` interface
3. Special handling for `*big.Int`, `*big.Float`


## How Decoding Works

### Flow
1. `Unmarshal(data, v)` gets a `decodeState` from sync.Pool
2. `decodeValue()` reads type code and dispatches to type-specific handler
3. `dereferenceAndGetUnmarshaler()` walks pointers, checks for Unmarshaler
4. Validation occurs during string storage (UTF-8, NUL checks)

### Security Validation (all enabled by default)
- **Duplicate keys**: Tracked via `seenFields` boolean slice for structs, MapIndex check for maps
- **UTF-8**: `utf8.Valid()` fast path, byte-by-byte scan for error position
- **NUL characters**: `bytes.IndexByte()` check (configurable via `AllowNUL()`)
- **NaN/Infinity**: Rejected in float decoders (BigNumber cannot represent these values)

### Configurable Limits (on Decoder)
All defaults follow the BONJSON spec "Resource Limits" table:
- `SetMaxDocumentSize(n)` - default 2 GB (2,000,000,000 bytes)
- `SetMaxDepth(n)` - default 500
- `SetMaxContainerSize(n)` - default 1,000,000 elements
- `SetMaxStringLength(n)` - default 10 MB (10,000,000 bytes)
- `SetMaxBigNumberMagnitude(n)` - default 256 bytes (~617 decimal digits)
- `SetMaxBigNumberExponent(n)` - default ±100,000
- `AllowNUL()` - allow NUL characters in strings
- `DisallowUnknownFields()` - reject unknown struct fields
- `SetOutOfRangeMode(mode)` - stringify out-of-range BigNumbers instead of error
- `SetUnicodeNormalizationMode(mode)` - apply NFC normalization to decoded strings

### Security Behavior Options (on Decoder)

**Invalid UTF-8 Handling** (`SetInvalidUTF8Mode`):
- `UTF8Reject` (default) - return error on invalid UTF-8
- `UTF8Replace` - replace invalid bytes with U+FFFD (modifies data)
- `UTF8Delete` - remove invalid bytes (modifies data, changes length)
- `UTF8Ignore` - skip validation, pass through raw bytes

**Duplicate Key Handling** (`SetDuplicateKeyMode`):
- `DupKeyReject` (default) - return error on duplicate keys
- `DupKeyKeepFirst` - keep first value, silently ignore duplicates
- `DupKeyKeepLast` - replace with latest value (DANGEROUS - see warning in docs)

**NaN/Infinity Handling** (`SetNaNInfinityMode`):
- `NaNInfReject` (default) - return error on NaN/Infinity (JSON compatible)
- `NaNInfAllow` - allow NaN/Infinity as float values (breaks JSON compatibility)
- `NaNInfStringify` - convert to string representations ("NaN", "Infinity", "-Infinity")

**Out-of-Range BigNumber Handling** (`SetOutOfRangeMode`):
- `OutOfRangeReject` (default) - return error when BigNumber exceeds limits
- `OutOfRangeStringify` - convert to string (e.g. "1e6", "-15e5")

**Unicode Normalization** (`SetUnicodeNormalizationMode`):
- `UnicodeNormNone` (default) - no normalization
- `UnicodeNormNFC` - apply NFC normalization to all decoded strings and object keys


## Wire Format

### Type Codes
```
0x00-0x64  Small integers (0 to 100, type code = value)
0x65-0xA7  Short strings (0-66 bytes, length = tc - 0x65)
0xA8-0xAB  Unsigned integers (native sizes: 1, 2, 4, 8 bytes)
0xAC-0xAF  Signed integers (native sizes: 1, 2, 4, 8 bytes)
0xB0       Float32
0xB1       Float64
0xB2       BigNumber (zigzag LEB128 exponent + zigzag LEB128 signed_length + LE magnitude)
0xB3       Null
0xB4       False
0xB5       True
0xB6       Container end marker
0xB7       Array (delimiter-terminated)
0xB8       Object (delimiter-terminated)
0xB9       Record definition (string keys + end marker)
0xBA       Record instance (LEB128 def_index + values + end marker)
0xBB-0xF4  Reserved
0xF5-0xFE  Typed arrays (LEB128 count + packed element data)
0xFF       Long string (0xFF + data + 0xFF)
```

Type code detection:
- Small integers: `tc <= 0x64`, value = `tc`
- Short strings: `tc >= 0x65 && tc <= 0xA7`, length = `tc - 0x65`
- Unsigned integers: `tc&0xFC == 0xA8`, size index = `tc&0x03`, byte count = `1 << sizeIndex`
- Signed integers: `tc&0xFC == 0xAC`, size index = `tc&0x03`, byte count = `1 << sizeIndex`

### Delimiter-Terminated Containers
Arrays and objects use end-marker termination:
- Array: 0xB7 + values + 0xB6
- Object: 0xB8 + key-value pairs + 0xB6
- This enables streaming without knowing element count up front

### Long Strings
Long strings (>66 bytes) use delimiter termination:
- 0xFF + raw data bytes + 0xFF
- The data bytes themselves cannot contain 0xFF (strings are UTF-8, and 0xFF is not valid UTF-8)
- UTF-8 validation happens on the complete string

### BigNumber Format
Encoded as 0xB2 + zigzag LEB128 exponent (base-10) + zigzag LEB128 signed_length + LE magnitude bytes.
- **exponent**: zigzag LEB128 signed integer, base-10 exponent
- **signed_length**: zigzag LEB128 signed integer encoding both sign and byte count of magnitude.
  Positive = positive significand, negative = negative significand, zero = significand is zero (no magnitude bytes)
- **magnitude_bytes**: unsigned little-endian integer, exactly abs(signed_length) bytes, normalized (last byte non-zero)
- Value = sign(signed_length) × magnitude × 10^exponent
- No special value encoding; NaN and Infinity are not representable in BigNumber

### Typed Arrays
Format: `[type_code] [element_count (LEB128)] [data bytes...]`
- Type codes 0xF5-0xFE map to element types (float64, float32, int64..int8, uint64..uint8)
- Data is packed contiguously in little-endian byte order
- Semantically identical to a regular array of numbers

### Records
Record definitions (0xB9) appear at the start of a document before the root value.
Each definition declares a list of key strings terminated by 0xB6.
Record instances (0xBA) reference a definition by LEB128 index and supply values
positionally matched to the keys. Semantically identical to objects.


## Struct Field Caching

### Two-Level Cache
1. **fieldCache** (sync.Map): reflect.Type → structFields
2. **encoderCache** (sync.Map): reflect.Type → encoderFunc

### Field Extraction Process
1. BFS through struct fields (handles embedded fields, Go visibility rules)
2. Parse tags ("bonjson" first, fallback to "json")
3. Sort by name → depth → tag presence → field index
4. Apply Go's field shadowing rules
5. Assign `seenIndex` for O(1) duplicate detection
6. Pre-compute case-folded names

### Duplicate Key Detection
- Boolean slice indexed by `seenIndex` (faster than map)
- Stack of slices for nested struct decoding (`seenStructFieldsStack`)


## Design Decisions

### Why panic/recover for encoding errors?
Follows `encoding/json` pattern. Allows clean recursive encoding without threading errors through every call. Panic is caught at top level and converted to error return.

### Why sorted map keys?
Deterministic output is important for testing, caching, and comparing encoded values. The overhead is acceptable for correctness.

### Why sync.Pool for encode/decode state?
High-frequency operations benefit from object reuse. The state objects contain buffers that grow to accommodate typical workloads.

### Why linear search for struct fields?
For typical structs (<20 fields), linear search beats hash map lookup. The fields slice is compact in memory and has good cache locality.


## Security Model

### Default Behavior (secure)
- Reject duplicate object keys
- Reject invalid UTF-8 in strings
- Reject NUL characters in strings
- Reject NaN and Infinity values
- Enforce depth, string length, and container size limits

### Configurable Relaxations
- `AllowNUL()` - permit NUL characters
- `SetMaxStringLength(0)` - unlimited string length (not recommended)
- `SetMaxDepth(0)` - unlimited nesting (not recommended)
- `SetNaNInfinityMode(NaNInfAllow)` - allow NaN/Infinity as float values
- `SetNaNInfinityMode(NaNInfStringify)` - convert NaN/Infinity to strings

Note: Both Encoder and Decoder support `SetNaNInfinityMode()` and `AllowNUL()` for consistent handling.


## Common Patterns

### Round-trip Testing
```go
original := SomeStruct{...}
data, err := bonjson.Marshal(original)
var decoded SomeStruct
err = bonjson.Unmarshal(data, &decoded)
// Compare original and decoded
```

### Streaming Multiple Documents
```go
enc := bonjson.NewEncoder(writer)
for _, v := range values {
    enc.Encode(v)
}

dec := bonjson.NewDecoder(reader)
for {
    var v SomeType
    if err := dec.Decode(&v); err == io.EOF {
        break
    }
}
```

### Custom Marshaling
```go
func (t *MyType) MarshalBONJSON() ([]byte, error) {
    // Return raw BONJSON bytes
}

func (t *MyType) UnmarshalBONJSON(data []byte) error {
    // Parse raw BONJSON bytes
}
```


## Specification Conformance Tests

The test runner runs BONJSON universal test specification files as Go tests. This enables running cross-implementation test suites to verify correct encoding/decoding behavior. Test results count towards code coverage.

### Usage

```bash
# Run conformance tests
go test -v -run TestBONJSONSpec

# Run test runner validation tests
go test -v -run TestRunnerValidation

# Run validation function unit tests
go test -v -run TestValidationFunctions
```

### Test Organization

- `spec_test.go` - Test runner implementation
- `specification/tests/conformance/` - Codec conformance tests
- `specification/tests/test-runner-validation/` - Test runner validation tests

### Supported Test Types

| Type | Description |
|------|-------------|
| `encode` | Verify encoding produces specific bytes |
| `decode` | Verify decoding produces specific value |
| `roundtrip` | Verify value survives encode→decode |
| `encode_error` | Verify encoding fails with specific error |
| `decode_error` | Verify decoding fails with specific error |

### Supported Options

| Option | Type | Description |
|--------|------|-------------|
| `allow_nul` | boolean | Allow NUL characters in strings |
| `allow_trailing_bytes` | boolean | Allow unconsumed bytes after decoding |
| `nan_infinity_behavior` | string | NaN/Infinity handling: `reject`, `allow`, `stringify` |
| `duplicate_key` | string | Duplicate key handling: `reject`, `keep_first`, `keep_last` |
| `invalid_utf8` | string | Invalid UTF-8 handling: `reject`, `replace`, `delete`, `ignore`, `pass_through` |
| `max_depth` | integer | Maximum container nesting depth |
| `max_string_length` | integer | Maximum string length in bytes |
| `max_container_size` | integer | Maximum elements per container |
| `max_document_size` | integer | Maximum document size in bytes |
| `max_bignumber_magnitude` | integer | Maximum byte length of BigNumber magnitude |
| `max_bignumber_exponent` | integer | Maximum absolute value of BigNumber exponent |
| `out_of_range` | string | Out-of-range BigNumber handling: `error`, `stringify` |
| `unicode_normalization` | string | Unicode normalization: `none`, `nfc` |

### Error Type Mapping

The runner recognizes these standardized error types:

| Error Type | Description |
|------------|-------------|
| `truncated` | Unexpected end of input data |
| `trailing_bytes` | Unconsumed bytes after decoding |
| `invalid_type_code` | Unrecognized or reserved type code |
| `invalid_utf8` | Invalid UTF-8 byte sequence |
| `nul_character` / `nul_in_string` | NUL (U+0000) byte in string |
| `duplicate_key` | Duplicate key in object |
| `unclosed_container` | Missing container end marker |
| `invalid_data` | Generic invalid data |
| `invalid_object_key` | Non-string key in object |
| `value_out_of_range` | Value exceeds allowed range |
| `max_depth_exceeded` | Container nesting too deep |
| `max_string_length_exceeded` | String exceeds length limit |
| `max_container_size_exceeded` | Container has too many elements |
| `max_document_size_exceeded` | Document exceeds size limit |
| `nan_not_allowed` | NaN value when not allowed |
| `infinity_not_allowed` | Infinity value when not allowed |
| `max_bignumber_magnitude_exceeded` | BigNumber magnitude exceeds byte length limit |
| `max_bignumber_exponent_exceeded` | BigNumber exponent exceeds absolute value limit |

### Marker Objects

#### $number Marker

Special numeric values use the `$number` marker:
- `{"$number": "NaN"}` - IEEE 754 NaN
- `{"$number": "Infinity"}` - positive infinity
- `{"$number": "-Infinity"}` - negative infinity
- `{"$number": "18446744073709551615"}` - large integers
- `{"$number": "0x1.921fb54442d18p+1"}` - hex floats (for exact bit patterns)

#### $bytes Marker

Raw byte sequences for invalid UTF-8 testing use the `$bytes` marker:
- `{"$bytes": "68 65 6c 6c 6f ff 77 6f 72 6c 64"}` - raw bytes with invalid UTF-8

Tests using `$bytes` require the `raw_string_bytes` capability, which go-bonjson does not support (Go strings must be valid UTF-8). These tests are automatically skipped.

### Test Runner Validation

The test runner validates test files according to the BONJSON Universal Test Specification:

- **Test name validation**: Must start with letter, contain only letters/digits/underscores
- **Duplicate detection**: Test names checked case-insensitively
- **Semver validation**: Version field must be valid semantic versioning
- **Option validation**: Options checked for correct types (boolean/integer/string enum)
- **String enum validation**: String options validated against allowed values
- **Required fields**: Each test type has required fields that must be present
- **Marker validation**: `$number` and `$bytes` markers validated for format and no extra keys
- **Hex validation**: Hex strings must have even digits, valid characters only

Tests with unrecognized options or error types are skipped with a warning (not structural errors).

### Encoder Limitations

The encoder does not support all options that the decoder supports:
- `max_depth`, `max_string_length`, etc. - Only supported on decoder

Tests requiring these encoder options are skipped.

### Encoding Differences

go-bonjson makes valid encoding choices that may differ from test expectations:
- **Error types**: Reports `truncated` instead of `unclosed_container` when data ends unexpectedly (both indicate the same underlying problem).

### Capability Flags

Some tests require capabilities not all implementations support. Tests declare required capabilities via the `requires` field. The test runner skips tests requiring unsupported capabilities.

| Capability | Supported | Notes |
|------------|-----------|-------|
| `arbitrary_precision_bignumber` | Yes | BigNumber with >17 significant digits. When decoding to `interface{}`, go-bonjson uses `*big.Int` or `*big.Float` when primitives would lose precision. |
| `bignumber_exponent_gt_127` | Yes | BigNumber exponents > 127 (uses 2-3 byte exponent encoding) |
| `bignumber_exponent_lt_neg128` | Yes | BigNumber exponents < -128 (uses 2-3 byte exponent encoding) |
| `nan_infinity_reject` | Yes | NaN/Infinity rejected by default in float32/float64 values |
| `nan_infinity_stringify` | Yes | Converting NaN/Infinity to string representations (`NaNInfStringify` mode) |
| `out_of_range_stringify` | Yes | Converting out-of-range BigNumbers to string representations (`OutOfRangeStringify` mode) |


## Potential Improvements

Areas that could be enhanced:
- Noncharacter codepoint rejection option (U+FDD0-U+FDEF, U+xFFFE, U+xFFFF)
  - Go's utf8.Valid() accepts these; would need custom validation
