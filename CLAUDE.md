# CLAUDE.md - go-bonjson

## Overview

This is a Go implementation of BONJSON, a binary JSON-compatible format. It provides a drop-in replacement for `encoding/json` with better performance, smaller size, and enhanced security.

**Key advantages over JSON:**
- 2-15x faster encoding/decoding
- 25-75% smaller payload size
- Native integer type preservation (int64/uint64 instead of float64)
- Security-first defaults (duplicate key rejection, UTF-8 validation, NUL rejection)


## Build and Test Commands

```bash
go test              # Run all tests
go test -v           # Verbose output
go test -bench .     # Run benchmarks
go test -cover       # Coverage report
```


## Architecture

### Source Files

| File | Purpose |
|------|---------|
| `bonjson.go` | Package entry point, `Valid()` function |
| `encode.go` | `Marshal()`, `AppendMarshal()`, `Marshaler` interface |
| `decode.go` | `Unmarshal()`, `UnmarshalWithByteCount()`, `Unmarshaler` interface |
| `stream.go` | `Encoder`/`Decoder` for streaming I/O, `RawMessage`, Token API |
| `wire.go` | Low-level binary encoding: length fields, integers, floats, strings, BigNumber |
| `types.go` | Wire format type codes and security limit constants |
| `fields.go` | Struct field caching, duplicate key detection infrastructure |
| `tags.go` | Struct tag parsing ("bonjson" tag, fallback to "json" tag) |
| `fold.go` | Case-insensitive field name matching via Unicode SimpleFold |
| `errors.go` | All error types with byte offset tracking |

### Test Files

| File | Coverage |
|------|----------|
| `decode_test.go` | Decoding, type coercion, struct fields, errors |
| `encode_test.go` | Encoding, collections, structs, custom types, big numbers |
| `stream_test.go` | Streaming encoder/decoder, Token API, concatenated documents |
| `wire_test.go` | Length fields, integers, floats, BigNumber encoding |
| `security_test.go` | Duplicate keys, UTF-8, NUL, NaN/Infinity, configurable limits |
| `security_attack_test.go` | DoS attack scenarios (deep nesting, large payloads, chunk bombs) |
| `bench_test.go` | Performance comparisons vs encoding/json |


## How Encoding Works

### Flow
1. `Marshal(v)` gets an `encodeState` from sync.Pool
2. `marshal()` uses panic/recover for error propagation
3. `reflectValue()` dispatches to type-specific encoder via `valueEncoder()`
4. Encoder cache (sync.Map) avoids repeated reflection for custom types

### Type-Specific Encoding
- **Small integers** (-100 to +100): Single byte (type code = value)
- **Large integers**: Type code + 1-8 bytes little-endian
- **Floats**: Auto-selects bfloat16 (3 bytes), float32 (5 bytes), or float64 (9 bytes)
- **Short strings** (0-15 bytes): Type code encodes length (0x80-0x8f)
- **Long strings**: Header (0x68) + length field + data
- **Maps**: Sorted by key for deterministic output
- **Structs**: Uses cached field metadata, respects omitempty/omitzero

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
- **NaN/Infinity**: Rejected in float decoders and BigNumber special values

### Configurable Limits (on Decoder)
- `SetMaxStringLength(n)` - default 10 MB
- `SetMaxDepth(n)` - default 1000
- `SetMaxChunks(n)` - default 100
- `AllowNUL()` - allow NUL characters in strings
- `DisallowUnknownFields()` - reject unknown struct fields

### Security Behavior Options (on Decoder)

**Invalid UTF-8 Handling** (`SetInvalidUTF8Mode`):
- `UTF8Reject` (default) - return error on invalid UTF-8
- `UTF8Replace` - replace invalid bytes with U+FFFD (modifies data)
- `UTF8Delete` - remove invalid bytes (modifies data, changes length)
- `UTF8Ignore` - skip validation, pass through raw bytes

**Duplicate Key Handling** (`SetDuplicateKeyMode`):
- `DupKeyReject` (default) - return error on duplicate keys
- `DupKeyKeepFirst` - keep first value, silently ignore duplicates
- `DupKeyReplace` - replace with latest value (DANGEROUS - see warning in docs)

**NaN/Infinity Handling** (`SetNaNInfinityMode`):
- `NaNInfReject` (default) - return error on NaN/Infinity (JSON compatible)
- `NaNInfAllow` - allow NaN/Infinity as float values (breaks JSON compatibility)
- `NaNInfStringify` - convert to string representations ("NaN", "Infinity", "-Infinity")

Note: `AllowNaNInfinity()` is a convenience method equivalent to `SetNaNInfinityMode(NaNInfAllow)`.


## Wire Format

### Type Codes
```
0x00-0x64  Small positive integers (0-100)
0x68       Long string
0x69       BigNumber
0x6a       Float16 (bfloat16)
0x6b       Float32
0x6c       Float64
0x6d       Null
0x6e       False
0x6f       True
0x70-0x77  Unsigned integers (1-8 bytes)
0x78-0x7f  Signed integers (1-8 bytes)
0x80-0x8f  Short strings (0-15 bytes)
0x99       Array start
0x9a       Object start
0x9b       Container end
0x9c-0xff  Small negative integers (-100 to -1)
```

### Length Field Encoding
Variable-length encoding using continuation bits:
- Single byte: payload fits in 7 bits
- Multi-byte: trailing zeros count determines byte count
- 9-byte: header 0x00 + 8-byte little-endian payload

### BigNumber Format
Header byte: `[sig_len:5][exp_len:2][negative:1]`
- Significand: 0-31 bytes little-endian
- Exponent: 0, 1, 2, or 4 bytes (base-10)
- Special values (sigLen=0): zero, infinity (rejected), NaN (rejected)


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
- Enforce depth, string length, and chunk limits

### Configurable Relaxations
- `AllowNUL()` - permit NUL characters
- `SetMaxStringLength(0)` - unlimited string length (not recommended)
- `SetMaxDepth(0)` - unlimited nesting (not recommended)
- `SetMaxChunks(0)` - unlimited chunks (not recommended)
- `SetNaNInfinityMode(NaNInfAllow)` - allow NaN/Infinity as float values
- `SetNaNInfinityMode(NaNInfStringify)` - convert NaN/Infinity to strings

Note: Both Encoder and Decoder support `SetNaNInfinityMode()` for consistent handling.


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


## Potential Improvements

Areas that could be enhanced:
- Noncharacter codepoint rejection option (U+FDD0-U+FDEF, U+xFFFE, U+xFFFF)
  - Go's utf8.Valid() accepts these; would need custom validation
