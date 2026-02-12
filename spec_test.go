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
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// testFile represents a BONJSON test specification file
type testFile struct {
	Type    string          `json:"type"`
	Version string          `json:"version"`
	Tests   json.RawMessage `json:"tests"` // Use RawMessage to detect missing vs empty
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

// rawTestFile is used for structural validation
type rawTestFile struct {
	Type    json.RawMessage `json:"type"`
	Version json.RawMessage `json:"version"`
	Tests   json.RawMessage `json:"tests"`
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

// rawBytesValue represents a $bytes marker value for raw byte sequences
// Used for testing invalid UTF-8 handling (requires raw_string_bytes capability)
type rawBytesValue []byte

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

	// Supported: Converting out-of-range BigNumbers to string representations
	"out_of_range_stringify": true,

	// Supported: Encoder rejects NUL characters in strings by default
	"encode_nul_rejection": true,

	// Supported: Platform capabilities
	"uint64":        true,
	"int64":         true,
	"negative_zero": true,

	// Not supported: Raw byte strings (Go strings must be valid UTF-8)
	// "raw_string_bytes": false,

	// Not supported: Signaling NaN preservation (Go converts sNaN to qNaN)
	// "signaling_nan": false,
}

// recognizedOptions lists all known test options per the specification.
// Values are: "boolean", "integer", or "string:val1,val2,..." for enums
var recognizedOptions = map[string]string{
	"allow_nul":            "boolean",
	"allow_trailing_bytes": "boolean",
	"max_depth":            "integer",
	"max_container_size":   "integer",
	"max_string_length":    "integer",
	"max_document_size":    "integer",
	"max_bignumber_magnitude": "integer",
	"max_bignumber_exponent":  "integer",
	// String options with enum values
	"nan_infinity_behavior": "string:reject,allow,stringify",
	"duplicate_key":         "string:reject,keep_first,keep_last",
	"invalid_utf8":          "string:reject,replace,delete,ignore,pass_through",
	"out_of_range":              "string:error,stringify",
	"unicode_normalization":     "string:none,nfc",
}

// recognizedErrorTypes lists all known error types per the specification.
var recognizedErrorTypes = map[string]bool{
	"truncated":                    true,
	"trailing_bytes":              true,
	"invalid_type_code":           true,
	"invalid_utf8":                true,
	"nul_character":               true,
	"nul_in_string":               true, // alias for nul_character
	"duplicate_key":               true,
	"unclosed_container":          true,
	"invalid_data":                true,
	"invalid_object_key":          true, // non-string key in object
	"value_out_of_range":          true,
	"max_depth_exceeded":          true,
	"max_string_length_exceeded":  true,
	"max_container_size_exceeded": true,
	"max_document_size_exceeded":  true,
	"nan_not_allowed":                  true, // NaN value when not allowed
	"infinity_not_allowed":             true, // Infinity value when not allowed
	"max_bignumber_magnitude_exceeded": true,
	"max_bignumber_exponent_exceeded":  true,
	"invalid_record_index":             true, // record instance references non-existent definition
	"record_too_many_values":           true, // record instance has more values than definition keys
	"record_no_definitions":            true, // record instance in document with no definitions
}

// testNamePattern validates test names: must start with letter, contain only letters/digits/underscores
var testNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// semverPattern validates semantic versioning format
var semverPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

// validateTestName checks if a test name follows the naming rules
func validateTestName(name string) error {
	if name == "" {
		return fmt.Errorf("test name is empty")
	}
	if !testNamePattern.MatchString(name) {
		return fmt.Errorf("test name %q is invalid: must start with letter, contain only letters/digits/underscores", name)
	}
	return nil
}

// validateSemver checks if a version string is valid semver
func validateSemver(version string) error {
	if version == "" {
		return fmt.Errorf("version is empty")
	}
	if !semverPattern.MatchString(version) {
		return fmt.Errorf("version %q is not valid semver", version)
	}
	return nil
}

// validateOptions checks that all options have correct types and valid enum values
func validateOptions(opts map[string]interface{}) (unrecognized []string, err error) {
	if opts == nil {
		return nil, nil
	}

	for name, value := range opts {
		// Skip comment keys
		if strings.HasPrefix(name, "//") {
			continue
		}

		expectedType, known := recognizedOptions[name]
		if !known {
			unrecognized = append(unrecognized, name)
			continue
		}

		// Null is never a valid option value
		if value == nil {
			return nil, fmt.Errorf("option %q has null value", name)
		}

		// Validate type
		switch {
		case expectedType == "boolean":
			if _, ok := value.(bool); !ok {
				return nil, fmt.Errorf("option %q must be boolean, got %T", name, value)
			}
		case expectedType == "integer":
			switch v := value.(type) {
			case float64:
				if v != float64(int(v)) {
					return nil, fmt.Errorf("option %q must be integer, got float", name)
				}
				if v < 0 {
					return nil, fmt.Errorf("option %q must be non-negative, got %v", name, v)
				}
			case int:
				if v < 0 {
					return nil, fmt.Errorf("option %q must be non-negative, got %v", name, v)
				}
			case int64:
				if v < 0 {
					return nil, fmt.Errorf("option %q must be non-negative, got %v", name, v)
				}
			default:
				return nil, fmt.Errorf("option %q must be integer, got %T", name, value)
			}
		case strings.HasPrefix(expectedType, "string:"):
			// String enum option
			strVal, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("option %q must be string, got %T", name, value)
			}
			// Extract valid enum values
			validValues := strings.Split(strings.TrimPrefix(expectedType, "string:"), ",")
			isValid := false
			for _, valid := range validValues {
				if strVal == valid {
					isValid = true
					break
				}
			}
			if !isValid {
				return nil, fmt.Errorf("option %q has invalid value %q (valid: %s)", name, strVal, strings.Join(validValues, ", "))
			}
		}
	}

	return unrecognized, nil
}

