// Package model holds the supplier-side product type and exact money handling.
package model

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Money is an amount in minor units (kopecks). Integer arithmetic keeps
// prices exact through markup and formatting.
type Money int64

var ErrBadMoney = errors.New("model: not a valid amount")

// ParseMoney parses "1234", "1234.5", "1 234,50", "1234.567" (rounded half up
// to kopecks). Empty or non-numeric input returns ErrBadMoney.
func ParseMoney(s string) (Money, error) {
	s = strings.NewReplacer(" ", "", " ", "", ",", ".").Replace(strings.TrimSpace(s))
	if s == "" {
		return 0, ErrBadMoney
	}
	neg := false
	if s[0] == '-' {
		neg, s = true, s[1:]
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" && !hasFrac {
		return 0, ErrBadMoney
	}
	var w int64
	if whole != "" {
		v, err := strconv.ParseInt(whole, 10, 64)
		if err != nil || v < 0 {
			return 0, ErrBadMoney
		}
		w = v
	}
	var cents int64
	if hasFrac {
		if frac == "" {
			return 0, ErrBadMoney
		}
		for _, r := range frac {
			if r < '0' || r > '9' {
				return 0, ErrBadMoney
			}
		}
		for len(frac) < 3 {
			frac += "0"
		}
		c, _ := strconv.ParseInt(frac[:2], 10, 64)
		cents = c
		if frac[2] >= '5' {
			cents++
		}
	}
	m := Money(w*100 + cents)
	if neg {
		m = -m
	}
	return m, nil
}

// Format renders "1234" for whole amounts and "1234.50" otherwise.
func (m Money) Format() string {
	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}
	w, c := int64(m)/100, int64(m)%100
	if c == 0 {
		return sign + strconv.FormatInt(w, 10)
	}
	return sign + strconv.FormatInt(w, 10) + "." + pad2(c)
}

func pad2(c int64) string {
	if c < 10 {
		return "0" + strconv.FormatInt(c, 10)
	}
	return strconv.FormatInt(c, 10)
}

// Scale multiplies by factor and rounds to the nearest kopeck.
func (m Money) Scale(factor float64) Money {
	return Money(math.Round(float64(m) * factor))
}

// RoundUpTo rounds up to a multiple of step whole currency units (step >= 1).
func (m Money) RoundUpTo(step int64) Money {
	if step <= 0 {
		return m
	}
	unit := step * 100
	q := (int64(m) + unit - 1) / unit
	return Money(q * unit)
}

// Param is one product characteristic.
type Param struct {
	Name  string
	Value string
}

// Product is a normalized supplier product, independent of the input format.
type Product struct {
	SupplierID  string
	SKU         string // vendor code / article
	Name        string
	Description string
	Vendor      string
	CategoryRef string // supplier category id
	Price       Money
	OldPrice    Money
	Currency    string
	Quantity    int
	Pictures    []string
	Params      []Param
	URL         string
}
