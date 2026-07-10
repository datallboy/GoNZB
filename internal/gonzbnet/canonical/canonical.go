package canonical

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

const HashPrefixSHA256 = "sha256:"

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Marshal returns RFC 8785 JSON Canonicalization Scheme bytes for signing and
// hashing. Raw JSON is preserved until canonicalization so duplicate keys can
// be rejected instead of being collapsed by encoding/json.
func Marshal(v any) ([]byte, error) {
	var raw []byte
	switch value := v.(type) {
	case json.RawMessage:
		raw = append([]byte(nil), value...)
	case []byte:
		raw = append([]byte(nil), value...)
	default:
		var err error
		raw, err = json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal json: %w", err)
		}
	}
	return Canonicalize(raw)
}

// Canonicalize validates and transforms raw JSON using RFC 8785 JCS.
func Canonicalize(raw []byte) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("canonicalize json: input is not valid UTF-8")
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("canonicalize json: invalid JSON")
	}

	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("canonicalize json: empty input")
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		wrapped, err := jsoncanonicalizer.Transform([]byte("[" + trimmed + "]"))
		if err != nil {
			return nil, fmt.Errorf("canonicalize json: %w", err)
		}
		return append([]byte(nil), wrapped[1:len(wrapped)-1]...), nil
	}
	canonicalJSON, err := jsoncanonicalizer.Transform([]byte(trimmed))
	if err != nil {
		return nil, fmt.Errorf("canonicalize json: %w", err)
	}
	return canonicalJSON, nil
}

// ValidateJSON applies the same strict parser used for canonicalization. It is
// intended for receive boundaries that must reject duplicate object keys before
// decoding into Go structs.
func ValidateJSON(raw []byte) error {
	_, err := Canonicalize(raw)
	return err
}

func BodyHash(v any) (string, []byte, error) {
	canonicalBytes, err := Marshal(v)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(canonicalBytes)
	return HashPrefixSHA256 + Base64URL(sum[:]), canonicalBytes, nil
}

func HashID(prefix string, payload []byte) string {
	sum := sha256.Sum256(payload)
	return prefix + "_" + strings.ToLower(base32NoPadding.EncodeToString(sum[:]))
}

func Base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func DecodeBase64URL(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimSpace(s))
}
