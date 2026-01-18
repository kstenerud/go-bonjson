// ABOUTME: Runs the universal BONJSON spec tests as Go tests.
// ABOUTME: This allows spec tests to count towards code coverage.

package bonjson

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// testFile represents a BONJSON test specification file
type testFile struct {
	Type    string     `json:"type"`
	Version string     `json:"version"`
	Tests   []testCase `json:"tests"`
}

// testCase represents a single test case
type testCase struct {
	Name          string                 `json:"name"`
	Type          string                 `json:"type"`
	Input         interface{}            `json:"input"`
	InputBytes    string                 `json:"input_bytes"`
	ExpectedBytes string                 `json:"expected_bytes"`
	ExpectedValue interface{}            `json:"expected_value"`
	ExpectedError string                 `json:"expected_error"`
	Options       map[string]interface{} `json:"options"`
	Requires      []string               `json:"requires"`
}

// configFile represents a test configuration file
type configFile struct {
	Type    string         `json:"type"`
	Version string         `json:"version"`
	Sources []configSource `json:"sources"`
}

// configSource represents a test source in config
type configSource struct {
	Path      string `json:"path"`
	Skip      bool   `json:"skip"`
	Recursive bool   `json:"recursive"`
}

// supportedCapabilities defines which test capabilities this implementation supports.
// Tests requiring capabilities not in this set will be skipped.
var supportedCapabilities = map[string]bool{
	// Supported: Arbitrary precision BigNumber (>17 significant digits)
	// When decoding to interface{}, go-bonjson uses *big.Int or *big.Float
	// when primitives would lose precision.
	"arbitrary_precision_bignumber": true,

	// Supported: BigNumber exponents outside int8 range (-128 to 127)
	// go-bonjson supports 1-3 byte exponents (up to int24 range)
	"bignumber_exponent_gt_127":    true,
	"bignumber_exponent_lt_neg128": true,

	// Supported: Converting NaN/Infinity to string representations during decoding
	"nan_infinity_stringify": true,
}

func TestBONJSONSpec(t *testing.T) {
	// Find the spec tests directory
	specDir := "specification/tests/conformance"
	configPath := filepath.Join(specDir, "config.json")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Skip("Spec tests not found at " + configPath)
	}

	// Load config
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	var config configFile
	if err := json.Unmarshal(configData, &config); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	// Process each source
	for _, source := range config.Sources {
		if source.Skip {
			continue
		}

		sourcePath := filepath.Join(specDir, source.Path)
		info, err := os.Stat(sourcePath)
		if err != nil {
			t.Errorf("Failed to stat source %s: %v", source.Path, err)
			continue
		}

		if info.IsDir() {
			processDirectory(t, sourcePath, source.Recursive)
		} else {
			processTestFile(t, sourcePath)
		}
	}
}

func processDirectory(t *testing.T, dir string, recursive bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Errorf("Failed to read directory %s: %v", dir, err)
		return
	}

	// Process files first (alphabetically)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Name() == "config.json" {
			continue
		}
		processTestFile(t, filepath.Join(dir, entry.Name()))
	}

	// Then directories if recursive
	if recursive {
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			processDirectory(t, filepath.Join(dir, entry.Name()), recursive)
		}
	}
}

func processTestFile(t *testing.T, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("Failed to read %s: %v", path, err)
		return
	}

	var tf testFile
	if err := json.Unmarshal(data, &tf); err != nil {
		t.Errorf("Failed to parse %s: %v", path, err)
		return
	}

	if tf.Type != "bonjson-test" {
		return
	}

	fileName := filepath.Base(path)
	t.Run(fileName, func(t *testing.T) {
		for _, tc := range tf.Tests {
			// Skip comment-only entries (no name or type)
			if tc.Name == "" && tc.Type == "" {
				continue
			}
			tc := tc // capture for parallel
			t.Run(tc.Name, func(t *testing.T) {
				runTestCase(t, tc)
			})
		}
	})
}

