package source

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/valpere/feedsync/internal/config"
	"github.com/valpere/feedsync/internal/model"
)

// ReadCSV streams a CSV feed; the header row is mapped to product fields via
// cfg.Columns (field -> header). Unknown headers listed in ParamColumns
// become product params.
func ReadCSV(ctx context.Context, cfg config.CSV, r io.Reader, meta *Meta, h Handler) error {
	cr := csv.NewReader(r)
	cr.Comma = []rune(cfg.Delimiter)[0]
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	head, err := cr.Read()
	if err != nil {
		return fmt.Errorf("source: csv header: %w", err)
	}
	idx := map[string]int{}
	for i, name := range head {
		idx[strings.TrimSpace(name)] = i
	}
	col := func(rec []string, field string) string {
		name := cfg.Columns[field]
		if name == "" {
			name = field
		}
		if i, ok := idx[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		p := model.Product{
			SupplierID:  col(rec, "id"),
			SKU:         col(rec, "sku"),
			Name:        col(rec, "name"),
			Description: col(rec, "description"),
			Vendor:      col(rec, "vendor"),
			CategoryRef: col(rec, "category"),
			Currency:    col(rec, "currency"),
			URL:         col(rec, "url"),
		}
		p.Price, _ = model.ParseMoney(col(rec, "price"))
		p.OldPrice, _ = model.ParseMoney(col(rec, "old_price"))
		if q := col(rec, "quantity"); q != "" {
			p.Quantity = parseQty(q, "")
		}
		if pics := col(rec, "pictures"); pics != "" {
			for _, s := range strings.Split(pics, cfg.PictureSeparator) {
				if s = strings.TrimSpace(s); s != "" {
					p.Pictures = append(p.Pictures, s)
				}
			}
		}
		for _, pc := range cfg.ParamColumns {
			if i, ok := idx[pc]; ok && i < len(rec) && strings.TrimSpace(rec[i]) != "" {
				p.Params = append(p.Params, model.Param{Name: pc, Value: strings.TrimSpace(rec[i])})
			}
		}
		if err := h(p); err != nil {
			return err
		}
	}
}

// parseQty reads a quantity like "12", "12.0", ">10" or "in stock"; when the
// value is not numeric it falls back to the available attribute.
func parseQty(q, available string) int {
	q = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(q), "><="))
	if q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(strings.ReplaceAll(q, ",", "."), 64); err == nil {
			return int(f)
		}
	}
	if strings.EqualFold(available, "true") {
		return 1
	}
	return 0
}
