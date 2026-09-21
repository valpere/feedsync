package model

import "testing"

func TestParseMoney(t *testing.T) {
	cases := []struct {
		in   string
		want Money
		ok   bool
	}{
		{"1234", 123400, true}, {"1234.5", 123450, true}, {"1 234,50", 123450, true},
		{"0.995", 100, true}, {"10.004", 1000, true}, {"10.005", 1001, true},
		{" 12 000", 1200000, true}, {"-5.25", -525, true},
		{"", 0, false}, {"abc", 0, false}, {"12.", 0, false}, {"12.3x", 0, false}, {".", 0, false},
	}
	for _, c := range cases {
		got, err := ParseMoney(c.in)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("ParseMoney(%q) = %d, %v; want %d ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

func TestFormatAndScale(t *testing.T) {
	if got := Money(123400).Format(); got != "1234" {
		t.Errorf("whole: %q", got)
	}
	if got := Money(123405).Format(); got != "1234.05" {
		t.Errorf("frac: %q", got)
	}
	if got := Money(10000).Scale(1.15); got != 11500 {
		t.Errorf("scale: %d", got)
	}
	if got := Money(9999).Scale(1.2); got != 11999 { // 119.988 -> 119.99
		t.Errorf("scale round: %d", got)
	}
}

func TestRoundUpTo(t *testing.T) {
	if got := Money(10001).RoundUpTo(1); got != 10100 {
		t.Errorf("got %d", got)
	}
	if got := Money(10000).RoundUpTo(1); got != 10000 {
		t.Errorf("exact multiple changed: %d", got)
	}
	if got := Money(10100).RoundUpTo(5); got != 10500 {
		t.Errorf("step 5: %d", got)
	}
}