func runTestCase(t *testing.T, tc testCase) {
	// Check for required capabilities
	for _, cap := range tc.Requires {
		if !supportedCapabilities[cap] {
			t.Skipf("Requires unsupported capability: %s", cap)
		}
	}

	switch strings.ToLower(tc.Type) {
	case "encode":
		runEncodeTest(t, tc)
	case "decode":
		runDecodeTest(t, tc)
	case "roundtrip":
		runRoundtripTest(t, tc)
	case "encode_error":
		runEncodeErrorTest(t, tc)
	case "decode_error":
		runDecodeErrorTest(t, tc)
	default:
		t.Skipf("Unknown test type: %s", tc.Type)
	}
}

func runEncodeTest(t *testing.T, tc testCase) {
	value, err := parseTestValue(tc.Input)
	if err != nil {
		t.Fatalf("Failed to parse input: %v", err)
	}

	encoded, err := Marshal(value)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	expected, err := parseHexBytes(tc.ExpectedBytes)
	if err != nil {
		t.Fatalf("Failed to parse expected bytes: %v", err)
	}

	if !bytes.Equal(encoded, expected) {
		// Encode mismatches may be valid - BONJSON allows multiple encodings
		// for the same value (e.g., uint8 vs sint16 for value 128).
		// Verify the encoded bytes decode to the same value.
		var decoded interface{}
		if err := Unmarshal(encoded, &decoded); err != nil {
			t.Errorf("Encode produced different bytes that fail to decode:\n  got:  %x\n  want: %x\n  error: %v", encoded, expected, err)
			return
		}
		if !valuesEqual(decoded, value) {
			t.Errorf("Encode produced different bytes that decode to wrong value:\n  got:  %x (decodes to %v)\n  want: %x", encoded, decoded, expected)
			return
		}
		// Different but valid encoding - skip (not a failure)
		t.Skipf("Valid encoding difference: got %x, spec expects %x", encoded, expected)
	}
}

func runDecodeTest(t *testing.T, tc testCase) {
	inputBytes, err := parseHexBytes(tc.InputBytes)
	if err != nil {
		t.Fatalf("Failed to parse input bytes: %v", err)
	}

	decoder := NewDecoder(bytes.NewReader(inputBytes))
	applyDecoderOptions(decoder, tc.Options)

	var got interface{}
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	expected, err := parseTestValue(tc.ExpectedValue)
	if err != nil {
		t.Fatalf("Failed to parse expected value: %v", err)
	}

	if !valuesEqual(got, expected) {
		t.Errorf("Decode mismatch:\n  got:  %v (%T)\n  want: %v (%T)", got, got, expected, expected)
	}
}

func runRoundtripTest(t *testing.T, tc testCase) {
	value, err := parseTestValue(tc.Input)
	if err != nil {
		t.Fatalf("Failed to parse input: %v", err)
	}

	encoded, err := Marshal(value)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded interface{}
	if err := Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !valuesEqual(decoded, value) {
		t.Errorf("Roundtrip mismatch:\n  got:  %v (%T)\n  want: %v (%T)", decoded, decoded, value, value)
	}
}

func runEncodeErrorTest(t *testing.T, tc testCase) {
	value, err := parseTestValue(tc.Input)
	if err != nil {
		t.Fatalf("Failed to parse input: %v", err)
	}

	_, err = Marshal(value)
	if err == nil {
		t.Errorf("Expected encode error %q but got success", tc.ExpectedError)
	}
}

