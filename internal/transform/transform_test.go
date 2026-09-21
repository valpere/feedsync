package transform

import (
	"testing"

	"github.com/valpere/feedsync/internal/config"
	"github.com/valpere/feedsync/internal/model"
)

func cfg() *config.Config {
	return &config.Config{
		Categories:    map[string]config.Category{"10": {ID: "1001", Name: "Phones"}, "11": {ID: "1002", Name: "Laptops"}},
		CurrencyRates: map[string]float64{"USD": 40},
		Markup: config.Markup{
			Default: config.Rule{Type: "percent", Value: 10},
			Rules: []config.Rule{
				{PriceTo: 500, Type: "fixed", Value: 40},
				{Category: "10", Type: "percent", Value: 5},
			},
		},
		Filter:   config.Filter{DropZeroStock: true},
		Pictures: config.Pictures{ForceHTTPS: true},
		IDs:      config.IDs{Prefix: "s"},
	}
}

func prod(price model.Money, cat string) model.Product {
	return model.Product{SupplierID: "X1", SKU: "K1", Name: "n", CategoryRef: cat, Price: price, Currency: "UAH", Quantity: 3}
}

func TestMarkupFirstMatchWins(t *testing.T) {
	tr := New(cfg())
	cases := []struct {
		price model.Money
		cat   string
		want  model.Money
	}{
		{30000, "10", 34000},   // < 500: fixed +40 beats the category rule
		{100000, "10", 105000}, // category rule: +5%
		{100000, "11", 110000}, // default +10%
		{49999, "11", 53999},   // 499.99 + 40 = 539.99
	}
	for _, c := range cases {
		o, reason := tr.Apply(prod(c.price, c.cat))
		if reason != "" || o.Price != c.want {
			t.Errorf("price %d cat %s = %d (%s), want %d", c.price, c.cat, o.Price, reason, c.want)
		}
	}
}

func TestRejections(t *testing.T) {
	tr := New(cfg())
	cases := []struct {
		name   string
		mutate func(*model.Product)
		reason string
	}{
		{"unmapped category", func(p *model.Product) { p.CategoryRef = "99" }, ReasonUnmappedCategory},
		{"zero stock", func(p *model.Product) { p.Quantity = 0 }, ReasonZeroStock},
		{"bad price", func(p *model.Product) { p.Price = 0 }, ReasonInvalidPrice},
		{"no id", func(p *model.Product) { p.SupplierID, p.SKU = "", "" }, ReasonMissingID},
		{"unknown currency", func(p *model.Product) { p.Currency = "PLN" }, ReasonUnknownCurrency},
	}
	for _, c := range cases {
		p := prod(100000, "10")
		c.mutate(&p)
		if _, reason := tr.Apply(p); reason != c.reason {
			t.Errorf("%s: reason %q, want %q", c.name, reason, c.reason)
		}
	}
}

func TestCurrencyConversionBeforeMarkup(t *testing.T) {
	p := prod(1000, "11") // 10.00 USD
	p.Currency = "usd"
	o, _ := New(cfg()).Apply(p) // 10 USD * 40 = 400 UAH, < 500 so +40 fixed
	if o.Price != 44000 || o.Currency != "UAH" {
		t.Errorf("got %d %s", o.Price, o.Currency)
	}
}

func TestOfferIDStableAndSafe(t *testing.T) {
	tr := New(cfg())
	a := prod(100000, "10")
	a.SupplierID = "SUP-0001"
	o1, _ := tr.Apply(a)
	o2, _ := tr.Apply(a)
	if o1.ID != o2.ID || o1.ID == "SUP-0001" || len(o1.ID) != 13 {
		t.Errorf("derived id not stable/safe: %q %q", o1.ID, o2.ID)
	}
	a.SupplierID = "ABC123"
	if o, _ := tr.Apply(a); o.ID != "ABC123" {
		t.Errorf("safe id was rewritten: %q", o.ID)
	}
}

func TestForceHTTPSAndOldPrice(t *testing.T) {
	p := prod(100000, "11")
	p.Pictures = []string{"http://a/b.jpg", "https://c/d.jpg"}
	p.OldPrice = 90000 // lower than the marked-up price: must be dropped
	o, _ := New(cfg()).Apply(p)
	if o.Pictures[0] != "https://a/b.jpg" || o.Pictures[1] != "https://c/d.jpg" {
		t.Errorf("pictures: %v", o.Pictures)
	}
	if o.OldPrice != 0 {
		t.Errorf("old price should be dropped, got %d", o.OldPrice)
	}
}
