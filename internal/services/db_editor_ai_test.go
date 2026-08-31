package services

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

type fakeBouwer struct {
	antwoord  domain.AISQLAntwoord
	fout      error
	vraag     string
	schema    string
	aanroepen int
}

func (f *fakeBouwer) BouwQuery(_ context.Context, vraag, schema string) (domain.AISQLAntwoord, error) {
	f.aanroepen++
	f.vraag, f.schema = vraag, schema
	return f.antwoord, f.fout
}

func TestTabellenInQuery(t *testing.T) {
	bekend := []string{"wp_options", "wp_posts", "wp_postmeta", "wp_users"}
	tests := []struct {
		naam string
		sql  string
		want []string
	}{
		{
			"enkele tabel",
			"SELECT * FROM wp_options WHERE option_name = 'home'",
			[]string{"wp_options"},
		},
		{
			"join",
			"SELECT p.ID FROM wp_posts p JOIN wp_postmeta m ON m.post_id = p.ID",
			[]string{"wp_posts", "wp_postmeta"},
		},
		{
			"wp_posts matcht niet ook wp_postmeta",
			"SELECT * FROM wp_posts",
			[]string{"wp_posts"},
		},
		{
			"tabelnaam alleen in een stringwaarde telt niet",
			"SELECT * FROM wp_options WHERE option_value = 'wp_users'",
			[]string{"wp_options"},
		},
		{
			"tabelnaam in commentaar telt niet",
			"SELECT 1 -- wp_users",
			nil,
		},
		{
			"onbekende tabel levert niets",
			"SELECT * FROM andere_database_tabel",
			nil,
		},
		{
			"hoofdletters",
			"SELECT * FROM WP_OPTIONS",
			[]string{"wp_options"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.naam, func(t *testing.T) {
			got := tabellenInQuery(tt.sql, bekend)
			if len(got) != len(tt.want) {
				t.Fatalf("tabellenInQuery = %v, wil %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, wil %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBouwQueryZonderVraag(t *testing.T) {
	projects, id := editorProject(t, "DB_NAME=dev_site\nDB_HOST=mysql\n")
	svc := NewDBEditorService(projects, &fakeDumper{}, nil)
	svc.ai = &fakeBouwer{}

	if _, err := svc.BouwQuery(id, "dev_site", "   "); err == nil {
		t.Error("een lege vraag werd geaccepteerd")
	}
}

// Zonder API-key hoort er een melding te komen die zegt waar je die invult, niet
// een technische fout.
func TestBouwQueryZonderAPIKey(t *testing.T) {
	projects, id := editorProject(t, "DB_NAME=dev_site\nDB_HOST=mysql\n")
	svc := NewDBEditorService(projects, &fakeDumper{}, nil)

	_, err := svc.BouwQuery(id, "dev_site", "hoeveel gebruikers?")
	if err == nil {
		t.Fatal("verwachtte een fout zonder configuratie")
	}
	if !strings.Contains(err.Error(), "configuratie") && !strings.Contains(err.Error(), "API-key") {
		t.Errorf("fout = %v", err)
	}
}

// --- Integratietests tegen de echte lokale MySQL. ---

func TestIntegratieBouwQueryLeesVraag(t *testing.T) {
	svc, id, dbNaam, _ := editorMetEchteDB(t)
	bouwer := &fakeBouwer{antwoord: domain.AISQLAntwoord{
		SQL:      "SELECT option_name, option_value FROM wp_options LIMIT 50",
		Uitleg:   "Haalt de eerste 50 opties op.",
		Aannames: []string{"aangenomen dat het om het prefix wp_ gaat"},
	}}
	svc.ai = bouwer

	voorstel, err := svc.BouwQuery(id, dbNaam, "laat me de opties zien")
	if err != nil {
		t.Fatalf("BouwQuery: %v", err)
	}
	if voorstel.SQL == "" {
		t.Fatal("geen sql in het voorstel")
	}
	if voorstel.Beoordeling.Soort != SQLLezen {
		t.Errorf("soort = %q, wil lezen", voorstel.Beoordeling.Soort)
	}
	if len(voorstel.Tabellen) != 1 || voorstel.Tabellen[0] != "wp_options" {
		t.Errorf("tabellen = %v", voorstel.Tabellen)
	}
	if len(voorstel.Aannames) != 1 {
		t.Errorf("aannames = %v", voorstel.Aannames)
	}

	// Het schema is meegegeven en bevat de kolomnamen, maar geen rijdata.
	if !strings.Contains(bouwer.schema, "wp_options(") {
		t.Errorf("schema mist de tabel: %s", bouwer.schema)
	}
	if !strings.Contains(bouwer.schema, "option_value") {
		t.Errorf("schema mist een kolom: %s", bouwer.schema)
	}
	if bouwer.vraag != "laat me de opties zien" {
		t.Errorf("vraag = %q", bouwer.vraag)
	}
}

// TestIntegratieBouwQueryVoertNietUit is de kern van de opdracht: het voorstel
// mag niets aan de database veranderen. Uitvoeren is een aparte klik.
func TestIntegratieBouwQueryVoertNietUit(t *testing.T) {
	svc, id, dbNaam, dumper := editorMetEchteDB(t)

	// Eerst een rij, zodat er iets te verliezen is.
	if _, err := svc.VoerQueryUit(id, dbNaam,
		"INSERT INTO wp_options (option_name) VALUES ('blijft_staan')", false); err != nil {
		t.Fatal(err)
	}
	voorDump := dumper.aantal()

	svc.ai = &fakeBouwer{antwoord: domain.AISQLAntwoord{
		SQL:          "DELETE FROM wp_options",
		Uitleg:       "Verwijdert alle opties.",
		Waarschuwing: "dit verwijdert alles",
	}}

	voorstel, err := svc.BouwQuery(id, dbNaam, "gooi alle opties weg")
	if err != nil {
		t.Fatalf("BouwQuery: %v", err)
	}
	if voorstel.Beoordeling.Soort != SQLBevestigen {
		t.Errorf("soort = %q, wil bevestigen", voorstel.Beoordeling.Soort)
	}

	// De rij staat er nog en er is niet extra gedumpt: bouwen voert niets uit.
	weergave, err := svc.Tabel(id, RijenVerzoek{Database: dbNaam, Tabel: "wp_options"})
	if err != nil {
		t.Fatal(err)
	}
	if weergave.Totaal != 1 {
		t.Errorf("totaal = %d; het voorstel heeft rijen aangeraakt", weergave.Totaal)
	}
	if dumper.aantal() != voorDump {
		t.Errorf("er is gedumpt tijdens het bouwen (%d -> %d)", voorDump, dumper.aantal())
	}

	// En de waarschuwing dat dit geen lees-query is, staat erbij.
	var gemeld bool
	for _, w := range voorstel.Waarschuwingen {
		if strings.Contains(w, "geen lees-query") {
			gemeld = true
		}
	}
	if !gemeld {
		t.Errorf("waarschuwingen = %v", voorstel.Waarschuwingen)
	}
}

// Een AI-query die om bevestiging vraagt, moet daar bij uitvoeren ook echt om
// vragen — de poort geldt net zo goed voor AI-SQL als voor getypte SQL.
func TestIntegratieAIQueryGaatDoorDezelfdePoort(t *testing.T) {
	svc, id, dbNaam, _ := editorMetEchteDB(t)
	if _, err := svc.VoerQueryUit(id, dbNaam,
		"INSERT INTO wp_options (option_name) VALUES ('a'),('b')", false); err != nil {
		t.Fatal(err)
	}

	svc.ai = &fakeBouwer{antwoord: domain.AISQLAntwoord{
		SQL: "DELETE FROM wp_options", Uitleg: "alles weg",
	}}
	voorstel, err := svc.BouwQuery(id, dbNaam, "leeg de tabel")
	if err != nil {
		t.Fatal(err)
	}

	uit, err := svc.VoerQueryUit(id, dbNaam, voorstel.SQL, false)
	if err != nil {
		t.Fatal(err)
	}
	if !uit.BevestigingNodig {
		t.Fatal("de AI-query werd zonder bevestiging uitgevoerd")
	}
	weergave, _ := svc.Tabel(id, RijenVerzoek{Database: dbNaam, Tabel: "wp_options"})
	if weergave.Totaal != 2 {
		t.Errorf("totaal = %d, wil 2", weergave.Totaal)
	}
}

func TestIntegratieBouwQueryLegeSQLGeeftUitleg(t *testing.T) {
	svc, id, dbNaam, _ := editorMetEchteDB(t)
	svc.ai = &fakeBouwer{antwoord: domain.AISQLAntwoord{
		SQL:    "",
		Uitleg: "Er is geen tabel met bestelgegevens in dit schema.",
	}}

	voorstel, err := svc.BouwQuery(id, dbNaam, "hoeveel bestellingen zijn er?")
	if err != nil {
		t.Fatalf("BouwQuery: %v", err)
	}
	if voorstel.SQL != "" {
		t.Errorf("sql = %q", voorstel.SQL)
	}
	if voorstel.Uitleg == "" {
		t.Error("uitleg ontbreekt")
	}
	if len(voorstel.Waarschuwingen) == 0 {
		t.Error("geen waarschuwing dat er geen query is")
	}
}

// Een query over een tabel die niet in deze database bestaat, hoort een
// waarschuwing te geven in plaats van pas bij uitvoeren te falen.
func TestIntegratieBouwQueryWaarschuwtBijOnbekendeTabellen(t *testing.T) {
	svc, id, dbNaam, _ := editorMetEchteDB(t)
	svc.ai = &fakeBouwer{antwoord: domain.AISQLAntwoord{
		SQL:    "SELECT * FROM wp_woocommerce_order_items LIMIT 10",
		Uitleg: "Haalt orderregels op.",
	}}

	voorstel, err := svc.BouwQuery(id, dbNaam, "welke orderregels zijn er?")
	if err != nil {
		t.Fatalf("BouwQuery: %v", err)
	}
	if len(voorstel.Tabellen) != 0 {
		t.Errorf("tabellen = %v, wil leeg", voorstel.Tabellen)
	}
	var gemeld bool
	for _, w := range voorstel.Waarschuwingen {
		if strings.Contains(w, "geen enkele tabel") {
			gemeld = true
		}
	}
	if !gemeld {
		t.Errorf("waarschuwingen = %v", voorstel.Waarschuwingen)
	}
}

func TestIntegratieBouwQueryGeeftAIFoutDoor(t *testing.T) {
	svc, id, dbNaam, _ := editorMetEchteDB(t)
	svc.ai = &fakeBouwer{fout: fmt.Errorf("401 unauthorized")}

	if _, err := svc.BouwQuery(id, dbNaam, "iets"); err == nil {
		t.Error("een AI-fout werd ingeslikt")
	}
}

func TestIntegratieSchemaTekstBevatGeenRijdata(t *testing.T) {
	svc, id, dbNaam, _ := editorMetEchteDB(t)

	// Een rij met een herkenbare waarde erin.
	if _, err := svc.VoerQueryUit(id, dbNaam,
		"INSERT INTO wp_options (option_name, option_value) VALUES ('geheim','waarde-die-niet-mag-lekken')", false); err != nil {
		t.Fatal(err)
	}

	bouwer := &fakeBouwer{antwoord: domain.AISQLAntwoord{SQL: "SELECT 1", Uitleg: "x"}}
	svc.ai = bouwer
	if _, err := svc.BouwQuery(id, dbNaam, "vraag"); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(bouwer.schema, "waarde-die-niet-mag-lekken") {
		t.Errorf("het schema bevat rijdata:\n%s", bouwer.schema)
	}
	if strings.Contains(bouwer.schema, "geheim") {
		t.Errorf("het schema bevat een rijwaarde:\n%s", bouwer.schema)
	}
	// Maar de kolomnamen horen er wel in te staan.
	if !strings.Contains(bouwer.schema, "option_name") {
		t.Errorf("het schema mist kolomnamen:\n%s", bouwer.schema)
	}
}