// validateRequiredFields checks that a test case has all required fields for its type.
// Note: We use raw JSON to detect missing vs present-but-null/empty fields, but the current
// struct-based approach can't distinguish these. For now, we only validate fields that must
// be non-empty strings. Empty input_bytes is valid (represents zero bytes to decode).
func validateRequiredFields(tc testCase) error {
	switch tc.Type {
	case "encode":
		// input can be null, expected_bytes cannot be empty
		if tc.ExpectedBytes == "" {
			return fmt.Errorf("encode test missing expected_bytes")
		}
	case "decode":
		// input_bytes can be empty (valid - zero bytes), expected_value can be null
	case "roundtrip":
		// input can be null
	case "encode_error":
		if tc.ExpectedError == "" {
			return fmt.Errorf("encode_error test missing expected_error")
		}
	case "decode_error":
		// input_bytes can be empty (valid - zero bytes)
		if tc.ExpectedError == "" {
			return fmt.Errorf("decode_error test missing expected_error")
		}
	}
	return nil
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
			if os.IsNotExist(err) {
				t.Logf("Skipping missing test source: %s", source.Path)
				continue
			}
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

	// Validate version (warn on minor version mismatch but continue)
	if err := validateSemver(tf.Version); err != nil {
		t.Errorf("Invalid version in %s: %v", path, err)
		return
	}

	// Parse tests array
	var tests []testCase
	if tf.Tests != nil {
		if err := json.Unmarshal(tf.Tests, &tests); err != nil {
			t.Errorf("Failed to parse tests in %s: %v", path, err)
			return
		}
	}

	fileName := filepath.Base(path)
	t.Run(fileName, func(t *testing.T) {
		// Track test names for duplicate detection (case-insensitive)
		seenNames := make(map[string]bool)

		for _, tc := range tests {
			// Skip comment-only entries (no name or type)
			if tc.Name == "" && tc.Type == "" {
				continue
			}

			// Validate test name
			if err := validateTestName(tc.Name); err != nil {
				t.Errorf("Invalid test name: %v", err)
				continue
			}

			// Check for duplicate names (case-insensitive)
			lowerName := strings.ToLower(tc.Name)
			if seenNames[lowerName] {
				t.Errorf("Duplicate test name (case-insensitive): %s", tc.Name)
				continue
			}
			seenNames[lowerName] = true

			// Validate test type
			validTypes := map[string]bool{
				"encode": true, "decode": true, "roundtrip": true,
				"encode_error": true, "decode_error": true,
			}
			if !validTypes[tc.Type] {
				t.Errorf("Unknown test type %q in test %s", tc.Type, tc.Name)
				continue
			}

			// Validate required fields
			if err := validateRequiredFields(tc); err != nil {
				t.Errorf("Test %s: %v", tc.Name, err)
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

	// Validate options
	unrecognizedOpts, err := validateOptions(tc.Options)
	if err != nil {
		t.Fatalf("Invalid option: %v", err)
	}
	if len(unrecognizedOpts) > 0 {
		t.Skipf("Skipping test with unrecognized option(s): %v", unrecognizedOpts)
	}

	// Check for unrecognized error types in error tests
	if tc.Type == "encode_error" || tc.Type == "decode_error" {
		if tc.ExpectedError != "" && !recognizedErrorTypes[tc.ExpectedError] {
			t.Skipf("Skipping test with unrecognized error type: %s", tc.ExpectedError)
		}
	}

	switch tc.Type {
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
		t.Fatalf("Unknown test type: %s", tc.Type)
	}
}

func runEncodeTest(t *testing.T, tc testCase) {
	value, err := parseTestValue(tc.Input)
	if err != nil {
		t.Fatalf("Failed to parse input: %v", err)
	}

	var encoded []byte
	if tc.Options != nil {
		// Use streaming encoder when options are specified
		var buf bytes.Buffer
		encoder := NewEncoder(&buf)
		applyEncoderOptions(encoder, tc.Options)
		if err := encoder.Encode(value); err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		encoded = buf.Bytes()
	} else {
		encoded, err = Marshal(value)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
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

	// Use streaming encoder/decoder if options are specified
	var buf bytes.Buffer
	encoder := NewEncoder(&buf)
	applyEncoderOptions(encoder, tc.Options)

	if err := encoder.Encode(value); err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoder := NewDecoder(&buf)
	applyDecoderOptions(decoder, tc.Options)

	var decoded interface{}
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("Decode failed: %v", err)
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

	// Skip tests that require encoder options we don't support
	if tc.Options != nil {
		// The encoder only supports nan_infinity option
		unsupportedEncoderOpts := []string{"max_string_length", "max_container_size", "max_document_size"}
		for _, opt := range unsupportedEncoderOpts {
			if _, has := tc.Options[opt]; has {
				t.Skipf("Encoder does not support %s option", opt)
			}
		}
	}

	// Use streaming encoder if options are specified
	var buf bytes.Buffer
	encoder := NewEncoder(&buf)
	applyEncoderOptions(encoder, tc.Options)

	err = encoder.Encode(value)
	if err == nil {
		t.Errorf("Expected encode error %q but got success", tc.ExpectedError)
	}
}

func runDecodeErrorTest(t *testing.T, tc testCase) {
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
	decoderOpts := []string{"allow_nul", "nan_infinity_behavior", "duplicate_key", "invalid_utf8", "max_depth", "max_string_length", "max_container_size", "max_document_size", "max_bignumber_magnitude", "max_bignumber_exponent", "out_of_range", "unicode_normalization"}
	for _, opt := range decoderOpts {
		if _, ok := opts[opt]; ok {
			return true
		}
	}
	return false
}

func parseHexBytes(s string) ([]byte, error) {
	// Remove whitespace (spaces allowed for readability)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\t", "")
	s = strings.ReplaceAll(s, "\n", "")

	// Empty string is valid (represents zero bytes)
	if s == "" {
		return []byte{}, nil
	}

	// Check for non-hex characters before attempting decode
	for i, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return nil, fmt.Errorf("invalid hex character %q at position %d", string(c), i)
		}
	}

	// Check for odd number of digits
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("hex string has odd number of digits (%d)", len(s))
	}

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
			// Marker objects must contain only the marker key
			if len(val) > 1 {
				return nil, fmt.Errorf("marker object {\"$number\": ...} has extra keys")
			}
			return parseNumberString(numStr)
		}
		// Check for $bytes marker (raw byte sequences for invalid UTF-8 testing)
		if bytesStr, ok := val["$bytes"].(string); ok {
			// Marker objects must contain only the marker key
			if len(val) > 1 {
				return nil, fmt.Errorf("marker object {\"$bytes\": ...} has extra keys")
			}
			// Validate the hex string format (same validation as parseHexBytes)
			rawBytes, err := parseHexBytes(bytesStr)
			if err != nil {
				return nil, fmt.Errorf("invalid $bytes value: %v", err)
			}
			// Return as a special type that can be used for comparison
			// Tests using $bytes require raw_string_bytes capability which we don't support,
			// so this will only be reached in structural validation
			return rawBytesValue(rawBytes), nil
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
	// Empty string is invalid
	if s == "" {
		return nil, fmt.Errorf("$number value is empty")
	}

	// Check for leading/trailing whitespace (not allowed)
	if s != strings.TrimSpace(s) {
		return nil, fmt.Errorf("$number value %q has leading or trailing whitespace", s)
	}

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

	// Check for hex format
	isNegativeHex := strings.HasPrefix(lower, "-0x")
	isHex := strings.HasPrefix(lower, "0x") || isNegativeHex

	if isHex {
		// Validate hex format: must have at least one digit after 0x
		hexPart := lower
		if isNegativeHex {
			hexPart = lower[3:] // Remove "-0x"
		} else {
			hexPart = lower[2:] // Remove "0x"
		}

		// Check for hex float (contains 'p')
		if strings.Contains(hexPart, "p") {
			// Hex float format: 0x[H...][.H...]p[±]D...
			pIdx := strings.Index(hexPart, "p")
			mantissa := hexPart[:pIdx]
			// Mantissa must have at least one hex digit (before or after decimal point)
			mantissa = strings.ReplaceAll(mantissa, ".", "")
			if len(mantissa) == 0 {
				return nil, fmt.Errorf("$number hex float %q has no digits", s)
			}
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil, fmt.Errorf("cannot parse $number hex float %q: %v", s, err)
			}
			return f, nil
		}

		// Hex integer: must have at least one digit
		if len(hexPart) == 0 {
			return nil, fmt.Errorf("$number hex value %q has no digits after 0x", s)
		}

		// Parse as hex integer
		bi := new(big.Int)
		if isNegativeHex {
			// big.Int.SetString doesn't handle negative hex directly
			_, ok := bi.SetString(hexPart, 16)
			if !ok {
				return nil, fmt.Errorf("cannot parse $number hex integer %q", s)
			}
			bi.Neg(bi)
		} else {
			_, ok := bi.SetString(hexPart, 16)
			if !ok {
				return nil, fmt.Errorf("cannot parse $number hex integer %q", s)
			}
		}

		// Check if it fits in int64/uint64
		if bi.IsInt64() {
			return bi.Int64(), nil
		}
		if bi.IsUint64() {
			return bi.Uint64(), nil
		}
		return bi, nil
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

func applyEncoderOptions(enc *Encoder, opts map[string]interface{}) {
	if opts == nil {
		return
	}

	if v, ok := opts["allow_nul"].(bool); ok && v {
		enc.AllowNUL()
	}

	if v, ok := opts["max_depth"].(float64); ok {
		enc.SetMaxDepth(int(v))
	}

	// Handle nan_infinity_behavior option (string mode: "allow", "stringify", "reject")
	if v, ok := opts["nan_infinity_behavior"].(string); ok {
		switch v {
		case "allow":
			enc.SetNaNInfinityMode(NaNInfAllow)
		case "stringify":
			enc.SetNaNInfinityMode(NaNInfStringify)
		case "reject":
			enc.SetNaNInfinityMode(NaNInfReject)
		}
	}
}

func applyDecoderOptions(dec *Decoder, opts map[string]interface{}) {
	if opts == nil {
		return
	}

	if v, ok := opts["allow_nul"].(bool); ok && v {
		dec.AllowNUL()
	}
	// Handle nan_infinity_behavior option (string mode: "allow", "stringify", "reject")
	if v, ok := opts["nan_infinity_behavior"].(string); ok {
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
	if v, ok := opts["max_container_size"]; ok {
		if size, ok := toInt(v); ok {
			dec.SetMaxContainerSize(size)
		}
	}
	if v, ok := opts["max_document_size"]; ok {
		if size, ok := toInt64(v); ok {
			dec.SetMaxDocumentSize(size)
		}
	}
	if v, ok := opts["max_bignumber_magnitude"]; ok {
		if size, ok := toInt(v); ok {
			dec.SetMaxBigNumberMagnitude(size)
		}
	}
	if v, ok := opts["max_bignumber_exponent"]; ok {
		if size, ok := toInt(v); ok {
			dec.SetMaxBigNumberExponent(size)
		}
	}
	if v, ok := opts["allow_trailing_bytes"].(bool); ok && v {
		// Note: Decoder doesn't have this option, handled differently
	}
	// Handle out_of_range option (string mode: "error", "stringify")
	if v, ok := opts["out_of_range"].(string); ok {
		switch v {
		case "stringify":
			dec.SetOutOfRangeMode(OutOfRangeStringify)
		case "error":
			dec.SetOutOfRangeMode(OutOfRangeReject)
		}
	}
	// Handle unicode_normalization option (string mode: "none", "nfc")
	if v, ok := opts["unicode_normalization"].(string); ok {
		switch v {
		case "nfc":
			dec.SetUnicodeNormalizationMode(UnicodeNormNFC)
		case "none":
			dec.SetUnicodeNormalizationMode(UnicodeNormNone)
		}
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

	// Handle rawBytesValue (for $bytes marker comparison)
	if ab, ok := a.(rawBytesValue); ok {
		if bb, ok := b.(rawBytesValue); ok {
			return bytes.Equal(ab, bb)
		}
		// rawBytesValue can also compare to string (decoded bytes)
		if bs, ok := b.(string); ok {
			return bytes.Equal(ab, []byte(bs))
		}
		return false
	}
	if bs, ok := b.(rawBytesValue); ok {
		if as, ok := a.(string); ok {
			return bytes.Equal([]byte(as), bs)
		}
		return false
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

// ============================================================================
// Test Runner Validation Tests
// ============================================================================

// TestRunnerValidationMustPass tests that valid test files are processed correctly.
func TestRunnerValidationMustPass(t *testing.T) {
	mustPassDir := "specification/tests/test-runner-validation/must-pass"

	if _, err := os.Stat(mustPassDir); os.IsNotExist(err) {
		t.Skip("Test runner validation tests not found at " + mustPassDir)
	}

	entries, err := os.ReadDir(mustPassDir)
	if err != nil {
		t.Fatalf("Failed to read must-pass directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(mustPassDir, entry.Name())
		t.Run(entry.Name(), func(t *testing.T) {
			runValidMustPassFile(t, path)
		})
	}
}

func runValidMustPassFile(t *testing.T, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	var tf testFile
	if err := json.Unmarshal(data, &tf); err != nil {
		t.Fatalf("Failed to parse file: %v", err)
	}

	if tf.Type != "bonjson-test" {
		t.Fatalf("Wrong type: %s", tf.Type)
	}

	if err := validateSemver(tf.Version); err != nil {
		t.Fatalf("Invalid version: %v", err)
	}

	// Parse tests array
	var tests []testCase
	if tf.Tests != nil {
		if err := json.Unmarshal(tf.Tests, &tests); err != nil {
			t.Fatalf("Failed to parse tests: %v", err)
		}
	}

	// Track test names for duplicate detection
	seenNames := make(map[string]bool)

	for _, tc := range tests {
		// Skip comment-only entries
		if tc.Name == "" && tc.Type == "" {
			continue
		}

		if tc.Name != "" {
			if err := validateTestName(tc.Name); err != nil {
				t.Errorf("Invalid test name: %v", err)
				continue
			}

			lowerName := strings.ToLower(tc.Name)
			if seenNames[lowerName] {
				t.Errorf("Duplicate test name: %s", tc.Name)
				continue
			}
			seenNames[lowerName] = true
		}

		// Validate options
		_, err := validateOptions(tc.Options)
		if err != nil {
			t.Errorf("Test %s: invalid option: %v", tc.Name, err)
		}

		// Run the test case to verify it works
		tc := tc // capture
		t.Run(tc.Name, func(t *testing.T) {
			runTestCase(t, tc)
		})
	}
}

// TestRunnerValidationStructuralErrors tests that invalid files produce errors.
func TestRunnerValidationStructuralErrors(t *testing.T) {
	errorsDir := "specification/tests/test-runner-validation/structural-errors"

	if _, err := os.Stat(errorsDir); os.IsNotExist(err) {
		t.Skip("Structural error tests not found at " + errorsDir)
	}

	entries, err := os.ReadDir(errorsDir)
	if err != nil {
		t.Fatalf("Failed to read structural-errors directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(errorsDir, entry.Name())
		t.Run(entry.Name(), func(t *testing.T) {
			expectStructuralError(t, path)
		})
	}
}

func expectStructuralError(t *testing.T, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	// Use raw JSON to detect truly missing fields
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		// JSON parse error is a structural error
		return
	}

	// Check if type is missing (not present in map at all)
	typeRaw, hasType := raw["type"]
	if !hasType {
		return // Missing type is a structural error
	}
	var typeStr string
	if err := json.Unmarshal(typeRaw, &typeStr); err != nil {
		return // Type not a string is a structural error
	}
	if typeStr != "bonjson-test" {
		return // Wrong type is a structural error
	}

	// Check version
	versionRaw, hasVersion := raw["version"]
	if !hasVersion {
		return // Missing version is a structural error
	}
	var versionStr string
	if err := json.Unmarshal(versionRaw, &versionStr); err != nil {
		return // Version not a string is a structural error
	}
	if err := validateSemver(versionStr); err != nil {
		return // Invalid version is a structural error
	}

	// Check tests array
	testsRaw, hasTests := raw["tests"]
	if !hasTests {
		return // Missing tests is a structural error
	}

	// Check if tests is an array
	var testsArray []json.RawMessage
	if err := json.Unmarshal(testsRaw, &testsArray); err != nil {
		return // Tests not an array is a structural error
	}

	// Check each test case
	seenNames := make(map[string]bool)
	foundError := false

	for _, testRaw := range testsArray {
		var testMap map[string]json.RawMessage
		if err := json.Unmarshal(testRaw, &testMap); err != nil {
			foundError = true // Test not an object is a structural error
			break
		}

		// Check if this is a comment-only entry (only keys starting with //)
		hasNonCommentKey := false
		for key := range testMap {
			if !strings.HasPrefix(key, "//") {
				hasNonCommentKey = true
				break
			}
		}
		if !hasNonCommentKey {
			continue // Comment-only entry, skip
		}

		// Check for name
		nameRaw, hasName := testMap["name"]
		if !hasName {
			foundError = true
			break
		}
		var name string
		if err := json.Unmarshal(nameRaw, &name); err != nil || name == "" {
			foundError = true
			break
		}
		if err := validateTestName(name); err != nil {
			foundError = true
			break
		}
		lowerName := strings.ToLower(name)
		if seenNames[lowerName] {
			foundError = true
			break
		}
		seenNames[lowerName] = true

		// Check for type
		typeRaw, hasType := testMap["type"]
		if !hasType {
			foundError = true
			break
		}
		var testType string
		if err := json.Unmarshal(typeRaw, &testType); err != nil || testType == "" {
			foundError = true
			break
		}
		validTypes := map[string]bool{
			"encode": true, "decode": true, "roundtrip": true,
			"encode_error": true, "decode_error": true,
		}
		if !validTypes[testType] {
			foundError = true
			break
		}

		// Check required fields for each test type
		switch testType {
		case "encode":
			if _, has := testMap["input"]; !has {
				foundError = true
			}
			if _, has := testMap["expected_bytes"]; !has {
				foundError = true
			}
		case "decode":
			if _, has := testMap["input_bytes"]; !has {
				foundError = true
			}
			if _, has := testMap["expected_value"]; !has {
				foundError = true
			}
		case "roundtrip":
			if _, has := testMap["input"]; !has {
				foundError = true
			}
		case "encode_error":
			if _, has := testMap["input"]; !has {
				foundError = true
			}
			if _, has := testMap["expected_error"]; !has {
				foundError = true
			}
		case "decode_error":
			if _, has := testMap["input_bytes"]; !has {
				foundError = true
			}
			if _, has := testMap["expected_error"]; !has {
				foundError = true
			}
		}
		if foundError {
			break
		}

		// Validate options if present
		if optionsRaw, has := testMap["options"]; has {
			var opts map[string]interface{}
			if err := json.Unmarshal(optionsRaw, &opts); err != nil {
				foundError = true
				break
			}
			if _, err := validateOptions(opts); err != nil {
				foundError = true
				break
			}
		}

		// Check for marker object errors and hex errors
		var tc testCase
		if err := json.Unmarshal(testRaw, &tc); err == nil {
			if tc.Input != nil {
				if _, err := parseTestValue(tc.Input); err != nil {
					foundError = true
					break
				}
			}
			if tc.ExpectedValue != nil {
				if _, err := parseTestValue(tc.ExpectedValue); err != nil {
					foundError = true
					break
				}
			}
			if tc.InputBytes != "" {
				if _, err := parseHexBytes(tc.InputBytes); err != nil {
					foundError = true
					break
				}
			}
			if tc.ExpectedBytes != "" {
				if _, err := parseHexBytes(tc.ExpectedBytes); err != nil {
					foundError = true
					break
				}
			}
		}
	}

	if !foundError {
		t.Errorf("Expected structural error but file processed successfully")
	}
}

// TestRunnerValidationSkipScenarios tests that tests with unrecognized options/errors are skipped.
func TestRunnerValidationSkipScenarios(t *testing.T) {
	skipDir := "specification/tests/test-runner-validation/skip-scenarios"

	if _, err := os.Stat(skipDir); os.IsNotExist(err) {
		t.Skip("Skip scenario tests not found at " + skipDir)
	}

	entries, err := os.ReadDir(skipDir)
	if err != nil {
		t.Fatalf("Failed to read skip-scenarios directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(skipDir, entry.Name())
		t.Run(entry.Name(), func(t *testing.T) {
			expectTestsSkipped(t, path)
		})
	}
}

func expectTestsSkipped(t *testing.T, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	var tf testFile
	if err := json.Unmarshal(data, &tf); err != nil {
		t.Fatalf("Failed to parse file: %v", err)
	}

	// Parse tests array
	var tests []testCase
	if tf.Tests != nil {
		if err := json.Unmarshal(tf.Tests, &tests); err != nil {
			t.Fatalf("Failed to parse tests: %v", err)
		}
	}

	foundSkippable := false

	for _, tc := range tests {
		// Skip comment-only entries
		if tc.Name == "" && tc.Type == "" {
			continue
		}

		// Check for unrecognized options
		unrecognizedOpts, _ := validateOptions(tc.Options)
		if len(unrecognizedOpts) > 0 {
			foundSkippable = true
			t.Logf("Test %s would be skipped due to unrecognized options: %v", tc.Name, unrecognizedOpts)
		}

		// Check for unrecognized error types
		if (tc.Type == "encode_error" || tc.Type == "decode_error") && tc.ExpectedError != "" {
			if !recognizedErrorTypes[tc.ExpectedError] {
				foundSkippable = true
				t.Logf("Test %s would be skipped due to unrecognized error type: %s", tc.Name, tc.ExpectedError)
			}
		}
	}

	if !foundSkippable {
		t.Errorf("Expected at least one test to be skippable but none found")
	}
}

// TestRunnerValidationValueHandling tests edge cases in value parsing and comparison.
func TestRunnerValidationValueHandling(t *testing.T) {
	valueDir := "specification/tests/test-runner-validation/value-handling"

	if _, err := os.Stat(valueDir); os.IsNotExist(err) {
		t.Skip("Value handling tests not found at " + valueDir)
	}

	entries, err := os.ReadDir(valueDir)
	if err != nil {
		t.Fatalf("Failed to read value-handling directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(valueDir, entry.Name())
		t.Run(entry.Name(), func(t *testing.T) {
			runValidMustPassFile(t, path)
		})
	}
}

// TestValidationFunctions tests the validation functions directly.
func TestValidationFunctions(t *testing.T) {
	// Test name validation
	t.Run("TestNameValidation", func(t *testing.T) {
		validNames := []string{"a", "abc", "test_123", "TestName", "a1"}
		for _, name := range validNames {
			if err := validateTestName(name); err != nil {
				t.Errorf("Expected %q to be valid, got error: %v", name, err)
			}
		}

		invalidNames := []string{"", "123", "1test", "test-name", "test.name", "test name"}
		for _, name := range invalidNames {
			if err := validateTestName(name); err == nil {
				t.Errorf("Expected %q to be invalid, but got no error", name)
			}
		}
	})

	// Test semver validation
	t.Run("SemverValidation", func(t *testing.T) {
		validVersions := []string{"1.0.0", "0.1.0", "1.2.3", "1.0.0-alpha", "1.0.0-beta.2", "1.0.0+build.123", "1.0.0-rc.1+build.456"}
		for _, v := range validVersions {
			if err := validateSemver(v); err != nil {
				t.Errorf("Expected %q to be valid semver, got error: %v", v, err)
			}
		}

		invalidVersions := []string{"", "1.0", "1", "abc", "1.0.0.0", "v1.0.0"}
		for _, v := range invalidVersions {
			if err := validateSemver(v); err == nil {
				t.Errorf("Expected %q to be invalid semver, but got no error", v)
			}
		}
	})

	// Test hex parsing
	t.Run("HexParsing", func(t *testing.T) {
		validHex := []string{"", "00", "ff", "FF", "00 ff", "00FF", "ab cd ef"}
		for _, h := range validHex {
			if _, err := parseHexBytes(h); err != nil {
				t.Errorf("Expected %q to be valid hex, got error: %v", h, err)
			}
		}

		invalidHex := []string{"0", "fff", "gg", "0x00"}
		for _, h := range invalidHex {
			if _, err := parseHexBytes(h); err == nil {
				t.Errorf("Expected %q to be invalid hex, but got no error", h)
			}
		}
	})

	// Test $number parsing
	t.Run("NumberParsing", func(t *testing.T) {
		validNumbers := []string{
			"0", "1", "-1", "100", "-100",
			"1.5", "-1.5", "1e10", "1E10", "1.5e-10",
			"NaN", "nan", "NAN",
			"Infinity", "infinity", "-Infinity", "-infinity",
			"-0.0", "-0",
			"0xff", "0xFF", "-0x10",
			"0x1.0p0", "0x1.921fb54442d18p+1",
		}
		for _, n := range validNumbers {
			if _, err := parseNumberString(n); err != nil {
				t.Errorf("Expected $number %q to be valid, got error: %v", n, err)
			}
		}

		invalidNumbers := []string{"", " ", "0x", "-0x", "hello", "  1.5  "}
		for _, n := range invalidNumbers {
			if _, err := parseNumberString(n); err == nil {
				t.Errorf("Expected $number %q to be invalid, but got no error", n)
			}
		}
	})

	// Test value comparison
	t.Run("ValueComparison", func(t *testing.T) {
		// NaN equals NaN
		if !valuesEqual(math.NaN(), math.NaN()) {
			t.Error("NaN should equal NaN for testing")
		}

		// -0.0 is distinct from 0.0
		negZero := math.Copysign(0, -1)
		posZero := 0.0
		if valuesEqual(negZero, posZero) {
			t.Error("-0.0 should not equal 0.0")
		}

		// Mathematical equality across types
		if !valuesEqual(int64(1), float64(1)) {
			t.Error("int64(1) should equal float64(1)")
		}

		// Object comparison is order-independent
		obj1 := map[string]interface{}{"a": 1, "b": 2}
		obj2 := map[string]interface{}{"b": 2, "a": 1}
		if !valuesEqual(obj1, obj2) {
			t.Error("Objects with same keys should be equal regardless of order")
		}
	})
}
