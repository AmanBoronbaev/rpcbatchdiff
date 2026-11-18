package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"
	"unicode/utf8"
)

func readBatch(path string, allowEmptyBody bool) ([]any, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if allowEmptyBody && len(bytes.Trim(body, " \r\n\t")) == 0 {
		return nil, nil
	}
	if !utf8.Valid(body) {
		return nil, errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	value, err := readValue(d)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("trailing JSON value")
	}
	batch, ok := value.([]any)
	if !ok || len(batch) == 0 {
		return nil, errors.New("expected a nonempty JSON-RPC batch array")
	}
	return batch, nil
}

// Reading tokens lets us reject repeated object names instead of silently
// replacing an ID, result, or nested field with its last occurrence.
func readValue(d *json.Decoder) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		obj := make(map[string]any)
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, errors.New("expected an object member name")
			}
			if _, exists := obj[name]; exists {
				return nil, fmt.Errorf("duplicate object member %q", name)
			}
			obj[name], err = readValue(d)
			if err != nil {
				return nil, err
			}
		}
		_, err = d.Token() // closing brace
		return obj, err
	case json.Delim('['):
		array := []any{}
		for d.More() {
			v, err := readValue(d)
			if err != nil {
				return nil, err
			}
			array = append(array, v)
		}
		_, err = d.Token() // closing bracket
		return array, err
	default:
		return token, nil
	}
}

// numberKey represents a JSON number by its significant decimal digits and
// power of ten. Even very large exponents never expand into strings of zeros.
func numberKey(n json.Number) string {
	raw := n.String()
	negative := strings.HasPrefix(raw, "-")
	raw = strings.TrimPrefix(raw, "-")
	mantissa, exponent := raw, ""
	if i := strings.IndexAny(raw, "eE"); i >= 0 {
		mantissa, exponent = raw[:i], raw[i+1:]
	}
	whole, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	if len(digits) == 0 {
		return "0"
	}
	significant := strings.TrimRight(digits, "0")
	shift := big.NewInt(int64(len(digits) - len(significant) - len(fraction)))
	if exponent != "" {
		var e big.Int
		e.SetString(exponent, 10) // n comes from a JSON decoder.
		shift.Add(shift, &e)
	}
	if negative {
		significant = "-" + significant
	}
	if shift.Sign() == 0 {
		return significant
	}
	return significant + "e" + shift.String()
}