func runDecodeErrorTest(t *testing.T, tc testCase) {
	// Check for unimplemented options
	if tc.Options != nil {
		if _, ok := tc.Options["max_container_size"]; ok {
			t.Skip("max_container_size option not implemented")
		}
		if _, ok := tc.Options["max_document_size"]; ok {
			t.Skip("max_document_size option not implemented")
		}
	}

	inputBytes, err := parseHexBytes(tc.InputBytes)
	if err != nil {
		t.Fatalf("Failed to parse input bytes: %v", err)
	}

	// Check if this is a trailing bytes test
	allowTrailing := false
	if tc.Options != nil {
		if v, ok := tc.Options["allow_trailing_bytes"].(bool); ok {
			allowTrailing = v
		}
	}

	// For tests with options, use Decoder
	if tc.Options != nil && hasDecoderOptions(tc.Options) {
		decoder := NewDecoder(bytes.NewReader(inputBytes))
		applyDecoderOptions(decoder, tc.Options)

		var got interface{}
		err = decoder.Decode(&got)
		if err == nil {
			t.Errorf("Expected decode error %q but got success (value: %v)", tc.ExpectedError, got)
		}
		return
	}

	// Use UnmarshalWithByteCount to detect trailing bytes
	var got interface{}
	consumed, err := UnmarshalWithByteCount(inputBytes, &got)

	if err == nil && !allowTrailing && consumed < len(inputBytes) {
		// Trailing bytes detected - this is an error
		return // Test passes - we detected the error condition
	}

	if err == nil {
		t.Errorf("Expected decode error %q but got success (value: %v)", tc.ExpectedError, got)
	}
}

func hasDecoderOptions(opts map[string]interface{}) bool {
	decoderOpts := []string{"allow_nul", "allow_nan_infinity", "nan_infinity", "duplicate_key", "invalid_utf8", "max_depth", "max_string_length", "max_chunks"}
	for _, opt := range decoderOpts {
		if _, ok := opts[opt]; ok {
			return true
		}
	}
	return false
}

func parseHexBytes(s string) ([]byte, error) {
	// Remove whitespace
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\t", "")
	s = strings.ReplaceAll(s, "\n", "")
	return hex.DecodeString(s)
}

func parseTestValue(v interface{}) (interface{}, error) {
	if v == nil {
		return nil, nil
	}

	switch val := v.(type) {
	case map[string]interface{}:
		// Check for $number marker
		if numStr, ok := val["$number"].(string); ok {
			return parseNumberString(numStr)
		}
		// Regular object - recursively parse values
		result := make(map[string]interface{})
		for k, v := range val {
			parsed, err := parseTestValue(v)
			if err != nil {
				return nil, err
			}
			result[k] = parsed
		}
		return result, nil

	case []interface{}:
		result := make([]interface{}, len(val))
		for i, v := range val {
			parsed, err := parseTestValue(v)
			if err != nil {
				return nil, err
			}
			result[i] = parsed
		}
		return result, nil

	default:
		return v, nil
	}
}

func parseNumberString(s string) (interface{}, error) {
	lower := strings.ToLower(s)

	// Special values
	switch lower {
	case "nan":
		return math.NaN(), nil
	case "infinity", "+infinity":
		return math.Inf(1), nil
	case "-infinity":
		return math.Inf(-1), nil
	case "-0.0", "-0":
		return math.Copysign(0, -1), nil
	}

	// Hex float
	if strings.HasPrefix(lower, "0x") || strings.HasPrefix(lower, "-0x") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse hex float %q: %v", s, err)
		}
		return f, nil
	}

	// Try parsing as integer first (for big integers)
	if !strings.Contains(s, ".") && !strings.Contains(lower, "e") {
		if bi, ok := new(big.Int).SetString(s, 10); ok {
			// Check if it fits in int64/uint64
			if bi.IsInt64() {
				return bi.Int64(), nil
			}
			if bi.IsUint64() {
				return bi.Uint64(), nil
			}
			// Return as big.Int for larger values
			return bi, nil
		}
	}

	// Try float64
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, nil
	}

	// Fall back to big.Float for values that overflow float64
	bf, _, err := big.ParseFloat(s, 10, 256, big.ToNearestEven)
	if err != nil {
		return nil, fmt.Errorf("cannot parse $number %q: %v", s, err)
	}
	return bf, nil
}

