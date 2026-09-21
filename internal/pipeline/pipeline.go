// Package pipeline wires the stages together in one streaming pass:
// read -> transform -> per-target validation -> write -> independent
// validation of the finished file -> atomic publish.
package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/valpere/feedsync/internal/config"
	"github.com/valpere/feedsync/internal/export"
	"github.com/valpere/feedsync/internal/model"
	"github.com/valpere/feedsync/internal/offer"
	"github.com/valpere/feedsync/internal/opencart"
	"github.com/valpere/feedsync/internal/source"
	"github.com/valpere/feedsync/internal/transform"
	"github.com/valpere/feedsync/internal/validate"
)

const maxExamples = 5

type TargetReport struct {
	Out            string               `json:"out"`
	Offers         int                  `json:"offers"`
	RejectedOffers int                  `json:"rejected_offers"`
	Issues         map[string]int       `json:"issues,omitempty"`
	Examples       map[string][]string  `json:"examples,omitempty"`
	FileValidation *validate.FileReport `json:"file_validation,omitempty"`
}

type Report struct {
	Source     string                   `json:"source"`
	StartedAt  time.Time                `json:"started_at"`
	DurationMS int64                    `json:"duration_ms"`
	Read       int                      `json:"read"`
	Dropped    map[string]int           `json:"dropped,omitempty"` // dropped before export, by reason
	Examples   map[string][]string      `json:"dropped_examples,omitempty"`
	Targets    map[string]*TargetReport `json:"targets"`
	OpenCart   *opencart.Stats          `json:"opencart,omitempty"`
}

type Options struct {
	// DB, when set, receives the price/stock sync after the files are published.
	DB *sql.DB
}

type target struct {
	name      string
	out       string
	tmp       string
	file      *os.File
	w         *export.Writer
	ctx       *validate.Ctx
	rep       *TargetReport
	published bool
}

// Run executes one full conversion. On any validation failure of a finished
// file nothing is published: the previous good file stays in place.
func Run(ctx context.Context, cfg *config.Config, opt Options) (*Report, error) {
	start := time.Now()
	rep := &Report{Source: cfg.Source.Path, StartedAt: start.UTC(), Dropped: map[string]int{},
		Examples: map[string][]string{}, Targets: map[string]*TargetReport{}}

	cats, catIDs := targetCategories(cfg)
	var targets []*target
	names := make([]string, 0, len(cfg.Targets))
	for n := range cfg.Targets {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out := cfg.Targets[n].Out
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return rep, err
		}
		f, err := os.CreateTemp(filepath.Dir(out), filepath.Base(out)+".*.tmp")
		if err != nil {
			return rep, err
		}
		t := &target{name: n, out: out, tmp: f.Name(), file: f, w: export.NewWriter(n, f),
			ctx: validate.NewCtx(n, catIDs), rep: &TargetReport{Out: out, Issues: map[string]int{}, Examples: map[string][]string{}}}
		rep.Targets[n] = t.rep
		targets = append(targets, t)
	}
	defer func() {
		for _, t := range targets { // remove leftovers of a failed run
			if !t.published {
				t.file.Close()
				os.Remove(t.tmp)
			}
		}
	}()

	shop := export.Shop{Name: cfg.Shop.Name, Company: cfg.Shop.Company, URL: cfg.Shop.URL}
	for _, t := range targets {
		t.w.Begin(shop, cats, start)
	}

	src, err := source.Open(ctx, cfg.Source)
	if err != nil {
		return rep, err
	}
	defer src.Close()

	tr := transform.New(cfg)
	seenNames := map[string]struct{}{}
	var items []opencart.Item
	meta := &source.Meta{}

	drop := func(reason, id string) {
		rep.Dropped[reason]++
		if len(rep.Examples[reason]) < maxExamples {
			rep.Examples[reason] = append(rep.Examples[reason], id)
		}
	}

	err = source.Read(ctx, cfg.Source, src, meta, func(p model.Product) error {
		rep.Read++
		o, reason := tr.Apply(p)
		if reason != "" {
			drop(reason, idOf(p))
			// An out-of-stock product must still reach the CMS as quantity 0,
			// otherwise the shop keeps selling what the supplier no longer has.
			if reason == transform.ReasonZeroStock && p.SKU != "" {
				items = append(items, opencart.Item{Model: p.SKU, Quantity: max(p.Quantity, 0), KeepPrice: true})
			}
			return nil
		}
		o = uniqueName(o, seenNames)
		if o.Article != "" {
			items = append(items, opencart.Item{Model: o.Article, Price: o.Price, Quantity: o.Quantity})
		}
		raw := validate.FromOffer(o)
		for _, t := range targets {
			issues := t.ctx.Check(raw)
			if len(issues) > 0 {
				t.rep.RejectedOffers++
				for _, is := range issues {
					t.rep.Issues[is.Rule]++
					if len(t.rep.Examples[is.Rule]) < maxExamples {
						t.rep.Examples[is.Rule] = append(t.rep.Examples[is.Rule], o.ID+": "+is.Detail)
					}
				}
				continue
			}
			t.ctx.Accept(raw)
			t.w.Offer(o)
		}
		return nil
	})
	if err != nil {
		return rep, fmt.Errorf("pipeline: read: %w", err)
	}

	for _, t := range targets {
		if err := t.w.End(); err != nil {
			return rep, err
		}
		if err := t.file.Close(); err != nil {
			return rep, err
		}
		t.rep.Offers = t.w.Count()
		fr, err := validate.File(t.tmp, t.name)
		if err != nil {
			return rep, err
		}
		t.rep.FileValidation = fr
		if fr.Errors > 0 {
			return rep, fmt.Errorf("pipeline: %s output failed independent validation (%d issues); previous file left in place", t.name, fr.Errors)
		}
	}
	for _, t := range targets { // publish atomically, only after everything passed
		if err := os.Rename(t.tmp, t.out); err != nil {
			return rep, err
		}
		t.published = true
	}

	if opt.DB != nil {
		st, err := opencart.Sync(ctx, opt.DB, cfg.OpenCart.Prefix, items, cfg.OpenCart.Batch)
		rep.OpenCart = &st
		if err != nil {
			return rep, err
		}
	}
	rep.DurationMS = time.Since(start).Milliseconds()
	if cfg.Report != "" {
		if err := writeReport(cfg.Report, rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

func idOf(p model.Product) string {
	if p.SupplierID != "" {
		return p.SupplierID
	}
	return p.SKU
}

// uniqueName disambiguates a repeated name with the article, since Rozetka
// requires a unique name per offer; if there is no article the duplicate is
// left for validation to reject.
func uniqueName(o offer.Offer, seen map[string]struct{}) offer.Offer {
	if _, dup := seen[o.Name]; dup && o.Article != "" {
		o.Name += " (" + o.Article + ")"
	}
	seen[o.Name] = struct{}{}
	return o
}

// targetCategories returns the deduplicated target categories in ID order.
func targetCategories(cfg *config.Config) ([]export.Category, []string) {
	byID := map[string]export.Category{}
	for _, c := range cfg.Categories {
		byID[c.ID] = export.Category{ID: c.ID, Name: c.Name, RzID: c.RzID}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	cats := make([]export.Category, 0, len(ids))
	for _, id := range ids {
		cats = append(cats, byID[id])
	}
	return cats, ids
}

func writeReport(path string, rep *Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
