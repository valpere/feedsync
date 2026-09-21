package pipeline

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/valpere/feedsync/internal/config"
	"github.com/valpere/feedsync/internal/opencart"
	"github.com/valpere/feedsync/internal/samplegen"
	"github.com/valpere/feedsync/internal/validate"
)

func testConfig(t *testing.T, feed string) (*config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "supplier.yml")
	if err := os.WriteFile(src, []byte(feed), 0o644); err != nil {
		t.Fatal(err)
	}
	cats := map[string]config.Category{}
	for i := 0; i < 12; i++ {
		cats[string(rune('0'+(10+i)/10))+string(rune('0'+(10+i)%10))] = config.Category{ID: "10" + string(rune('a'+i)), Name: "Cat"}
	}
	return &config.Config{
		Source:        config.Source{Format: "yml", Path: src},
		Categories:    cats,
		CurrencyRates: map[string]float64{"USD": 41.5},
		Markup:        config.Markup{Default: config.Rule{Type: "percent", Value: 10}},
		Filter:        config.Filter{DropZeroStock: true},
		Pictures:      config.Pictures{ForceHTTPS: true},
		IDs:           config.IDs{Prefix: "s"},
		Targets: map[string]config.Target{
			"rozetka": {Out: filepath.Join(dir, "out", "rozetka.xml")},
			"prom":    {Out: filepath.Join(dir, "out", "prom.xml")},
		},
		Report:   filepath.Join(dir, "out", "report.json"),
		OpenCart: config.OpenCart{Prefix: "oc_", Batch: 100},
	}, dir
}

func sample(t *testing.T, n int) string {
	t.Helper()
	var b bytes.Buffer
	if err := samplegen.Write(&b, samplegen.Options{N: n, Seed: 7}); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestRunProducesValidFilesAndAReport(t *testing.T) {
	cfg, _ := testConfig(t, sample(t, 2000))
	rep, err := Run(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Read != 2000 {
		t.Errorf("read %d", rep.Read)
	}
	for _, name := range []string{"rozetka", "prom"} {
		out := cfg.Targets[name].Out
		fr, err := validate.File(out, name) // a fresh, independent check of the published file
		if err != nil {
			t.Fatal(err)
		}
		if fr.Errors != 0 || fr.Offers == 0 || fr.Offers != rep.Targets[name].Offers {
			t.Errorf("%s: file report %+v vs pipeline %d", name, fr, rep.Targets[name].Offers)
		}
	}
	// the generator deliberately breaks a few offers: the report must say why
	if rep.Targets["rozetka"].RejectedOffers == 0 || rep.Targets["rozetka"].Issues["vendor_missing"] == 0 {
		t.Errorf("expected vendor_missing rejections: %+v", rep.Targets["rozetka"])
	}
	if rep.Targets["prom"].RejectedOffers >= rep.Targets["rozetka"].RejectedOffers {
		t.Error("Prom is more lenient and should reject fewer offers")
	}
	if rep.Dropped["zero_stock"] == 0 || rep.Dropped["unmapped_category"] == 0 {
		t.Errorf("dropped: %v", rep.Dropped)
	}
	if _, err := os.Stat(cfg.Report); err != nil {
		t.Errorf("report file missing: %v", err)
	}
}

func TestBrokenSourceKeepsThePreviousFile(t *testing.T) {
	cfg, _ := testConfig(t, sample(t, 200))
	if _, err := Run(context.Background(), cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	good, _ := os.ReadFile(cfg.Targets["rozetka"].Out)

	// truncate the feed mid-offer: the run must fail and leave the old file alone
	feed, _ := os.ReadFile(cfg.Source.Path)
	os.WriteFile(cfg.Source.Path, feed[:len(feed)/2], 0o644)
	if _, err := Run(context.Background(), cfg, Options{}); err == nil {
		t.Fatal("expected an error for a truncated feed")
	}
	after, _ := os.ReadFile(cfg.Targets["rozetka"].Out)
	if !bytes.Equal(good, after) {
		t.Error("published file changed after a failed run")
	}
	entries, _ := os.ReadDir(filepath.Dir(cfg.Targets["rozetka"].Out))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestDuplicateNamesAreDisambiguated(t *testing.T) {
	feed := `<yml_catalog><shop><categories><category id="10">C</category></categories><offers>
<offer id="a1" available="true"><price>100</price><currencyId>UAH</currencyId><categoryId>10</categoryId><picture>https://x/1.jpg</picture><vendor>V</vendor><vendorCode>K1</vendorCode><stock_quantity>1</stock_quantity><name>Same</name><description>d</description></offer>
<offer id="a2" available="true"><price>100</price><currencyId>UAH</currencyId><categoryId>10</categoryId><picture>https://x/2.jpg</picture><vendor>V</vendor><vendorCode>K2</vendorCode><stock_quantity>1</stock_quantity><name>Same</name><description>d</description></offer>
</offers></shop></yml_catalog>`
	cfg, _ := testConfig(t, feed)
	rep, err := Run(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Targets["rozetka"].Offers != 2 {
		t.Fatalf("both offers should be published: %+v", rep.Targets["rozetka"])
	}
	out, _ := os.ReadFile(cfg.Targets["rozetka"].Out)
	if !strings.Contains(string(out), "Same (K2)") {
		t.Error("second duplicate should carry its article")
	}
}

func TestOpenCartSyncAfterPublish(t *testing.T) {
	cfg, _ := testConfig(t, sample(t, 300))
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx := context.Background()
	if err := opencart.EnsureSchema(ctx, db, "oc_", "sqlite"); err != nil {
		t.Fatal(err)
	}
	var seed []opencart.Item
	for i := 1; i <= 300; i++ {
		if i%10 != 0 {
			seed = append(seed, opencart.Item{Model: fmt.Sprintf("SKU-%07d", i), Price: 100, Quantity: 0})
		}
	}
	if err := opencart.Seed(ctx, db, "oc_", seed); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(ctx, cfg, Options{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	if rep.OpenCart == nil || rep.OpenCart.Updated == 0 || rep.OpenCart.Missing == 0 {
		t.Errorf("opencart stats: %+v", rep.OpenCart)
	}
	// products the supplier reports as out of stock must be zeroed in the CMS too
	var zero, nonZero int
	db.QueryRow("SELECT COUNT(*) FROM oc_product WHERE quantity = 0").Scan(&zero)
	db.QueryRow("SELECT COUNT(*) FROM oc_product WHERE quantity > 0").Scan(&nonZero)
	if nonZero == 0 || zero == 0 || rep.Dropped["zero_stock"] == 0 {
		t.Errorf("stock not synced: zero=%d nonzero=%d dropped=%v", zero, nonZero, rep.Dropped)
	}
	rep2, err := Run(ctx, cfg, Options{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	if rep2.OpenCart.Updated != 0 {
		t.Errorf("second run should change nothing: %+v", rep2.OpenCart)
	}
}
