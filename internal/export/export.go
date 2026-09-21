package export

import (
	"bufio"
	"fmt"
	"io"
	"time"

	"github.com/valpere/feedsync/internal/offer"
)

const (
	Rozetka = "rozetka"
	Prom    = "prom"
)

// Category is a target-marketplace category written into <categories>.
type Category struct {
	ID, Name, RzID string
}

type Shop struct {
	Name, Company, URL string
}

// Writer streams one price list. Tag names that differ between marketplaces
// (article/vendorCode, stock_quantity/quantity_in_stock, price_old/oldprice)
// are chosen by target.
type Writer struct {
	target string
	bw     *bufio.Writer
	err    error
	n      int
}

func NewWriter(target string, w io.Writer) *Writer {
	return &Writer{target: target, bw: bufio.NewWriterSize(w, 1<<20)}
}

func (w *Writer) printf(format string, a ...any) {
	if w.err == nil {
		_, w.err = fmt.Fprintf(w.bw, format, a...)
	}
}

func (w *Writer) Begin(shop Shop, cats []Category, at time.Time) {
	w.printf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<yml_catalog date=\"%s\">\n<shop>\n", at.Format("2006-01-02 15:04"))
	if shop.Name != "" {
		w.printf("<name>%s</name>\n", Esc(shop.Name))
	}
	if shop.Company != "" {
		w.printf("<company>%s</company>\n", Esc(shop.Company))
	}
	if shop.URL != "" {
		w.printf("<url>%s</url>\n", Esc(shop.URL))
	}
	w.printf("<currencies>\n<currency id=\"UAH\" rate=\"1\"/>\n</currencies>\n<categories>\n")
	for _, c := range cats {
		if w.target == Rozetka && c.RzID != "" {
			w.printf("<category id=\"%s\" rz_id=\"%s\">%s</category>\n", Esc(c.ID), Esc(c.RzID), Esc(c.Name))
		} else {
			w.printf("<category id=\"%s\">%s</category>\n", Esc(c.ID), Esc(c.Name))
		}
	}
	w.printf("</categories>\n<offers>\n")
}

func (w *Writer) Offer(o offer.Offer) {
	w.n++
	w.printf("<offer id=\"%s\" available=\"%s\">\n", Esc(o.ID), o.AvailableStr())
	if o.URL != "" {
		w.printf("<url>%s</url>\n", Esc(o.URL))
	}
	w.printf("<price>%s</price>\n", o.PriceStr())
	if o.OldPrice > 0 {
		tag := "price_old"
		if w.target == Prom {
			tag = "oldprice"
		}
		w.printf("<%s>%s</%s>\n", tag, o.OldPriceStr(), tag)
	}
	w.printf("<currencyId>%s</currencyId>\n<categoryId>%s</categoryId>\n", Esc(o.Currency), Esc(o.CategoryID))
	for _, p := range o.Pictures {
		w.printf("<picture>%s</picture>\n", Esc(p))
	}
	if o.Vendor != "" {
		w.printf("<vendor>%s</vendor>\n", Esc(o.Vendor))
	}
	if o.Article != "" {
		tag := "article"
		if w.target == Prom {
			tag = "vendorCode"
		}
		w.printf("<%s>%s</%s>\n", tag, Esc(o.Article), tag)
	}
	qtag := "stock_quantity"
	if w.target == Prom {
		qtag = "quantity_in_stock"
	}
	w.printf("<%s>%d</%s>\n", qtag, o.Quantity, qtag)
	w.printf("<name>%s</name>\n", Esc(o.Name))
	if o.Description != "" {
		w.printf("<description>%s</description>\n", cdata(o.Description))
	}
	for _, p := range o.Params {
		w.printf("<param name=\"%s\">%s</param>\n", Esc(p.Name), Esc(p.Value))
	}
	w.printf("</offer>\n")
}

// End closes the document and flushes; it returns the first write error.
func (w *Writer) End() error {
	w.printf("</offers>\n</shop>\n</yml_catalog>\n")
	if w.err == nil {
		w.err = w.bw.Flush()
	}
	return w.err
}

// Count is the number of offers written so far.
func (w *Writer) Count() int { return w.n }
