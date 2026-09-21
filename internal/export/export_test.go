package export

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/valpere/feedsync/internal/model"
	"github.com/valpere/feedsync/internal/offer"
)

func sample() offer.Offer {
	return offer.Offer{
		ID: "a1", Available: true, Name: `Phone "X" <Pro> & co`, Description: "html <b>ok</b> ]]> tail",
		Vendor: "Acme", Article: "SKU-1", Price: 123450, OldPrice: 150000, Currency: "UAH", CategoryID: "1001", Quantity: 5,
		Pictures: []string{"https://cdn.example.com/a.jpg"}, Params: []model.Param{{Name: "Color", Value: "black & white"}},
	}
}

func render(t *testing.T, target string) string {
	t.Helper()
	var buf bytes.Buffer
	w := NewWriter(target, &buf)
	w.Begin(Shop{Name: "S"}, []Category{{ID: "1001", Name: "Phones", RzID: "80003"}}, time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC))
	w.Offer(sample())
	if err := w.End(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestWellFormedAndEscaped(t *testing.T) {
	for _, target := range []string{Rozetka, Prom} {
		out := render(t, target)
		d := xml.NewDecoder(strings.NewReader(out))
		for {
			if _, err := d.Token(); err != nil {
				if err.Error() != "EOF" {
					t.Fatalf("%s: malformed XML: %v\n%s", target, err, out)
				}
				break
			}
		}
		if !strings.Contains(out, "Phone &#34;X&#34; &lt;Pro&gt; &amp; co") {
			t.Errorf("%s: name not escaped:\n%s", target, out)
		}
		if strings.Contains(out, "<![CDATA[html <b>ok</b> ]]> tail") {
			t.Errorf("%s: raw ]]> leaked into CDATA", target)
		}
	}
}

func TestTargetSpecificTags(t *testing.T) {
	r, p := render(t, Rozetka), render(t, Prom)
	for _, want := range []string{"<stock_quantity>5</stock_quantity>", "<article>SKU-1</article>", "<price_old>1500</price_old>", `rz_id="80003"`, "<price>1234.50</price>"} {
		if !strings.Contains(r, want) {
			t.Errorf("rozetka missing %s", want)
		}
	}
	for _, want := range []string{"<quantity_in_stock>5</quantity_in_stock>", "<vendorCode>SKU-1</vendorCode>", "<oldprice>1500</oldprice>"} {
		if !strings.Contains(p, want) {
			t.Errorf("prom missing %s", want)
		}
	}
	if strings.Contains(p, "rz_id") {
		t.Error("prom must not carry rz_id")
	}
}

func TestControlCharsStripped(t *testing.T) {
	if got := Esc("a\x01b\x1fc\td"); got != "abc&#x9;d" {
		t.Errorf("got %q", got)
	}
}
