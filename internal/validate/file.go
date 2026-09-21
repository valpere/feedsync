package validate

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/valpere/feedsync/internal/model"
)

// FileReport is the outcome of validating a finished price-list file.
type FileReport struct {
	Target   string              `json:"target"`
	Offers   int                 `json:"offers"`
	Errors   int                 `json:"errors"`
	Issues   map[string]int      `json:"issues,omitempty"`
	Examples map[string][]string `json:"examples,omitempty"`
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$`)

type xmlOffer struct {
	ID          string   `xml:"id,attr"`
	Available   string   `xml:"available,attr"`
	Price       string   `xml:"price"`
	PriceOld    string   `xml:"price_old"`
	OldPrice    string   `xml:"oldprice"`
	CurrencyID  string   `xml:"currencyId"`
	CategoryID  string   `xml:"categoryId"`
	Pictures    []string `xml:"picture"`
	Vendor      string   `xml:"vendor"`
	StockQty    string   `xml:"stock_quantity"`
	QtyInStock  string   `xml:"quantity_in_stock"`
	Name        string   `xml:"name"`
	Description string   `xml:"description"`
	Params      []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:",chardata"`
	} `xml:"param"`
}

// File re-parses an XML price list and applies the marketplace rules to what
// is actually in it, independently of the code that wrote it.
func File(path, target string) (*FileReport, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rep := &FileReport{Target: target, Issues: map[string]int{}, Examples: map[string][]string{}}
	note := func(rule, detail string) {
		rep.Errors++
		rep.Issues[rule]++
		if len(rep.Examples[rule]) < 5 {
			rep.Examples[rule] = append(rep.Examples[rule], detail)
		}
	}

	dec := xml.NewDecoder(f)
	var cats []string
	var ctx *Ctx
	sawRoot, sawUAH := false, false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("validate: malformed XML: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "yml_catalog":
			sawRoot = true
			date := ""
			for _, a := range se.Attr {
				if a.Name.Local == "date" {
					date = a.Value
				}
			}
			if !dateRe.MatchString(date) {
				note("root_date", "yml_catalog date must be YYYY-MM-DD hh:mm, got "+date)
			}
		case "currency":
			id, rate := "", ""
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "id":
					id = a.Value
				case "rate":
					rate = a.Value
				}
			}
			if id == "UAH" {
				sawUAH = true
				if rate != "1" {
					note("currency_rate", "UAH rate must be 1, got "+rate)
				}
			}
		case "category":
			var c struct {
				ID string `xml:"id,attr"`
			}
			if err := dec.DecodeElement(&c, &se); err != nil {
				return nil, err
			}
			cats = append(cats, c.ID)
		case "offers":
			ctx = NewCtx(target, cats)
		case "offer":
			if ctx == nil {
				ctx = NewCtx(target, cats)
			}
			var xo xmlOffer
			if err := dec.DecodeElement(&xo, &se); err != nil {
				return nil, err
			}
			rep.Offers++
			raw := RawOffer{
				ID: xo.ID, Available: xo.Available, Name: xo.Name, Description: strings.TrimSpace(xo.Description),
				Vendor: xo.Vendor, Price: xo.Price, Currency: xo.CurrencyID, CategoryID: xo.CategoryID,
				Quantity: firstNonEmpty(xo.StockQty, xo.QtyInStock), Pictures: xo.Pictures,
				OldPrice: firstNonEmpty(xo.PriceOld, xo.OldPrice),
			}
			for _, p := range xo.Params {
				raw.Params = append(raw.Params, model.Param{Name: p.Name, Value: p.Value})
			}
			for _, is := range ctx.Check(raw) {
				note(is.Rule, "offer "+xo.ID+": "+is.Detail)
			}
			ctx.Accept(raw)
		}
	}
	if !sawRoot {
		note("root_missing", "no <yml_catalog> root element")
	}
	if !sawUAH {
		note("currency_missing", "no UAH currency with rate 1")
	}
	return rep, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
