// Package offer is the marketplace-ready product: prices marked up and
// converted to UAH, IDs made stable, category mapped.
package offer

import "github.com/valpere/feedsync/internal/model"

type Offer struct {
	ID          string
	Available   bool
	Name        string
	Description string
	Vendor      string
	Article     string
	URL         string
	Price       model.Money
	OldPrice    model.Money
	Currency    string
	CategoryID  string
	Quantity    int
	Pictures    []string
	Params      []model.Param
}

// Formatting shared by the exporters and the validators, so what is checked
// before writing is exactly what gets written.

func (o Offer) PriceStr() string    { return o.Price.Format() }
func (o Offer) OldPriceStr() string { return o.OldPrice.Format() }

func (o Offer) AvailableStr() string {
	if o.Available {
		return "true"
	}
	return "false"
}
