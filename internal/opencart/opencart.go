// Package opencart updates price and stock in an OpenCart-style product table.
//
// The sync is two-way in the practical sense: it first reads the current
// state of the table, computes the difference against the fresh supplier data,
// and writes only the rows that actually changed, in batched CASE updates
// inside one transaction. Only price and quantity are touched, so every other
// column of a real oc_product row is left exactly as it was.
package opencart

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/valpere/feedsync/internal/model"
)

// Item is what the supplier data says about one CMS product, matched by
// oc_product.model (the supplier SKU).
type Item struct {
	Model    string
	Price    model.Money
	Quantity int
	// KeepPrice leaves the CMS price alone and updates stock only; used for
	// products that went out of stock, whose marked-up price is not computed.
	KeepPrice bool
}

type Stats struct {
	Considered int   `json:"considered"`
	Matched    int   `json:"matched"`   // model exists in the CMS
	Missing    int   `json:"missing"`   // model not in the CMS (not created)
	Unchanged  int   `json:"unchanged"` // matched, same price and quantity
	Updated    int   `json:"updated"`
	Batches    int   `json:"batches"`
	DurationMS int64 `json:"duration_ms"`
}

type current struct {
	price model.Money
	qty   int
}

// Sync applies items to <prefix>product. batch is the number of rows per UPDATE.
func Sync(ctx context.Context, db *sql.DB, prefix string, items []Item, batch int) (Stats, error) {
	start := time.Now()
	st := Stats{Considered: len(items)}
	if batch <= 0 {
		batch = 500
	}
	table := prefix + "product"

	rows, err := db.QueryContext(ctx, "SELECT model, price, quantity FROM "+table)
	if err != nil {
		return st, fmt.Errorf("opencart: read current state: %w", err)
	}
	state := map[string]current{}
	for rows.Next() {
		var m, price string
		var q int
		if err := rows.Scan(&m, &price, &q); err != nil {
			rows.Close()
			return st, err
		}
		p, _ := model.ParseMoney(price)
		state[m] = current{price: p, qty: q}
	}
	if err := rows.Err(); err != nil {
		return st, err
	}
	rows.Close()

	var changed []Item
	for _, it := range items {
		cur, ok := state[it.Model]
		switch {
		case !ok:
			st.Missing++
		case (it.KeepPrice || cur.price == it.Price) && cur.qty == it.Quantity:
			st.Matched++
			st.Unchanged++
		default:
			st.Matched++
			changed = append(changed, it)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return st, err
	}
	defer tx.Rollback()
	for i := 0; i < len(changed); i += batch {
		end := min(i+batch, len(changed))
		q, args := updateSQL(table, changed[i:end])
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return st, fmt.Errorf("opencart: batch update: %w", err)
		}
		st.Batches++
	}
	if err := tx.Commit(); err != nil {
		return st, err
	}
	st.Updated = len(changed)
	st.DurationMS = time.Since(start).Milliseconds()
	return st, nil
}

// updateSQL builds one UPDATE ... CASE statement for a batch. It works on
// MySQL and SQLite alike; ELSE keeps rows outside the batch untouched.
func updateSQL(table string, items []Item) (string, []any) {
	var priceCase, qtyCase, in strings.Builder
	var priceArgs, qtyArgs, inArgs []any
	priceCase.WriteString("price = CASE model")
	qtyCase.WriteString("quantity = CASE model")
	for i, it := range items {
		if !it.KeepPrice {
			priceCase.WriteString(" WHEN ? THEN ?")
			priceArgs = append(priceArgs, it.Model, it.Price.Format())
		}
		qtyCase.WriteString(" WHEN ? THEN ?")
		qtyArgs = append(qtyArgs, it.Model, it.Quantity)
		if i > 0 {
			in.WriteString(",")
		}
		in.WriteString("?")
		inArgs = append(inArgs, it.Model)
	}
	priceCase.WriteString(" ELSE price END")
	qtyCase.WriteString(" ELSE quantity END")
	sets := []string{qtyCase.String()}
	args := qtyArgs
	if len(priceArgs) > 0 { // every item keeps its price: no price assignment at all
		sets = append([]string{priceCase.String()}, sets...)
		args = append(append([]any{}, priceArgs...), qtyArgs...)
	}
	args = append(args, inArgs...)
	return "UPDATE " + table + " SET " + strings.Join(sets, ", ") + " WHERE model IN (" + in.String() + ")", args
}

// EnsureSchema creates a minimal product table for the demo. dialect is
// "mysql" or "sqlite". A real OpenCart install already has a richer table;
// the sync only relies on model, price and quantity.
func EnsureSchema(ctx context.Context, db *sql.DB, prefix, dialect string) error {
	id := "product_id INTEGER PRIMARY KEY AUTOINCREMENT"
	if dialect == "mysql" {
		id = "product_id INT AUTO_INCREMENT PRIMARY KEY"
	}
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %sproduct (
		%s,
		model VARCHAR(64) NOT NULL,
		price DECIMAL(15,4) NOT NULL DEFAULT 0,
		quantity INT NOT NULL DEFAULT 0,
		status INT NOT NULL DEFAULT 1,
		UNIQUE (model))`, prefix, id)
	_, err := db.ExecContext(ctx, ddl)
	return err
}

// Seed inserts products (idempotent on model) so the demo has something to sync.
func Seed(ctx context.Context, db *sql.DB, prefix string, items []Item) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO "+prefix+"product (model, price, quantity) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, it := range items {
		if _, err := stmt.ExecContext(ctx, it.Model, it.Price.Format(), it.Quantity); err != nil {
			return err
		}
	}
	return tx.Commit()
}
