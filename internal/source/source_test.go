package source

import (
	"context"
	"strings"
	"testing"

	"github.com/valpere/feedsync/internal/config"
	"github.com/valpere/feedsync/internal/model"
)

func collect(t *testing.T, read func(Handler) error) []model.Product {
	t.Helper()
	var out []model.Product
	if err := read(func(p model.Product) error { out = append(out, p); return nil }); err != nil {
		t.Fatal(err)
	}
	return out
}

const ymlFeed = `<?xml version="1.0"?><yml_catalog><shop>
<categories><category id="10">Phones</category></categories>
<offers>
<offer id="a1" available="true"><price>1 234,50</price><currencyId>UAH</currencyId><categoryId>10</categoryId>
<picture> https://x/1.jpg </picture><picture>https://x/2.jpg</picture><vendor>Acme</vendor><vendorCode>K-1</vendorCode>
<stock_quantity>7</stock_quantity><name>Phone</name><description><![CDATA[<b>hi</b>]]></description>
<param name="Color">black</param><param name="Empty"> </param></offer>
<offer id="a2" available="true"><price>10</price><categoryId>10</categoryId><name>NoQty</name></offer>
</offers></shop></yml_catalog>`

func TestReadYML(t *testing.T) {
	meta := &Meta{}
	ps := collect(t, func(h Handler) error { return ReadYML(context.Background(), strings.NewReader(ymlFeed), meta, h) })
	if len(ps) != 2 || meta.Categories["10"] != "Phones" {
		t.Fatalf("got %d products, meta %v", len(ps), meta.Categories)
	}
	p := ps[0]
	if p.SupplierID != "a1" || p.Price != 123450 || p.Quantity != 7 || p.SKU != "K-1" || p.Vendor != "Acme" ||
		len(p.Pictures) != 2 || p.Pictures[0] != "https://x/1.jpg" || p.Description != "<b>hi</b>" || len(p.Params) != 1 {
		t.Errorf("parsed: %+v", p)
	}
	if ps[1].Quantity != 1 { // no quantity tag: falls back to available="true"
		t.Errorf("fallback qty = %d", ps[1].Quantity)
	}
}

func TestReadYMLStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ReadYML(ctx, strings.NewReader(ymlFeed), &Meta{}, func(model.Product) error { return nil }); err == nil {
		t.Error("expected a cancellation error")
	}
}

func TestReadCSV(t *testing.T) {
	feed := "Код;Назва;Ціна;Категорія;Залишок;Фото;Колір\nA1;Phone;1200,5;10;3;https://x/1.jpg|https://x/2.jpg;black\nA2;Case;50;10;0;;\n"
	cfg := config.CSV{Delimiter: ";", PictureSeparator: "|", ParamColumns: []string{"Колір"},
		Columns: map[string]string{"id": "Код", "name": "Назва", "price": "Ціна", "category": "Категорія", "quantity": "Залишок", "pictures": "Фото"}}
	ps := collect(t, func(h Handler) error { return ReadCSV(context.Background(), cfg, strings.NewReader(feed), &Meta{}, h) })
	if len(ps) != 2 || ps[0].Price != 120050 || ps[0].Quantity != 3 || len(ps[0].Pictures) != 2 ||
		len(ps[0].Params) != 1 || ps[0].Params[0].Value != "black" || ps[1].Quantity != 0 || len(ps[1].Params) != 0 {
		t.Errorf("parsed: %+v", ps)
	}
}

func TestReadJSON(t *testing.T) {
	feed := `[{"sku":"A1","title":"Phone","cost":"99.9","cat":10,"stock":4,"imgs":["https://x/1.jpg"],
	"params":{"Color":"black","Weight":2}},{"sku":"A2","title":"Case","cost":5,"cat":"10","stock":0}]`
	cfg := config.JSON{ParamsKey: "params", Fields: map[string]string{"id": "sku", "sku": "sku", "name": "title", "price": "cost", "category": "cat", "quantity": "stock", "pictures": "imgs"}}
	ps := collect(t, func(h Handler) error { return ReadJSON(context.Background(), cfg, strings.NewReader(feed), &Meta{}, h) })
	if len(ps) != 2 || ps[0].SupplierID != "A1" || ps[0].Price != 9990 || ps[0].CategoryRef != "10" || ps[0].Quantity != 4 {
		t.Fatalf("parsed: %+v", ps)
	}
	if len(ps[0].Params) != 2 || ps[0].Params[0].Name != "Color" || ps[0].Params[1].Name != "Weight" {
		t.Errorf("params not deterministic/complete: %v", ps[0].Params)
	}
}

func TestReadJSONRejectsNonArray(t *testing.T) {
	if err := ReadJSON(context.Background(), config.JSON{}, strings.NewReader(`{"a":1}`), &Meta{}, nil); err == nil {
		t.Error("expected an error for a non-array feed")
	}
}
