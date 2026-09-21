// Command feedsync converts supplier price feeds into Rozetka and Prom.ua
// price lists and keeps an OpenCart-style product table in sync.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"

	"github.com/valpere/feedsync/internal/config"
	"github.com/valpere/feedsync/internal/model"
	"github.com/valpere/feedsync/internal/opencart"
	"github.com/valpere/feedsync/internal/pipeline"
	"github.com/valpere/feedsync/internal/samplegen"
	"github.com/valpere/feedsync/internal/validate"
)

const usage = `feedsync - supplier feeds to Rozetka / Prom.ua price lists

usage:
  feedsync convert  -c config.yaml [-every 30m] [-opencart-dsn DSN]
  feedsync validate -target rozetka|prom FILE
  feedsync gen-sample -n 25000 [-seed 1] -o supplier.yml
  feedsync opencart-seed -dsn DSN -n 25000 [-prefix oc_]

DSN: a MySQL DSN (user:pass@tcp(host:3306)/db) or sqlite:PATH
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "convert":
		err = cmdConvert(ctx, os.Args[2:])
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "gen-sample":
		err = cmdGenSample(os.Args[2:])
	case "opencart-seed":
		err = cmdSeed(ctx, os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdConvert(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	path := fs.String("c", "", "config file (required)")
	every := fs.Duration("every", 0, "repeat on this interval (0 = run once)")
	dsn := fs.String("opencart-dsn", "", "sync price/stock into this OpenCart database")
	fs.Parse(args)
	if *path == "" {
		return fmt.Errorf("-c config.yaml is required")
	}
	for {
		cfg, err := config.Load(*path)
		if err != nil {
			return err
		}
		if *dsn != "" {
			cfg.OpenCart.DSN = *dsn
		}
		var db *sql.DB
		if cfg.OpenCart.DSN != "" {
			db, _, err = openDB(cfg.OpenCart.DSN)
			if err != nil {
				return err
			}
		}
		rep, runErr := pipeline.Run(ctx, cfg, pipeline.Options{DB: db})
		if db != nil {
			db.Close()
		}
		printSummary(rep)
		if runErr != nil {
			if *every == 0 {
				return runErr
			}
			fmt.Fprintln(os.Stderr, "run failed, previous files kept:", runErr)
		}
		if *every == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(*every):
		}
	}
}

func printSummary(r *pipeline.Report) {
	if r == nil {
		return
	}
	fmt.Printf("read %d products from %s in %d ms\n", r.Read, r.Source, r.DurationMS)
	if len(r.Dropped) > 0 {
		fmt.Printf("dropped before export: %s\n", counts(r.Dropped))
	}
	names := make([]string, 0, len(r.Targets))
	for n := range r.Targets {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t := r.Targets[n]
		fmt.Printf("%-8s %6d offers written, %d rejected", n, t.Offers, t.RejectedOffers)
		if len(t.Issues) > 0 {
			fmt.Printf(" (%s)", counts(t.Issues))
		}
		if t.FileValidation != nil {
			fmt.Printf("; file re-validated: %d errors", t.FileValidation.Errors)
		}
		fmt.Printf("  -> %s\n", t.Out)
	}
	if r.OpenCart != nil {
		s := r.OpenCart
		fmt.Printf("opencart considered %d: %d updated in %d batches, %d unchanged, %d not in the CMS (%d ms)\n",
			s.Considered, s.Updated, s.Batches, s.Unchanged, s.Missing, s.DurationMS)
	}
}

func counts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, m[k])
	}
	return strings.Join(parts, " ")
}

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	target := fs.String("target", "rozetka", "rozetka or prom")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: feedsync validate -target rozetka|prom FILE")
	}
	if *target != validate.Rozetka && *target != validate.Prom {
		return fmt.Errorf("unknown target %q", *target)
	}
	rep, err := validate.File(fs.Arg(0), *target)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	fmt.Println(string(b))
	if rep.Errors > 0 {
		os.Exit(1)
	}
	return nil
}

func cmdGenSample(args []string) error {
	fs := flag.NewFlagSet("gen-sample", flag.ExitOnError)
	n := fs.Int("n", 25000, "number of offers")
	seed := fs.Uint64("seed", 1, "random seed")
	out := fs.String("o", "testdata/generated/supplier.yml", "output file")
	fs.Parse(args)
	if err := os.MkdirAll(dirOf(*out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := samplegen.Write(f, samplegen.Options{N: *n, Seed: *seed}); err != nil {
		return err
	}
	fmt.Printf("wrote %d offers to %s\n", *n, *out)
	return nil
}

func cmdSeed(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("opencart-seed", flag.ExitOnError)
	dsn := fs.String("dsn", "", "database DSN (required)")
	n := fs.Int("n", 25000, "number of products")
	prefix := fs.String("prefix", "oc_", "table prefix")
	fs.Parse(args)
	if *dsn == "" {
		return fmt.Errorf("-dsn is required")
	}
	db, dialect, err := openDB(*dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := opencart.EnsureSchema(ctx, db, *prefix, dialect); err != nil {
		return err
	}
	items := make([]opencart.Item, 0, *n)
	for i := 1; i <= *n; i++ {
		if i%10 == 0 {
			continue // leave gaps: these show up as "not in the CMS"
		}
		items = append(items, opencart.Item{Model: fmt.Sprintf("SKU-%07d", i), Price: model.Money(10000), Quantity: 0})
	}
	if err := opencart.Seed(ctx, db, *prefix, items); err != nil {
		return err
	}
	fmt.Printf("seeded %d products into %sproduct\n", len(items), *prefix)
	return nil
}

func openDB(dsn string) (*sql.DB, string, error) {
	driver, dialect := "mysql", "mysql"
	if strings.HasPrefix(dsn, "sqlite:") {
		driver, dialect, dsn = "sqlite", "sqlite", strings.TrimPrefix(dsn, "sqlite:")
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, dialect, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, dialect, err
	}
	return db, dialect, nil
}

func dirOf(path string) string {
	if i := strings.LastIndex(path, "/"); i > 0 {
		return path[:i]
	}
	return "."
}
