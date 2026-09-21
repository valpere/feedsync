// Package samplegen writes a deterministic sample supplier feed so the
// pipeline can be exercised and measured at any size without real data. A
// small, fixed share of offers is deliberately broken (missing vendor,
// Cyrillic picture URL, unmapped category, unknown currency, duplicate names,
// ...) to show the validation and reporting working.
package samplegen

import (
	"bufio"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"

	"github.com/valpere/feedsync/internal/export"
)

var categories = []string{"Smartphones", "Laptops", "Tablets", "Headphones", "Monitors", "Keyboards",
	"Mice", "Routers", "Cameras", "Smartwatches", "Speakers", "Chargers"}

var vendors = []string{"Acme", "Nordic", "Helios", "Vertex", "Orbita", "Kvant", "Lumen", "Pixelor", "Sonar", "Tetra",
	"Zenit", "Boreal", "Cirrus", "Delta", "Ember", "Fjord", "Garnet", "Halcyon", "Indigo", "Juniper"}

var words = strings.Fields("reliable compact durable lightweight powerful silent modern classic premium ergonomic " +
	"wireless portable efficient precise smooth vivid crisp rugged sleek balanced advanced smart adaptive")

var paramNames = []string{"Color", "Weight", "Warranty", "Country", "Material", "Size"}
var colors = []string{"black", "white", "silver", "blue", "green", "red"}

// Options controls the size and the random seed of the feed.
type Options struct {
	N    int
	Seed uint64
}

// Write streams a yml_catalog with N offers to w.
func Write(w io.Writer, o Options) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	rng := rand.New(rand.NewPCG(o.Seed, o.Seed^0x9e3779b97f4a7c15))
	p := func(format string, a ...any) { fmt.Fprintf(bw, format, a...) }

	p("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<yml_catalog date=\"2026-09-21 12:00\">\n<shop>\n<name>Sample Supplier</name>\n<currencies><currency id=\"UAH\" rate=\"1\"/></currencies>\n<categories>\n")
	for i, c := range categories {
		p("<category id=\"%d\">%s</category>\n", 10+i, c)
	}
	p("</categories>\n<offers>\n")

	prevName := ""
	for i := 1; i <= o.N; i++ {
		catIdx := rng.IntN(len(categories))
		catID := fmt.Sprint(10 + catIdx)
		vendor := vendors[rng.IntN(len(vendors))]
		name := fmt.Sprintf("%s %s %s %d", vendor, categories[catIdx], words[rng.IntN(len(words))], 100+rng.IntN(900))
		if i%97 == 0 && prevName != "" {
			name = prevName // deliberate duplicate name, different SKU
		}
		prevName = name

		id := fmt.Sprintf("P%07d", i)
		if i%10 < 3 {
			id = fmt.Sprintf("SUP-%07d", i) // dash: not valid for Rozetka, exercises ID derivation
		}
		qty := 1 + rng.IntN(200)
		if rng.IntN(100) < 15 {
			qty = 0
		}
		price := float64(50+rng.IntN(60000)) + []float64{0, 0.5, 0.99}[rng.IntN(3)]
		currency := "UAH"
		switch r := rng.IntN(1000); {
		case r < 30:
			currency, price = "USD", price/41.5
		case r < 33:
			currency = "PLN" // not configured: dropped as unknown_currency
		}
		if i%200 == 0 {
			catID = "99" // unmapped supplier category
		}
		if i%211 == 0 {
			vendor = "" // Rozetka requires a vendor
		}

		p("<offer id=\"%s\" available=\"%t\">\n", id, qty > 0)
		p("<url>https://supplier.example.com/p/%d</url>\n<price>%.2f</price>\n<currencyId>%s</currencyId>\n<categoryId>%s</categoryId>\n", i, price, currency, catID)
		nPics := 1 + rng.IntN(3)
		if i%307 == 0 {
			nPics = 0 // no pictures
		}
		for k := 0; k < nPics; k++ {
			scheme := "https"
			if rng.IntN(2) == 0 {
				scheme = "http"
			}
			file := fmt.Sprintf("%d_%d.jpg", i, k)
			if i%251 == 0 && k == 0 {
				file = "фото.jpg" // Cyrillic path: Rozetka rejects it
			}
			p("<picture>%s://cdn.supplier.example.com/img/%s</picture>\n", scheme, file)
		}
		if vendor != "" {
			p("<vendor>%s</vendor>\n", vendor)
		}
		p("<vendorCode>SKU-%07d</vendorCode>\n<quantity_in_stock>%d</quantity_in_stock>\n<name>%s</name>\n", i, qty, export.Esc(name))
		p("<description><![CDATA[<p>%s</p>]]></description>\n", sentence(rng, 25+rng.IntN(55)))
		for k := 0; k < 4+rng.IntN(3); k++ {
			pn := paramNames[k%len(paramNames)]
			val := colors[rng.IntN(len(colors))]
			if pn != "Color" {
				val = fmt.Sprint(1 + rng.IntN(500))
			}
			p("<param name=\"%s\">%s</param>\n", pn, val)
		}
		p("</offer>\n")
	}
	p("</offers>\n</shop>\n</yml_catalog>\n")
	return bw.Flush()
}

func sentence(rng *rand.Rand, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(words[rng.IntN(len(words))])
	}
	return b.String() + "."
}
