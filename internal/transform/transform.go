// Package transform turns supplier products into marketplace offers:
// category mapping, currency conversion, markup rules, stock filters and
// stable, marketplace-safe offer IDs.
package transform

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/valpere/feedsync/internal/config"
	"github.com/valpere/feedsync/internal/model"
	"github.com/valpere/feedsync/internal/offer"
)

// Rejection reasons reported for products dropped before export.
const (
	ReasonUnmappedCategory = "unmapped_category"
	ReasonZeroStock        = "zero_stock"
	ReasonInvalidPrice     = "invalid_price"
	ReasonMissingID        = "missing_id"
	ReasonUnknownCurrency  = "unknown_currency"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9]+$`)

type Transformer struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Transformer { return &Transformer{cfg: cfg} }

// Apply returns the offer, or a non-empty rejection reason.
func (t *Transformer) Apply(p model.Product) (offer.Offer, string) {
	cat, ok := t.cfg.Categories[p.CategoryRef]
	if !ok {
		return offer.Offer{}, ReasonUnmappedCategory
	}
	if t.cfg.Filter.DropZeroStock && p.Quantity <= 0 {
		return offer.Offer{}, ReasonZeroStock
	}
	if p.Quantity < t.cfg.Filter.MinQuantity {
		return offer.Offer{}, ReasonZeroStock
	}
	if p.Price <= 0 {
		return offer.Offer{}, ReasonInvalidPrice
	}
	id := t.offerID(p)
	if id == "" {
		return offer.Offer{}, ReasonMissingID
	}

	price, old := p.Price, p.OldPrice
	cur := strings.ToUpper(strings.TrimSpace(p.Currency))
	if cur == "" {
		cur = "UAH"
	}
	if cur != "UAH" {
		rate, ok := t.cfg.CurrencyRates[cur]
		if !ok || rate <= 0 {
			return offer.Offer{}, ReasonUnknownCurrency
		}
		price, old = price.Scale(rate), old.Scale(rate)
	}
	price = t.markup(p.CategoryRef, price)
	if old > 0 {
		old = t.markup(p.CategoryRef, old)
		if old <= price {
			old = 0 // a "was" price must be higher than the current one
		}
	}

	pics := make([]string, 0, len(p.Pictures))
	for _, u := range p.Pictures {
		if t.cfg.Pictures.ForceHTTPS && strings.HasPrefix(u, "http://") {
			u = "https://" + strings.TrimPrefix(u, "http://")
		}
		pics = append(pics, u)
	}
	return offer.Offer{
		ID: id, Available: p.Quantity > 0, Name: p.Name, Description: p.Description,
		Vendor: p.Vendor, Article: p.SKU, URL: p.URL, Price: price, OldPrice: old,
		Currency: "UAH", CategoryID: cat.ID, Quantity: p.Quantity, Pictures: pics, Params: p.Params,
	}, ""
}

// offerID keeps a supplier ID that already satisfies the marketplace rule
// (letters and digits only); otherwise it derives a deterministic ID from it,
// so the same supplier product always maps to the same offer ID across runs.
// Stability matters: Rozetka does not allow offer IDs to change after publication.
func (t *Transformer) offerID(p model.Product) string {
	src := p.SupplierID
	if src == "" {
		src = p.SKU
	}
	if src == "" {
		return ""
	}
	if safeID.MatchString(src) {
		return src
	}
	sum := sha1.Sum([]byte(src))
	return t.cfg.IDs.Prefix + hex.EncodeToString(sum[:])[:12]
}

// markup applies the first matching rule (category and price range), or the
// default rule, then optional round-up to whole currency units.
func (t *Transformer) markup(categoryRef string, price model.Money) model.Money {
	rule := t.cfg.Markup.Default
	units := float64(price) / 100
	for _, r := range t.cfg.Markup.Rules {
		if r.Category != "" && r.Category != categoryRef {
			continue
		}
		if units < r.PriceFrom || (r.PriceTo != 0 && units >= r.PriceTo) {
			continue
		}
		rule = r
		break
	}
	switch rule.Type {
	case "percent":
		price = price.Scale(1 + rule.Value/100)
	case "fixed":
		price += model.Money(rule.Value * 100)
	}
	return price.RoundUpTo(t.cfg.Markup.RoundUp)
}
