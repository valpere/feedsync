// Package source streams supplier products out of YML/XML, CSV and JSON feeds
// one product at a time, so memory stays flat regardless of feed size.
package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/valpere/feedsync/internal/config"
	"github.com/valpere/feedsync/internal/model"
)

// Handler receives each product as it is parsed. Returning an error stops the read.
type Handler func(model.Product) error

// Meta is filled while reading; supplier categories arrive before offers in YML.
type Meta struct {
	Categories map[string]string // supplier category id -> name
}

// Open returns a reader for a file path or http(s) URL.
func Open(ctx context.Context, cfg config.Source) (io.ReadCloser, error) {
	if strings.HasPrefix(cfg.Path, "http://") || strings.HasPrefix(cfg.Path, "https://") {
		ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Path, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cancel()
			return nil, fmt.Errorf("source: %s returned %s", cfg.Path, resp.Status)
		}
		return &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}, nil
	}
	return os.Open(cfg.Path)
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error { c.cancel(); return c.ReadCloser.Close() }

// Read dispatches on the configured format.
func Read(ctx context.Context, cfg config.Source, r io.Reader, meta *Meta, h Handler) error {
	switch cfg.Format {
	case "yml":
		return ReadYML(ctx, r, meta, h)
	case "csv":
		return ReadCSV(ctx, cfg.CSV, r, meta, h)
	case "json":
		return ReadJSON(ctx, cfg.JSON, r, meta, h)
	}
	return fmt.Errorf("source: unsupported format %q", cfg.Format)
}
