package services

import (
	"strings"
	"testing"
)

func TestBeoordeelSQLTabel(t *testing.T) {
	tests := []struct {
		naam  string
		sql   string
		soort SQLSoort
	}{
		// Lezen.
		{"select", "SELECT * FROM wp_options", SQLLezen},
		{"select met kleine letters", "select id from wp_posts limit 10", SQLLezen},
		{"show tables", "SHOW TABLES", SQLLezen},
		{"describe", "DESCRIBE wp_options", SQLLezen},
		{"explain", "EXPLAIN SELECT * FROM wp_posts", SQLLezen},
		{"with cte select", "WITH x AS (SELECT 1) SELECT * FROM x", SQLLezen},
		{"voorafgaande witruimte", "\n\n   SELECT 1", SQLLezen},

		// Begrensd schrijven.
		{"insert", "INSERT INTO wp_options (option_name) VALUES ('x')", SQLSchrijven},
		{"update met where", "UPDATE wp_options SET option_value='x' WHERE option_id=1", SQLSchrijven},
		{"delete met where", "DELETE FROM wp_options WHERE option_id=1", SQLSchrijven},
		{"replace", "REPLACE INTO wp_options (option_name) VALUES ('x')", SQLSchrijven},
		{"where in andere schrijfwijze", "update wp_options set a=1 where b=2", SQLSchrijven},
		// Een WITH gevolgd door een UPDATE mét WHERE is gewoon begrensd schrijven.
		{"with cte update met where", "WITH x AS (SELECT 1) UPDATE wp_options SET a=1 WHERE b=2", SQLSchrijven},

		// Bevestigen.
		{"update zonder where", "UPDATE wp_options SET option_value='x'", SQLBevestigen},
		{"delete zonder where", "DELETE FROM wp_options", SQLBevestigen},
		{"drop table", "DROP TABLE wp_options", SQLBevestigen},
		{"drop database", "DROP DATABASE dev_site", SQLBevestigen},
		{"truncate", "TRUNCATE TABLE wp_options", SQLBevestigen},
		{"alter", "ALTER TABLE wp_options ADD COLUMN x INT", SQLBevestigen},
		{"rename", "RENAME TABLE a TO b", SQLBevestigen},
		{"create", "CREATE TABLE x (a INT)", SQLBevestigen},
		{"grant", "GRANT ALL ON *.* TO 'x'@'%'", SQLBevestigen},
		{"set", "SET FOREIGN_KEY_CHECKS=0", SQLBevestigen},
		{"call", "CALL mijn_procedure()", SQLBevestigen},
		{"onbekend commando", "VACUUM ANALYZE", SQLBevestigen},
		{"with cte update zonder where", "WITH x AS (SELECT 1) UPDATE wp_options SET a=1", SQLBevestigen},
	}
	for _, tt := range tests {
		t.Run(tt.naam, func(t *testing.T) {
			got := BeoordeelSQL(tt.sql)
			if got.Soort != tt.soort {
				t.Errorf("BeoordeelSQL(%q).Soort = %q, wil %q (reden: %s)", tt.sql, got.Soort, tt.soort, got.Reden)
			}
			if got.Soort == SQLBevestigen && got.Reden == "" && got.Fout == "" {
				t.Errorf("bevestiging zonder uitleg voor %q", tt.sql)
			}
		})
	}
}

// TestBeoordeelSQLNegeertKeywordsInStrings is de kern van de classificatie: een
// DROP in een tekstwaarde is geen DROP, en omgekeerd mag een WHERE die alleen in
// een string staat een DELETE niet veilig laten lijken.
func TestBeoordeelSQLNegeertKeywordsInStrings(t *testing.T) {
	tests := []struct {
		naam  string
		sql   string
		soort SQLSoort
	}{
		{
			"drop in een stringwaarde",
			`INSERT INTO wp_options (option_name, option_value) VALUES ('tip', 'gebruik DROP TABLE nooit')`,
			SQLSchrijven,
		},
		{
			"where alleen in een string maakt een delete niet veilig",
			`DELETE FROM wp_options WHERE option_name = 'x'`,
			SQLSchrijven,
		},
		{
			"delete met where uitsluitend binnen een literal",
			`DELETE FROM logboek /* opruimen */`,
			SQLBevestigen,
		},
		{
			"nep-where in een string",
			`DELETE FROM wp_options -- WHERE option_id = 1`,
			SQLBevestigen,
		},
		{
			"where achter een hash-commentaar",
			"DELETE FROM wp_options # WHERE option_id=1",
			SQLBevestigen,
		},
		{
			"select met een puntkomma in een string",
			`SELECT * FROM wp_options WHERE option_value = 'a;b'`,
			SQLLezen,
		},
		{
			"backtick-identifier met een puntkomma",
			"SELECT * FROM `raar;naam`",
			SQLLezen,
		},
		{
			"geescapete quote in de waarde",
			`UPDATE wp_options SET option_value = 'O\'Brien' WHERE option_id = 1`,
			SQLSchrijven,
		},
		{
			"verdubbelde quote in de waarde",
			`UPDATE wp_options SET option_value = 'O''Brien' WHERE option_id = 1`,
			SQLSchrijven,
		},
	}
	for _, tt := range tests {
		t.Run(tt.naam, func(t *testing.T) {
			if got := BeoordeelSQL(tt.sql); got.Soort != tt.soort {
				t.Errorf("Soort = %q, wil %q\nsql: %s\nkaal: %s",
					got.Soort, tt.soort, tt.sql, stripCommentaarEnLiteralen(tt.sql))
			}
		})
	}
}

