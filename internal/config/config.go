// Package config loads and validates the YAML pipeline configuration.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Source        Source              `yaml:"source"`
	Categories    map[string]Category `yaml:"categories"`     // supplier category id -> target category
	CurrencyRates map[string]float64  `yaml:"currency_rates"` // supplier currency -> UAH rate (e.g. USD: 41.2)
	Markup        Markup              `yaml:"markup"`
	Filter        Filter              `yaml:"filter"`
	Pictures      Pictures            `yaml:"pictures"`
	IDs           IDs                 `yaml:"ids"`
	Shop          Shop                `yaml:"shop"`
	Targets       map[string]Target   `yaml:"targets"` // "rozetka", "prom"
	Report        string              `yaml:"report"`
	OpenCart      OpenCart            `yaml:"opencart"`
}

type Source struct {
	Format  string        `yaml:"format"` // yml | csv | json
	Path    string        `yaml:"path"`   // file path or http(s) URL
	Timeout time.Duration `yaml:"timeout"`
	CSV     CSV           `yaml:"csv"`
	JSON    JSON          `yaml:"json"`
}

type CSV struct {
	Delimiter        string            `yaml:"delimiter"`
	Columns          map[string]string `yaml:"columns"` // product field -> header
	ParamColumns     []string          `yaml:"param_columns"`
	PictureSeparator string            `yaml:"picture_separator"`
	CategoryNames    bool              `yaml:"category_is_name"` // categories keyed by name, not id
}

type JSON struct {
	Fields    map[string]string `yaml:"fields"` // product field -> JSON key
	ParamsKey string            `yaml:"params_key"`
}

type Category struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	RzID string `yaml:"rz_id"` // optional Rozetka category id
}

type Markup struct {
	Default Rule   `yaml:"default"`
	Rules   []Rule `yaml:"rules"` // first match wins
	RoundUp int64  `yaml:"round_up_to"`
}

type Rule struct {
	Category  string  `yaml:"category"`   // supplier category id, empty = any
	PriceFrom float64 `yaml:"price_from"` // inclusive, in currency units
	PriceTo   float64 `yaml:"price_to"`   // exclusive, 0 = no upper bound
	Type      string  `yaml:"type"`       // percent | fixed
	Value     float64 `yaml:"value"`
}

type Filter struct {
	DropZeroStock bool `yaml:"drop_zero_stock"`
	MinQuantity   int  `yaml:"min_quantity"`
}

type Pictures struct {
	ForceHTTPS bool `yaml:"force_https"`
}

type IDs struct {
	Prefix string `yaml:"prefix"`
}

type Shop struct {
	Name    string `yaml:"name"`
	Company string `yaml:"company"`
	URL     string `yaml:"url"`
}

type Target struct {
	Out string `yaml:"out"`
}

type OpenCart struct {
	DSN    string `yaml:"dsn"`
	Prefix string `yaml:"prefix"`
	Batch  int    `yaml:"batch"`
}

// Load reads, defaults and validates a config file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	c.applyDefaults()
	return &c, c.Validate()
}

func (c *Config) applyDefaults() {
	if c.Source.Timeout == 0 {
		c.Source.Timeout = 30 * time.Second
	}
	if c.Source.CSV.Delimiter == "" {
		c.Source.CSV.Delimiter = ","
	}
	if c.Source.CSV.PictureSeparator == "" {
		c.Source.CSV.PictureSeparator = "|"
	}
	if c.Source.JSON.ParamsKey == "" {
		c.Source.JSON.ParamsKey = "params"
	}
	if c.IDs.Prefix == "" {
		c.IDs.Prefix = "s"
	}
	if c.Markup.Default.Type == "" {
		c.Markup.Default = Rule{Type: "percent", Value: 0}
	}
	if c.OpenCart.Prefix == "" {
		c.OpenCart.Prefix = "oc_"
	}
	if c.OpenCart.Batch == 0 {
		c.OpenCart.Batch = 500
	}
}

// Validate checks that the config is internally consistent.
func (c *Config) Validate() error {
	switch c.Source.Format {
	case "yml", "csv", "json":
	default:
		return fmt.Errorf("config: source.format must be yml, csv or json (got %q)", c.Source.Format)
	}
	if c.Source.Path == "" {
		return fmt.Errorf("config: source.path is required")
	}
	if len(c.Categories) == 0 {
		return fmt.Errorf("config: categories mapping is empty")
	}
	for k, v := range c.Categories {
		if v.ID == "" || v.Name == "" {
			return fmt.Errorf("config: categories[%q] needs id and name", k)
		}
	}
	for i, r := range append([]Rule{c.Markup.Default}, c.Markup.Rules...) {
		if r.Type != "percent" && r.Type != "fixed" {
			return fmt.Errorf("config: markup rule %d: type must be percent or fixed (got %q)", i, r.Type)
		}
		if r.PriceTo != 0 && r.PriceTo <= r.PriceFrom {
			return fmt.Errorf("config: markup rule %d: price_to must be greater than price_from", i)
		}
	}
	for name, t := range c.Targets {
		if name != "rozetka" && name != "prom" {
			return fmt.Errorf("config: unknown target %q (use rozetka or prom)", name)
		}
		if t.Out == "" {
			return fmt.Errorf("config: targets.%s.out is required", name)
		}
	}
	return nil
}
