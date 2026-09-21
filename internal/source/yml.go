package source

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/valpere/feedsync/internal/model"
)

// ymlOffer captures the offer tags suppliers commonly use; alternative tag
// names for the same field are accepted (quantity_in_stock / stock_quantity /
// quantity, vendorCode / article, oldprice / price_old).
type ymlOffer struct {
	ID          string   `xml:"id,attr"`
	Available   string   `xml:"available,attr"`
	URL         string   `xml:"url"`
	Name        string   `xml:"name"`
	Model       string   `xml:"model"`
	Price       string   `xml:"price"`
	OldPrice    string   `xml:"oldprice"`
	PriceOld    string   `xml:"price_old"`
	CurrencyID  string   `xml:"currencyId"`
	CategoryID  string   `xml:"categoryId"`
	Vendor      string   `xml:"vendor"`
	VendorCode  string   `xml:"vendorCode"`
	Article     string   `xml:"article"`
	Description string   `xml:"description"`
	Pictures    []string `xml:"picture"`
	QtyInStock  string   `xml:"quantity_in_stock"`
	StockQty    string   `xml:"stock_quantity"`
	Quantity    string   `xml:"quantity"`
	Params      []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:",chardata"`
	} `xml:"param"`
}

// ReadYML streams a yml_catalog document. Categories are collected into meta;
// each <offer> is decoded on its own and handed to h.
func ReadYML(ctx context.Context, r io.Reader, meta *Meta, h Handler) error {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "category":
			var c struct {
				ID   string `xml:"id,attr"`
				Name string `xml:",chardata"`
			}
			if err := dec.DecodeElement(&c, &se); err != nil {
				return err
			}
			if meta != nil && c.ID != "" {
				if meta.Categories == nil {
					meta.Categories = map[string]string{}
				}
				meta.Categories[c.ID] = strings.TrimSpace(c.Name)
			}
		case "offer":
			var o ymlOffer
			if err := dec.DecodeElement(&o, &se); err != nil {
				return err
			}
			if err := h(o.toProduct()); err != nil {
				return err
			}
		}
	}
}

func (o ymlOffer) toProduct() model.Product {
	p := model.Product{
		SupplierID:  strings.TrimSpace(o.ID),
		SKU:         first(o.VendorCode, o.Article),
		Name:        strings.TrimSpace(first(o.Name, o.Model)),
		Description: strings.TrimSpace(o.Description),
		Vendor:      strings.TrimSpace(o.Vendor),
		CategoryRef: strings.TrimSpace(o.CategoryID),
		Currency:    strings.TrimSpace(o.CurrencyID),
		URL:         strings.TrimSpace(o.URL),
	}
	p.Price, _ = model.ParseMoney(o.Price)
	p.OldPrice, _ = model.ParseMoney(first(o.OldPrice, o.PriceOld))
	p.Quantity = parseQty(first(o.QtyInStock, o.StockQty, o.Quantity), o.Available)
	for _, pic := range o.Pictures {
		if pic = strings.TrimSpace(pic); pic != "" {
			p.Pictures = append(p.Pictures, pic)
		}
	}
	for _, pr := range o.Params {
		if n, v := strings.TrimSpace(pr.Name), strings.TrimSpace(pr.Value); n != "" && v != "" {
			p.Params = append(p.Params, model.Param{Name: n, Value: v})
		}
	}
	return p
}

func first(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
