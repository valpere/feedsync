package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, yml string) (*Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

const base = `
source: {format: yml, path: f.yml}
categories: {"1": {id: "10", name: "A"}}
targets: {rozetka: {out: out/r.xml}}
`

func TestLoadDefaults(t *testing.T) {
	c, err := load(t, base)
	if err != nil {
		t.Fatal(err)
	}
	if c.Source.Timeout == 0 || c.IDs.Prefix != "s" || c.OpenCart.Prefix != "oc_" || c.OpenCart.Batch != 500 || c.Markup.Default.Type != "percent" {
		t.Errorf("defaults not applied: %+v", c)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]string{
		"bad format":     strings.Replace(base, "format: yml", "format: xls", 1),
		"no path":        strings.Replace(base, "path: f.yml", "path: ''", 1),
		"no categories":  strings.Replace(base, `categories: {"1": {id: "10", name: "A"}}`, "categories: {}", 1),
		"incomplete cat": strings.Replace(base, `name: "A"`, `name: ""`, 1),
		"unknown target": strings.Replace(base, "rozetka:", "amazon:", 1),
		"target no out":  strings.Replace(base, "out: out/r.xml", "out: ''", 1),
		"bad markup":     base + "markup: {default: {type: multiply, value: 2}}\n",
		"bad range":      base + "markup: {default: {type: percent}, rules: [{price_from: 100, price_to: 50, type: fixed, value: 1}]}\n",
	}
	for name, yml := range cases {
		if _, err := load(t, yml); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}
