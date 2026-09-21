// Package validate checks offers against marketplace price-list rules. The
// same rules run on the offers before they are written and, independently, on
// the finished XML file, so "valid" is verified on the real output.
package validate

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/valpere/feedsync/internal/model"
	"github.com/valpere/feedsync/internal/offer"
)

// RawOffer is an offer as plain strings, the way it appears in the file.
type RawOffer struct {
	ID, Available, Name, Description, Vendor string
	Price, OldPrice, Currency, CategoryID    string
	Quantity                                 string
	Pictures                                 []string
	Params                                   []model.Param
}

// FromOffer renders an offer exactly as the exporters will write it.
func FromOffer(o offer.Offer) RawOffer {
	r := RawOffer{
		ID: o.ID, Available: o.AvailableStr(), Name: o.Name, Description: o.Description, Vendor: o.Vendor,
		Price: o.PriceStr(), Currency: o.Currency, CategoryID: o.CategoryID,
		Quantity: strconv.Itoa(o.Quantity), Pictures: o.Pictures, Params: o.Params,
	}
	if o.OldPrice > 0 {
		r.OldPrice = o.OldPriceStr()
	}
	return r
}

type Issue struct {
	Rule   string
	Detail string
}

const (
	Rozetka = "rozetka"
	Prom    = "prom"
)

var (
	idRe      = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	pictureRe = regexp.MustCompile(`^https://[\x21-\x7e]+$`) // https, printable ASCII: no spaces, no Cyrillic
)

// Ctx carries the cross-offer state: known categories, seen IDs and names.
type Ctx struct {
	Target     string
	Categories map[string]struct{}
	ids, names map[string]struct{}
}

func NewCtx(target string, categories []string) *Ctx {
	c := &Ctx{Target: target, Categories: map[string]struct{}{}, ids: map[string]struct{}{}, names: map[string]struct{}{}}
	for _, id := range categories {
		c.Categories[id] = struct{}{}
	}
	return c
}

// Accept registers an offer's ID and name for the duplicate checks.
func (c *Ctx) Accept(o RawOffer) {
	c.ids[o.ID] = struct{}{}
	c.names[o.Name] = struct{}{}
}

// Check returns every rule the offer breaks; it does not register the offer.
func (c *Ctx) Check(o RawOffer) []Issue {
	var out []Issue
	add := func(rule, detail string) { out = append(out, Issue{rule, detail}) }
	rozetka := c.Target == Rozetka

	if o.ID == "" {
		add("id_missing", "offer id is empty")
	} else {
		if rozetka && !idRe.MatchString(o.ID) {
			add("id_format", "Rozetka offer id must be letters and digits only: "+o.ID)
		}
		if _, dup := c.ids[o.ID]; dup {
			add("id_duplicate", o.ID)
		}
	}
	if o.Available != "true" && o.Available != "false" {
		add("available_value", "available must be true or false, got "+strconv.Quote(o.Available))
	}
	price, perr := model.ParseMoney(o.Price)
	if perr != nil || price <= 0 {
		add("price", "price must be a positive number, got "+strconv.Quote(o.Price))
	}
	if o.OldPrice != "" {
		if old, err := model.ParseMoney(o.OldPrice); err != nil || old <= price {
			add("price_old", "price_old must be greater than price")
		}
	}
	switch o.Currency {
	case "UAH", "USD", "EUR":
	default:
		add("currency", "currencyId must be UAH, USD or EUR, got "+strconv.Quote(o.Currency))
	}
	if _, ok := c.Categories[o.CategoryID]; !ok {
		add("category_unknown", "categoryId "+strconv.Quote(o.CategoryID)+" is not in <categories>")
	}
	qty, qerr := strconv.Atoi(o.Quantity)
	if rozetka || o.Quantity != "" {
		if qerr != nil {
			add("stock_quantity", "stock quantity must be an integer, got "+strconv.Quote(o.Quantity))
		} else if (o.Available == "true") != (qty > 0) {
			add("stock_availability_mismatch", "available="+o.Available+" but quantity="+o.Quantity)
		}
	}

	if rozetka {
		if len(o.Pictures) == 0 {
			add("picture_missing", "at least one picture is required")
		}
		if len(o.Pictures) > 15 {
			add("picture_count", "more than 15 pictures")
		}
		if o.Vendor == "" {
			add("vendor_missing", "vendor is required")
		}
		if strings.TrimSpace(o.Description) == "" {
			add("description_missing", "description is required")
		}
		if utf8.RuneCountInString(o.Description) > 50000 {
			add("description_length", "description exceeds 50000 characters")
		}
	}
	for _, u := range o.Pictures {
		ok := pictureRe.MatchString(u) && !strings.Contains(u, "+") && len(u) <= 1999
		if !rozetka {
			ok = (strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) && !strings.ContainsAny(u, " ")
		}
		if !ok {
			add("picture_url", "bad picture URL: "+u)
		}
	}
	if strings.TrimSpace(o.Name) == "" {
		add("name_missing", "name is required")
	} else {
		if utf8.RuneCountInString(o.Name) > 255 {
			add("name_length", "name exceeds 255 characters")
		}
		if rozetka {
			if _, dup := c.names[o.Name]; dup {
				add("name_duplicate", "name must be unique per offer: "+o.Name)
			}
		}
	}
	for _, p := range o.Params {
		if strings.TrimSpace(p.Name) == "" {
			add("param_name", "param without a name")
		}
		if rozetka && utf8.RuneCountInString(p.Value) > 500 {
			add("param_length", "param value exceeds 500 characters: "+p.Name)
		}
	}
	return out
}
