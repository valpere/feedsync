package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valpere/feedsync/internal/model"
)

func good() RawOffer {
	return RawOffer{ID: "A1", Available: "true", Name: "Phone", Description: "d", Vendor: "V", Price: "10.5",
		Currency: "UAH", CategoryID: "1", Quantity: "3", Pictures: []string{"https://cdn.example.com/a.jpg"}}
}

func rules(is []Issue) string {
	var r []string
	for _, i := range is {
		r = append(r, i.Rule)
	}
	return strings.Join(r, ",")
}

func TestGoodOfferPassesBothTargets(t *testing.T) {
	for _, tg := range []string{Rozetka, Prom} {
		if is := NewCtx(tg, []string{"1"}).Check(good()); len(is) != 0 {
			t.Errorf("%s: %v", tg, is)
		}
	}
}

func TestRozetkaRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*RawOffer)
		want   string
	}{
		{"id with dash", func(o *RawOffer) { o.ID = "A-1" }, "id_format"},
		{"bad available", func(o *RawOffer) { o.Available = "yes" }, "available_value"},
		{"zero price", func(o *RawOffer) { o.Price = "0" }, "price"},
		{"old price below price", func(o *RawOffer) { o.OldPrice = "5" }, "price_old"},
		{"currency", func(o *RawOffer) { o.Currency = "PLN" }, "currency"},
		{"unknown category", func(o *RawOffer) { o.CategoryID = "9" }, "category_unknown"},
		{"non-integer qty", func(o *RawOffer) { o.Quantity = "many" }, "stock_quantity"},
		{"available but qty 0", func(o *RawOffer) { o.Quantity = "0" }, "stock_availability_mismatch"},
		{"no pictures", func(o *RawOffer) { o.Pictures = nil }, "picture_missing"},
		{"http picture", func(o *RawOffer) { o.Pictures = []string{"http://a/b.jpg"} }, "picture_url"},
		{"cyrillic picture", func(o *RawOffer) { o.Pictures = []string{"https://a/фото.jpg"} }, "picture_url"},
		{"plus in picture", func(o *RawOffer) { o.Pictures = []string{"https://a/a+b.jpg"} }, "picture_url"},
		{"no vendor", func(o *RawOffer) { o.Vendor = "" }, "vendor_missing"},
		{"no description", func(o *RawOffer) { o.Description = " " }, "description_missing"},
		{"no name", func(o *RawOffer) { o.Name = "" }, "name_missing"},
		{"param without name", func(o *RawOffer) { o.Params = append(o.Params, paramNoName()) }, "param_name"},
	}
	for _, c := range cases {
		o := good()
		c.mutate(&o)
		if got := rules(NewCtx(Rozetka, []string{"1"}).Check(o)); !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDuplicates(t *testing.T) {
	c := NewCtx(Rozetka, []string{"1"})
	a := good()
	c.Accept(a)
	if got := rules(c.Check(a)); !strings.Contains(got, "id_duplicate") || !strings.Contains(got, "name_duplicate") {
		t.Errorf("got %q", got)
	}
}

func TestPromIsMoreLenient(t *testing.T) {
	o := good()
	o.Vendor, o.Description, o.Pictures = "", "", nil
	if is := NewCtx(Prom, []string{"1"}).Check(o); len(is) != 0 {
		t.Errorf("prom should not require vendor/description/pictures: %v", is)
	}
	o.ID = "A-1" // dashes are fine for Prom
	if is := NewCtx(Prom, []string{"1"}).Check(o); len(is) != 0 {
		t.Errorf("prom id with dash: %v", is)
	}
}

func TestFileCatchesRealProblems(t *testing.T) {
	xmlDoc := `<?xml version="1.0" encoding="UTF-8"?>
<yml_catalog date="2026-09-21 12:00"><shop><currencies><currency id="UAH" rate="1"/></currencies>
<categories><category id="1">C</category></categories><offers>
<offer id="ok1" available="true"><price>10</price><currencyId>UAH</currencyId><categoryId>1</categoryId>
<picture>https://a/1.jpg</picture><vendor>V</vendor><stock_quantity>2</stock_quantity><name>N1</name><description>d</description></offer>
<offer id="bad-id" available="true"><price>10</price><currencyId>UAH</currencyId><categoryId>7</categoryId>
<picture>http://a/1.jpg</picture><stock_quantity>2</stock_quantity><name>N2</name></offer>
</offers></shop></yml_catalog>`
	path := filepath.Join(t.TempDir(), "r.xml")
	if err := os.WriteFile(path, []byte(xmlDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := File(path, Rozetka)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Offers != 2 || rep.Errors == 0 {
		t.Fatalf("report: %+v", rep)
	}
	for _, want := range []string{"id_format", "category_unknown", "picture_url", "vendor_missing", "description_missing"} {
		if rep.Issues[want] == 0 {
			t.Errorf("file validator missed %s: %v", want, rep.Issues)
		}
	}
}

func TestFileRejectsMalformedXML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.xml")
	os.WriteFile(path, []byte(`<yml_catalog date="2026-09-21 12:00"><shop><offers><offer id="a">`), 0o644)
	if _, err := File(path, Rozetka); err == nil {
		t.Error("expected a malformed XML error")
	}
}

func paramNoName() model.Param { return model.Param{Value: "v"} }
