package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/valpere/feedsync/internal/config"
	"github.com/valpere/feedsync/internal/model"
)

// ReadJSON streams a top-level JSON array of product objects. cfg.Fields maps
// product fields to JSON keys; params come from cfg.ParamsKey, either an object
// {"Name": "Value"} or a list of {"name": ..., "value": ...}.
func ReadJSON(ctx context.Context, cfg config.JSON, r io.Reader, meta *Meta, h Handler) error {
	dec := json.NewDecoder(r)
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("source: json: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return errors.New("source: json feed must be a top-level array")
	}
	key := func(field string) string {
		if k := cfg.Fields[field]; k != "" {
			return k
		}
		return field
	}
	for dec.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var raw map[string]any
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		str := func(field string) string { return strings.TrimSpace(fmt.Sprint(nz(raw[key(field)]))) }
		p := model.Product{
			SupplierID: str("id"), SKU: str("sku"), Name: str("name"), Description: str("description"),
			Vendor: str("vendor"), CategoryRef: str("category"), Currency: str("currency"), URL: str("url"),
		}
		p.Price, _ = model.ParseMoney(str("price"))
		p.OldPrice, _ = model.ParseMoney(str("old_price"))
		p.Quantity = parseQty(str("quantity"), "")
		switch v := raw[key("pictures")].(type) {
		case string:
			if v != "" {
				p.Pictures = []string{v}
			}
		case []any:
			for _, x := range v {
				if s := strings.TrimSpace(fmt.Sprint(x)); s != "" {
					p.Pictures = append(p.Pictures, s)
				}
			}
		}
		switch v := raw[cfg.ParamsKey].(type) {
		case map[string]any:
			names := make([]string, 0, len(v))
			for n := range v {
				names = append(names, n)
			}
			sort.Strings(names) // deterministic output
			for _, n := range names {
				p.Params = append(p.Params, model.Param{Name: n, Value: strings.TrimSpace(fmt.Sprint(v[n]))})
			}
		case []any:
			for _, x := range v {
				if m, ok := x.(map[string]any); ok {
					p.Params = append(p.Params, model.Param{Name: fmt.Sprint(m["name"]), Value: fmt.Sprint(m["value"])})
				}
			}
		}
		if err := h(p); err != nil {
			return err
		}
	}
	_, err = dec.Token()
	return err
}

func nz(v any) any {
	if v == nil {
		return ""
	}
	return v
}