func TestBeoordeelSQLMeerdereStatements(t *testing.T) {
	got := BeoordeelSQL("SELECT 1; DROP TABLE wp_options")
	if got.Fout == "" {
		t.Fatal("meerdere statements gaven geen fout")
	}
	if !strings.Contains(got.Fout, "2 statements") {
		t.Errorf("fout = %q", got.Fout)
	}
	// Zeker niet als 'lezen' doorlaten, want er zit een DROP in.
	if got.Soort == SQLLezen {
		t.Error("meerdere statements werden als lezen beoordeeld")
	}
}

func TestBeoordeelSQLAfsluitendePuntkommaIsEenStatement(t *testing.T) {
	got := BeoordeelSQL("SELECT * FROM wp_options;")
	if got.Fout != "" {
		t.Errorf("fout = %q, een afsluitende puntkomma is normaal", got.Fout)
	}
	if got.Soort != SQLLezen {
		t.Errorf("Soort = %q", got.Soort)
	}
}

func TestBeoordeelSQLLeeg(t *testing.T) {
	for _, sql := range []string{"", "   ", "\n\t", ";", "-- alleen commentaar"} {
		got := BeoordeelSQL(sql)
		if got.Fout == "" {
			t.Errorf("BeoordeelSQL(%q) gaf geen fout", sql)
		}
		if got.Verandert() {
			t.Errorf("BeoordeelSQL(%q) zegt dat er iets verandert", sql)
		}
	}
}

func TestVerandert(t *testing.T) {
	if BeoordeelSQL("SELECT 1").Verandert() {
		t.Error("een SELECT verandert niets")
	}
	if !BeoordeelSQL("UPDATE t SET a=1 WHERE b=2").Verandert() {
		t.Error("een UPDATE verandert wel iets")
	}
	if !BeoordeelSQL("DROP TABLE t").Verandert() {
		t.Error("een DROP verandert wel iets")
	}
}

func TestStripCommentaarEnLiteralen(t *testing.T) {
	tests := []struct {
		naam            string
		in              string
		magNietBevatten []string
		moetBevatten    []string
	}{
		{
			"regelcommentaar",
			"SELECT 1 -- DROP TABLE x\nFROM t",
			[]string{"DROP"},
			[]string{"SELECT", "FROM"},
		},
		{
			"blokcommentaar",
			"SELECT /* DROP TABLE x */ 1",
			[]string{"DROP"},
			[]string{"SELECT"},
		},
		{
			"hash-commentaar",
			"SELECT 1 # DROP TABLE x",
			[]string{"DROP"},
			[]string{"SELECT"},
		},
		{
			"stringliteraal",
			"INSERT INTO t VALUES ('DROP TABLE x')",
			[]string{"DROP"},
			[]string{"INSERT", "VALUES"},
		},
		{
			"dubbele quotes",
			`SELECT "DROP TABLE x" FROM t`,
			[]string{"DROP"},
			[]string{"SELECT", "FROM"},
		},
		{
			"backticks",
			"SELECT * FROM `DROP`",
			[]string{"DROP"},
			[]string{"SELECT", "FROM"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.naam, func(t *testing.T) {
			got := stripCommentaarEnLiteralen(tt.in)
			for _, v := range tt.magNietBevatten {
				if strings.Contains(strings.ToUpper(got), v) {
					t.Errorf("resultaat bevat nog %q: %q", v, got)
				}
			}
			for _, v := range tt.moetBevatten {
				if !strings.Contains(strings.ToUpper(got), v) {
					t.Errorf("resultaat mist %q: %q", v, got)
				}
			}
		})
	}
}

// Woorden mogen na het strippen niet aan elkaar plakken, want dan verandert de
// betekenis van de query.
func TestStripPlaktWoordenNietAanElkaar(t *testing.T) {
	got := stripCommentaarEnLiteralen("DELETE/*x*/FROM t")
	if strings.Contains(strings.ToUpper(got), "DELETEFROM") {
		t.Errorf("woorden geplakt: %q", got)
	}
}

// Een niet-afgesloten string mag de rest van de query niet als code laten gelden.
func TestStripNietAfgeslotenString(t *testing.T) {
	got := BeoordeelSQL("SELECT * FROM t WHERE a = 'onafgesloten")
	// Het maakt niet uit of dit lezen of bevestigen wordt; het mag alleen niet
	// vastlopen of iets als veilig-schrijvend beoordelen.
	if got.Soort == SQLSchrijven {
		t.Errorf("Soort = %q voor een kapotte query", got.Soort)
	}
}