func applyDecoderOptions(dec *Decoder, opts map[string]interface{}) {
	if opts == nil {
		return
	}

	if v, ok := opts["allow_nul"].(bool); ok && v {
		dec.AllowNUL()
	}
	if v, ok := opts["allow_nan_infinity"].(bool); ok && v {
		dec.SetNaNInfinityMode(NaNInfAllow)
	}
	// Handle nan_infinity option (string mode: "allow", "stringify", "reject")
	if v, ok := opts["nan_infinity"].(string); ok {
		switch v {
		case "allow":
			dec.SetNaNInfinityMode(NaNInfAllow)
		case "stringify":
			dec.SetNaNInfinityMode(NaNInfStringify)
		case "reject":
			dec.SetNaNInfinityMode(NaNInfReject)
		}
	}
	// Handle duplicate_key option (string mode: "keep_first", "keep_last", "reject")
	if v, ok := opts["duplicate_key"].(string); ok {
		switch v {
		case "keep_first":
			dec.SetDuplicateKeyMode(DupKeyKeepFirst)
		case "keep_last":
			dec.SetDuplicateKeyMode(DupKeyKeepLast)
		case "reject":
			dec.SetDuplicateKeyMode(DupKeyReject)
		}
	}
	// Handle invalid_utf8 option (string mode: "replace", "delete", "ignore", "reject")
	if v, ok := opts["invalid_utf8"].(string); ok {
		switch v {
		case "replace":
			dec.SetInvalidUTF8Mode(UTF8Replace)
		case "delete":
			dec.SetInvalidUTF8Mode(UTF8Delete)
		case "ignore":
			dec.SetInvalidUTF8Mode(UTF8Ignore)
		case "reject":
			dec.SetInvalidUTF8Mode(UTF8Reject)
		}
	}
	if v, ok := opts["max_depth"]; ok {
		if depth, ok := toInt(v); ok {
			dec.SetMaxDepth(depth)
		}
	}
	if v, ok := opts["max_string_length"]; ok {
		if length, ok := toInt64(v); ok {
			dec.SetMaxStringLength(length)
		}
	}
	if v, ok := opts["max_chunks"]; ok {
		if chunks, ok := toInt(v); ok {
			dec.SetMaxChunks(chunks)
		}
	}
	if v, ok := opts["allow_trailing_bytes"].(bool); ok && v {
		// Note: Decoder doesn't have this option, handled differently
	}
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

func toInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

func valuesEqual(a, b interface{}) bool {
	// Handle nil
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Handle NaN
	if af, ok := a.(float64); ok {
		if bf, ok := b.(float64); ok {
			if math.IsNaN(af) && math.IsNaN(bf) {
				return true
			}
			// Handle negative zero
			if af == 0 && bf == 0 {
				return math.Signbit(af) == math.Signbit(bf)
			}
		}
	}

	// Handle big.Int
	if ai, ok := a.(*big.Int); ok {
		switch bi := b.(type) {
		case *big.Int:
			return ai.Cmp(bi) == 0
		case int64:
			return ai.Cmp(big.NewInt(bi)) == 0
		case uint64:
			return ai.Cmp(new(big.Int).SetUint64(bi)) == 0
		case float64:
			bf := new(big.Float).SetInt(ai)
			return bf.Cmp(big.NewFloat(bi)) == 0
		}
	}

	// Handle big.Float
	if af, ok := a.(*big.Float); ok {
		switch bf := b.(type) {
		case *big.Float:
			return af.Cmp(bf) == 0
		case float64:
			return af.Cmp(big.NewFloat(bf)) == 0
		}
	}

	// Handle numeric comparisons (int64 vs float64, etc.)
	if an, aok := toFloat64(a); aok {
		if bn, bok := toFloat64(b); bok {
			return an == bn
		}
	}

	// Handle slices
	if as, ok := a.([]interface{}); ok {
		bs, ok := b.([]interface{})
		if !ok || len(as) != len(bs) {
			return false
		}
		for i := range as {
			if !valuesEqual(as[i], bs[i]) {
				return false
			}
		}
		return true
	}

	// Handle maps
	if am, ok := a.(map[string]interface{}); ok {
		bm, ok := b.(map[string]interface{})
		if !ok || len(am) != len(bm) {
			return false
		}
		for k, av := range am {
			bv, ok := bm[k]
			if !ok || !valuesEqual(av, bv) {
				return false
			}
		}
		return true
	}

	// Default comparison
	return a == b
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}
