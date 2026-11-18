package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
)

func TestNumberKey(t *testing.T) {
	for _, group := range [][]string{
		{"0", "-0", "0.0", "-0e+9999999999999999999999"},
		{"12", "12.00", "1.2e1", "0.00120E+4", "1200e-2"},
		{"-1e-1", "-0.1", "-0.1000", "-1.0000e-1"},
		{"1e100000000000000000000", "10e99999999999999999999"},
		{"1e-100000000000000000000", "0.1e-99999999999999999999"},
	} {
		for _, value := range group {
			if got := numberKey(json.Number(value)); got != group[0] {
				t.Errorf("numberKey(%q) = %q, want %q", value, got, group[0])
			}
		}
	}
}

// Check equivalent and distinct decimal spellings against a rational-number oracle.
func FuzzDecimalComparison(f *testing.F) {
	f.Add(int64(9007199254740993), int8(1), int8(0))
	f.Add(int64(0), int8(-12), int8(7))
	f.Add(int64(-123000), int8(127), int8(-2))
	f.Fuzz(func(t *testing.T, coeff int64, exponent, delta int8) {
		a := fmt.Sprintf("%de%d", coeff, exponent)
		var b string
		if coeff == 0 {
			b = fmt.Sprintf("-0.0e%d", int(exponent)+int(delta))
		} else {
			b = fmt.Sprintf("%d0e%d", coeff, int(exponent)-1+int(delta))
		}
		ar, okA := new(big.Rat).SetString(a)
		br, okB := new(big.Rat).SetString(b)
		if !okA || !okB {
			t.Fatalf("invalid fixture %s / %s", a, b)
		}
		if got, want := numberKey(json.Number(a)) == numberKey(json.Number(b)), ar.Cmp(br) == 0; got != want {
			t.Fatalf("equality for %s / %s: got %t, want %t", a, b, got, want)
		}
	})
}
