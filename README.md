# go-bonjson

A high-performance Go library for encoding and decoding [BONJSON (Binary Object Notation for JSON)](https://github.com/kstenerud/bonjson), a binary format that is 1:1 compatible with JSON.

## Why BONJSON?

BONJSON provides significant advantages over text-based JSON:

| Feature            | BONJSON           | JSON                          |
|--------------------|-------------------|-------------------------------|
| **Parsing Speed**  | Up to 8x faster   |                               |
| **Size**           | More compact      | Verbose text                  |
| **Security**       | Strict by default | Permissive                    |
| **UTF-8**          | Rejects invalid   | Omits or replaces with U+FFFD |
| **Duplicate Keys** | Rejected          | Silently accepted             |

### Benchmark Comparison

| Test                           | Time/op (ns)  | Bytes/op  | Allocs/op    | Notes       |
|--------------------------------|---------------|-----------|--------------|-------------|
| UnmarshalInt_BONJSON           |         26.76 |         0 |            0 | 2.7x faster |
| UnmarshalInt_JSON              |         72.07 |       144 |            1 |             |
| MarshalFloat_BONJSON           |         36.41 |        16 |            1 | 2.3x faster |
| MarshalFloat_JSON              |         83.12 |        16 |            1 |             |
| UnmarshalFloat_BONJSON         |         29.68 |         0 |            0 | 4.1x faster |
| UnmarshalFloat_JSON            |        122.57 |       144 |            1 |             |
| MarshalLongString_BONJSON      |        240.37 |      1040 |            2 | 3.2x faster |
| MarshalLongString_JSON         |        766.40 |      1040 |            2 |             |
| UnmarshalLongString_BONJSON    |        502.00 |      1024 |            1 | 7.4x faster |
| UnmarshalLongString_JSON       |       3731.00 |      1168 |            2 |             |
| UnmarshalStruct_BONJSON        |        240.83 |        24 |            2 | 3.0x faster |
| UnmarshalStruct_JSON           |        724.63 |       240 |            6 |             |
| MarshalMap_BONJSON             |        324.53 |       144 |            2 | 2.0x faster |
| MarshalMap_JSON                |        651.30 |       448 |           12 |             |
| UnmarshalMap_BONJSON           |        140.30 |        88 |            6 | 7.9x faster |
| UnmarshalMap_JSON              |       1111.67 |       528 |           27 |             |

| Test                           | BONJSON bytes | JSON bytes | Ratio % |
|--------------------------------|---------------|------------|---------|
| EncodedSize/int_small          |             1 |          2 |  50.00  |
| EncodedSize/int_large          |             5 |          7 |  71.43  |
| EncodedSize/float              |             9 |         16 |  56.25  |
| EncodedSize/string_short       |             6 |          7 |  85.71  |
| EncodedSize/string_long        |           102 |        102 | 100.00  |
| EncodedSize/bool               |             1 |          4 |  25.00  |
| EncodedSize/slice_int          |            12 |         22 |  54.55  |
| EncodedSize/struct_medium      |            64 |         82 |  78.05  |
| EncodedSize/map_mixed          |            28 |         41 |  68.29  |

## Drop-in Replacement

This library is designed to be a drop-in replacement for `encoding/json`. Simply change your imports:

```go
// Before
import "encoding/json"

// After
import bonjson "github.com/kstenerud/go-bonjson"
```

The following APIs work identically:

- `Marshal`, `Unmarshal`, `Valid`
- `NewEncoder`, `NewDecoder`
- `Encoder.Encode`, `Decoder.Decode`
- `Decoder.DisallowUnknownFields`
- `Decoder.Buffered`, `Decoder.More`, `Decoder.Token`, `Decoder.InputOffset`
- `RawMessage`, `Delim`, `Token`
- All error types

**Note:** The following JSON-specific functions are not provided since they relate to text formatting or JSON's text-based number representation:

- `MarshalIndent`, `Compact`, `Indent`, `HTMLEscape`
- `Encoder.SetEscapeHTML`, `Encoder.SetIndent`
- `Decoder.UseNumber`, `Number` type (see Type Mapping below)

## Installation

```bash
go get github.com/kstenerud/go-bonjson
```

## Quick Start

### Basic Encoding and Decoding

```go
package main

import (
    "fmt"
    bonjson "github.com/kstenerud/go-bonjson"
)

type Person struct {
    Name  string `json:"name"`
    Age   int    `json:"age"`
    Email string `json:"email,omitempty"`
}

func main() {
    // Encode
    p := Person{Name: "Alice", Age: 30, Email: "alice@example.com"}
    data, err := bonjson.Marshal(p)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Encoded %d bytes\n", len(data))

    // Decode
    var p2 Person
    err = bonjson.Unmarshal(data, &p2)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Decoded: %+v\n", p2)
}
```

### Streaming with Encoder/Decoder

```go
package main

import (
    "bytes"
    bonjson "github.com/kstenerud/go-bonjson"
)

func main() {
    var buf bytes.Buffer

    // Encode multiple values
    enc := bonjson.NewEncoder(&buf)
    enc.Encode(map[string]any{"event": "login", "user": "alice"})
    enc.Encode(map[string]any{"event": "logout", "user": "alice"})

    // Decode multiple values
    dec := bonjson.NewDecoder(&buf)
    for dec.More() {
        var event map[string]any
        if err := dec.Decode(&event); err != nil {
            break
        }
        // Process event...
    }
}
```

### Using Struct Tags

BONJSON supports the same struct tags as `encoding/json`:

```go
type Example struct {
    Field1 string `json:"field1"`           // Rename to "field1"
    Field2 string `json:"field2,omitempty"` // Omit if empty
    Field3 string `json:"-"`                // Always omit
    Field4 int    `json:",string"`          // Encode as string
}
```

You can also use a `bonjson` tag to have different behavior for JSON and BONJSON:

```go
type Mixed struct {
    Field string `json:"json_name" bonjson:"bonjson_name"`
}
```

### Custom Marshaling

Implement `Marshaler` and `Unmarshaler` for custom encoding:

```go
type Marshaler interface {
    MarshalBONJSON() ([]byte, error)
}

type Unmarshaler interface {
    UnmarshalBONJSON([]byte) error
}
```

If these interfaces aren't implemented, the library falls back to `encoding.TextMarshaler` and `encoding.TextUnmarshaler`.

## Security Features

BONJSON enforces strict security rules by default:

| Rule                      | Default  | Option to Allow                      |
|---------------------------|----------|--------------------------------------|
| Duplicate object keys     | Rejected | `Decoder.SetDuplicateKeyMode()`      |
| Invalid UTF-8             | Rejected | `Decoder.SetInvalidUTF8Mode()`       |
| NUL characters in strings | Rejected | `Decoder.AllowNUL()`                 |
| NaN/Infinity              | Rejected | `Decoder.SetNaNInfinityMode()`       |

### Configurable Limits

```go
dec := bonjson.NewDecoder(r)
dec.SetMaxStringLength(1024 * 1024)  // 1 MB max string
dec.SetMaxDepth(100)                 // 100 levels max nesting
dec.SetMaxContainerSize(10000)       // 10000 elements max per container
```

## Type Mapping

BONJSON preserves numeric types more precisely than JSON:

| BONJSON          | Go (when decoding to `interface{}`)                   |
|------------------|-------------------------------------------------------|
| null             | nil                                                   |
| bool             | bool                                                  |
| signed integer   | int64                                                 |
| unsigned integer | uint64                                                |
| float            | float64                                               |
| BigNumber        | int64, uint64, float64, *big.Int, or *big.Float       |
| string           | string                                                |
| array            | []any                                                 |
| object           | map[string]any                                        |

BONJSON stores numbers in their native binary format, which means that `Decoder.UseNumber()` is not needed when decoding to `interface{}`. You get the correct and precise type automatically: `int64` for signed integers, `uint64` for unsigned integers, and `float64` for floats.

BigNumber values are decoded to the smallest type that can represent them without precision loss: primitives (`int64`, `uint64`, `float64`) when possible, otherwise `*big.Int` for large integers or `*big.Float` for high-precision decimals.

## Additional Features

### Efficient Buffer Reuse

```go
// AppendMarshal appends to an existing buffer, avoiding allocation
buf := make([]byte, 0, 1024)
buf, err := bonjson.AppendMarshal(buf, value)
```

### Raw Messages

Delay parsing or store pre-encoded data:

```go
type Response struct {
    Status string              `json:"status"`
    Data   bonjson.RawMessage `json:"data"`
}
```

### Big Numbers

BONJSON natively supports arbitrary-precision numbers via `*big.Int` and `*big.Float`:

```go
import "math/big"

bigNum := new(big.Int)
bigNum.SetString("123456789012345678901234567890", 10)

data, _ := bonjson.Marshal(bigNum)

var decoded *big.Int
bonjson.Unmarshal(data, &decoded)
```

## License

BSD-style license. See [LICENSE](LICENSE) for details.

## Related Projects

- [BONJSON Specification](https://github.com/kstenerud/bonjson)
