package mysqldb

import (
	"strings"
	"testing"
)

func TestValideerIdentAccepteertEchteTabelnamen(t *testing.T) {
	// Namen zoals ze in WordPress-installaties voorkomen, inclusief multisite
	// (2_options) en plugintabellen.
	goed := []string{
		"wp_options", "wp_2_options", "wp_postmeta", "wp_woocommerce_order_items",
		"WP_Users", "table1", "_prefix", "a$b", "gravity-forms", "x",
		strings.Repeat("a", 64),
	}
	for _, naam := range goed {
		if err := ValideerIdent(naam); err != nil {
			t.Errorf("ValideerIdent(%q) = %v, wil geen fout", naam, err)
		}
	}
}

// TestValideerIdentWeigertInjectie is de guard: alles wat een query zou kunnen
// openbreken moet stuklopen op de validatie, niet op de quoting.
func TestValideerIdentWeigertInjectie(t *testing.T) {
	slecht := []string{
		"",
		"wp_options`; DROP TABLE wp_users; --",
		"wp_options`",
		"`wp_options`",
		"wp_options WHERE 1=1",
		"wp options",
		"wp_options;",
		"wp_options'",
		`wp_options"`,
		"wp_options\\",
		"wp_options\n",
		"wp_options\t",
		"wp_options/*x*/",
		"wp_options#",
		"information_schema.tables",
		"wp_options)",
		"wp_options(",
		strings.Repeat("a", 65),
		"tabel\x00naam",
	}
	for _, naam := range slecht {
		if err := ValideerIdent(naam); err == nil {
			t.Errorf("ValideerIdent(%q) werd geaccepteerd", naam)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	got, err := QuoteIdent("wp_options")
	if err != nil {
		t.Fatalf("QuoteIdent: %v", err)
	}
	if got != "`wp_options`" {
		t.Errorf("QuoteIdent = %q", got)
	}
}

func TestQuoteIdentWeigertOngeldig(t *testing.T) {
	if _, err := QuoteIdent("a`b"); err == nil {
		t.Error("een backtick werd doorgelaten")
	}
}

// Elke naam die de validatie haalt, mag na quoting geen los aanhalingsteken,
// puntkomma of commentaar bevatten — dat is de eigenschap waar de rest op leunt.
func TestQuoteIdentLevertAltijdEenAfgeslotenIdentifier(t *testing.T) {
	for _, naam := range []string{"wp_options", "a$b", "x-y", "_1"} {
		q, err := QuoteIdent(naam)
		if err != nil {
			t.Fatalf("QuoteIdent(%q): %v", naam, err)
		}
		if !strings.HasPrefix(q, "`") || !strings.HasSuffix(q, "`") {
			t.Errorf("QuoteIdent(%q) = %q, niet afgesloten", naam, q)
		}
		kern := q[1 : len(q)-1]
		for _, verboden := range []string{"`", ";", "'", `"`, "--", "/*", "\\"} {
			if strings.Contains(kern, verboden) {
				t.Errorf("QuoteIdent(%q) = %q bevat %q", naam, q, verboden)
			}
		}
	}
}

func TestQuoteIdentPad(t *testing.T) {
	got, err := QuoteIdentPad("dev_site", "wp_options")
	if err != nil {
		t.Fatalf("QuoteIdentPad: %v", err)
	}
	if got != "`dev_site`.`wp_options`" {
		t.Errorf("QuoteIdentPad = %q", got)
	}
	if _, err := QuoteIdentPad("dev_site", "wp; DROP"); err == nil {
		t.Error("ongeldig deel werd geaccepteerd")
	}
}
