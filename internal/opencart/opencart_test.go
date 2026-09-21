package opencart

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := EnsureSchema(context.Background(), db, "oc_", "sqlite"); err != nil {
		t.Fatal(err)
	}
	return db
}

func read(t *testing.T, db *sql.DB, m string) (string, int) {
	t.Helper()
	var p string
	var q int
	if err := db.QueryRow("SELECT price, quantity FROM oc_product WHERE model = ?", m).Scan(&p, &q); err != nil {
		t.Fatal(err)
	}
	return p, q
}

func TestSyncUpdatesOnlyChangedRows(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	if err := Seed(ctx, db, "oc_", []Item{{Model: "A", Price: 10000, Quantity: 5}, {Model: "B", Price: 20000, Quantity: 0}, {Model: "C", Price: 30000, Quantity: 7}, {Model: "Untouched", Price: 999, Quantity: 1}}); err != nil {
		t.Fatal(err)
	}
	items := []Item{
		{Model: "A", Price: 10000, Quantity: 5},   // unchanged
		{Model: "B", Price: 25000, Quantity: 3},   // price and stock change
		{Model: "C", Price: 30000, Quantity: 0},   // stock only
		{Model: "Ghost", Price: 100, Quantity: 1}, // not in the CMS
	}
	st, err := Sync(ctx, db, "oc_", items, 2)
	if err != nil {
		t.Fatal(err)
	}
	if st.Considered != 4 || st.Matched != 3 || st.Missing != 1 || st.Unchanged != 1 || st.Updated != 2 {
		t.Errorf("stats: %+v", st)
	}
	if p, q := read(t, db, "B"); p != "250" && p != "250.0" && p != "250.00" || q != 3 {
		t.Errorf("B = %s/%d", p, q)
	}
	if _, q := read(t, db, "C"); q != 0 {
		t.Errorf("C qty = %d", q)
	}
	if p, q := read(t, db, "Untouched"); q != 1 || p == "" {
		t.Errorf("row outside the batch was modified: %s/%d", p, q)
	}
}

func TestSyncBatching(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	var seed, items []Item
	for i := 0; i < 25; i++ {
		m := string(rune('a'+i%26)) + string(rune('A'+i/26))
		seed = append(seed, Item{Model: m, Price: 1000, Quantity: 1})
		items = append(items, Item{Model: m, Price: 2000, Quantity: 2})
	}
	if err := Seed(ctx, db, "oc_", seed); err != nil {
		t.Fatal(err)
	}
	st, err := Sync(ctx, db, "oc_", items, 10)
	if err != nil {
		t.Fatal(err)
	}
	if st.Updated != 25 || st.Batches != 3 {
		t.Errorf("want 25 updated in 3 batches, got %+v", st)
	}
	// idempotent: a second run has nothing to write
	st, _ = Sync(ctx, db, "oc_", items, 10)
	if st.Updated != 0 || st.Unchanged != 25 || st.Batches != 0 {
		t.Errorf("second run should be a no-op: %+v", st)
	}
}

func TestKeepPriceUpdatesStockOnly(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	if err := Seed(ctx, db, "oc_", []Item{{Model: "A", Price: 10000, Quantity: 5}, {Model: "B", Price: 20000, Quantity: 4}}); err != nil {
		t.Fatal(err)
	}
	// A goes out of stock (price untouched); B is a normal update in the same batch
	st, err := Sync(ctx, db, "oc_", []Item{{Model: "A", Quantity: 0, KeepPrice: true}, {Model: "B", Price: 25000, Quantity: 4}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if st.Updated != 2 {
		t.Fatalf("stats: %+v", st)
	}
	if p, q := read(t, db, "A"); q != 0 || (p != "100" && p != "100.0" && p != "100.00") {
		t.Errorf("A should keep its price and drop to 0 stock: %s/%d", p, q)
	}
	if p, _ := read(t, db, "B"); p == "200" || p == "200.0" {
		t.Errorf("B price was not updated: %s", p)
	}
	// only KeepPrice items in a batch: no price assignment in the SQL at all
	q, _ := updateSQL("oc_product", []Item{{Model: "A", KeepPrice: true}})
	if strings.Contains(q, "price =") {
		t.Errorf("unexpected price clause: %s", q)
	}
	// stock already 0 and KeepPrice: nothing to do
	if st, _ = Sync(ctx, db, "oc_", []Item{{Model: "A", Quantity: 0, KeepPrice: true}}, 10); st.Updated != 0 || st.Unchanged != 1 {
		t.Errorf("second run: %+v", st)
	}
}
