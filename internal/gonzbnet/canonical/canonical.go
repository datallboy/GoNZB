package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

const HashPrefixSHA256 = "sha256:"

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Marshal returns deterministic JSON bytes for signing and hashing.
func Marshal(v any) ([]byte, error) {
	normalized, err := normalize(v)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := writeValue(&buf, normalized); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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

func normalize(v any) (any, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		return normalizeRaw(t)
	case []byte:
		return normalizeRaw(t)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal json: %w", err)
		}
		return normalizeRaw(b)
	}
}

func normalizeRaw(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode json: trailing data")
		}
		return nil, fmt.Errorf("decode json: trailing data")
	}
	return v, nil
}

func writeValue(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		buf.Write(b)
	case json.Number:
		if err := validateJSONNumber(t.String()); err != nil {
			return err
		}
		buf.WriteString(t.String())
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return fmt.Errorf("non-finite json number")
		}
		buf.WriteString(strconv.FormatFloat(t, 'g', -1, 64))
	case []any:
		buf.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeValue(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for key := range t {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyBytes, err := json.Marshal(key)
			if err != nil {
				return err
			}
			buf.Write(keyBytes)
			buf.WriteByte(':')
			if err := writeValue(buf, t[key]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical value type %T", v)
	}
	return nil
}

func validateJSONNumber(value string) error {
	if strings.TrimSpace(value) != value || value == "" {
		return fmt.Errorf("invalid json number %q", value)
	}
	if _, err := strconv.ParseFloat(value, 64); err != nil {
		return fmt.Errorf("invalid json number %q: %w", value, err)
	}
	return nil
}
