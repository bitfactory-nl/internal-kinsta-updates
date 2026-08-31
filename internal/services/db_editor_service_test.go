package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/mysqldb"
	"github.com/rdm/sites-tool/internal/domain"
)

type fakeDumper struct {
	mu        sync.Mutex
	aanroepen []string
	fout      error
}

func (f *fakeDumper) DumpLocal(projectID, dbName string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aanroepen = append(f.aanroepen, projectID+"|"+dbName)
	if f.fout != nil {
		return "", "", f.fout
	}
	return "/dumps/" + dbName + ".sql.gz", "", nil
}

func (f *fakeDumper) aantal() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.aanroepen)
}

// editorProject maakt een project met een .env die naar de lokale database wijst.
func editorProject(t *testing.T, envRegels string) (*ProjectService, string) {
	t.Helper()
	dir := t.TempDir()
	if envRegels != "" {
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envRegels), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	projects := NewProjectService(nil)
	projects.projects = []domain.Project{{ID: "p1", DisplayName: "Test", Path: dir}}
	return projects, "p1"
}

func TestDBEditorInfoBeschikbaar(t *testing.T) {
	projects, id := editorProject(t, "DB_NAME=dev_site\nDB_USER=root\nDB_PASSWORD=secret\nDB_HOST=mysql\n")
	svc := NewDBEditorService(projects, &fakeDumper{}, nil)

	info, err := svc.Info(id)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if !info.Beschikbaar {
		t.Fatalf("niet beschikbaar: %s", info.Reden)
	}
	if info.Database != "dev_site" || info.Gebruiker != "root" {
		t.Errorf("info = %+v", info)
	}
	// De editor praat altijd met de lokale poort, nooit met een remote host.
	if info.Host != "127.0.0.1" || info.Poort != 3306 || info.Container != "bitf-mysql" {
		t.Errorf("info = %+v", info)
	}
}

func TestDBEditorInfoMysql84(t *testing.T) {
	projects, id := editorProject(t, "DB_NAME=dev_site\nDB_HOST=mysql84\n")
	info, err := NewDBEditorService(projects, &fakeDumper{}, nil).Info(id)
	if err != nil {
		t.Fatal(err)
	}
	if info.Poort != 3307 || info.Container != "bitf-mysql84" {
		t.Errorf("info = %+v", info)
	}
}

// Zonder lokale database hoort het menu-item niet te verschijnen, met een reden
// die uitlegt waarom.
func TestDBEditorInfoNietBeschikbaar(t *testing.T) {
	tests := []struct {
		naam string
		env  string
	}{
		{"geen .env", ""},
		{"geen DB_NAME", "DB_USER=root\nDB_HOST=mysql\n"},
		{"lege DB_NAME", "DB_NAME=\nDB_HOST=mysql\n"},
		{"onbekende DB_HOST", "DB_NAME=dev_site\nDB_HOST=postgres\n"},
	}
	for _, tt := range tests {
		t.Run(tt.naam, func(t *testing.T) {
			projects, id := editorProject(t, tt.env)
			info, err := NewDBEditorService(projects, &fakeDumper{}, nil).Info(id)
			if err != nil {
				t.Fatalf("Info gaf een fout in plaats van 'niet beschikbaar': %v", err)
			}
			if info.Beschikbaar {
				t.Errorf("onterecht beschikbaar: %+v", info)
			}
			if info.Reden == "" {
				t.Error("geen reden opgegeven")
			}
		})
	}
}

func TestDBEditorOnbekendProject(t *testing.T) {
	svc := NewDBEditorService(NewProjectService(nil), &fakeDumper{}, nil)
	if _, err := svc.Info("bestaatniet"); err == nil {
		t.Error("onbekend project gaf geen fout")
	}
}

// TestDBEditorQueryPoortIsServerSide is de belangrijkste test van de
// bevestigingspoort: een destructieve query mag zonder toestemming niet worden
// uitgevoerd, ook niet als de aanroep de frontend overslaat.
func TestDBEditorQueryPoortIsServerSide(t *testing.T) {
	projects, id := editorProject(t, "DB_NAME=dev_site\nDB_HOST=mysql\n")
	dumper := &fakeDumper{}
	svc := NewDBEditorService(projects, dumper, nil)

	for _, sql := range []string{
		"DROP TABLE wp_options",
		"TRUNCATE TABLE wp_options",
		"DELETE FROM wp_options",
		"UPDATE wp_options SET option_value=''",
		"ALTER TABLE wp_options ADD COLUMN x INT",
	} {
		uit, err := svc.VoerQueryUit(id, "dev_site", sql, false)
		if err != nil {
			t.Fatalf("VoerQueryUit(%q) gaf een fout in plaats van een bevestigingsverzoek: %v", sql, err)
		}
		if !uit.BevestigingNodig {
			t.Errorf("query %q werd zonder bevestiging uitgevoerd", sql)
		}
		if uit.Beoordeling.Reden == "" {
			t.Errorf("query %q kreeg geen uitleg", sql)
		}
	}
	// En er is niets gedumpt, want er is niets uitgevoerd.
	if dumper.aantal() != 0 {
		t.Errorf("er is %d keer gedumpt zonder uitvoering", dumper.aantal())
	}
}

func TestDBEditorQueryWeigertMeerdereStatements(t *testing.T) {
	projects, id := editorProject(t, "DB_NAME=dev_site\nDB_HOST=mysql\n")
	svc := NewDBEditorService(projects, &fakeDumper{}, nil)

	if _, err := svc.VoerQueryUit(id, "dev_site", "SELECT 1; DROP TABLE x", true); err == nil {
		t.Error("meerdere statements werden geaccepteerd")
	}
}

func TestDBEditorBeoordeelQuery(t *testing.T) {
	svc := NewDBEditorService(NewProjectService(nil), &fakeDumper{}, nil)
	if got := svc.BeoordeelQuery("SELECT 1"); got.Soort != SQLLezen {
		t.Errorf("Soort = %q", got.Soort)
	}
	if got := svc.BeoordeelQuery("DROP TABLE x"); got.Soort != SQLBevestigen {
		t.Errorf("Soort = %q", got.Soort)
	}
}

// --- Integratietests: deze praten met de echte lokale MySQL. ---

// editorMetEchteDB zet een project op met een verse testdatabase op bitf-mysql.
func editorMetEchteDB(t *testing.T) (*DBEditorService, string, string, *fakeDumper) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	beheer, err := mysqldb.Open(ctx, mysqldb.Config{Host: "127.0.0.1", Port: 3306, User: "root", Password: "secret"})
	if err != nil {
		t.Skipf("geen lokale MySQL: %v", err)
	}
	defer beheer.Close()

	dbNaam := fmt.Sprintf("rdm_ed_%d", time.Now().UnixNano()%1_000_000)
	if _, _, err := beheer.Exec(ctx, "CREATE DATABASE `"+dbNaam+"` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("testdatabase maken: %v", err)
	}
	t.Cleanup(func() {
		opruim, annuleer := context.WithTimeout(context.Background(), 10*time.Second)
		defer annuleer()
		c, err := mysqldb.Open(opruim, mysqldb.Config{Host: "127.0.0.1", Port: 3306, User: "root", Password: "secret"})
		if err == nil {
			c.Exec(opruim, "DROP DATABASE IF EXISTS `"+dbNaam+"`")
			c.Close()
		}
	})
	if _, _, err := beheer.Exec(ctx, "CREATE TABLE `"+dbNaam+"`.`wp_options` ("+
		"option_id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,"+
		"option_name VARCHAR(191) NOT NULL,"+
		"option_value LONGTEXT,"+
		"PRIMARY KEY (option_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"); err != nil {
		t.Fatal(err)
	}

	projects, id := editorProject(t, "DB_NAME="+dbNaam+"\nDB_USER=root\nDB_PASSWORD=secret\nDB_HOST=mysql\n")
	dumper := &fakeDumper{}
	svc := NewDBEditorService(projects, dumper, nil)
	t.Cleanup(svc.Sluit)
	return svc, id, dbNaam, dumper
}

func TestIntegratieEditorTabelWeergave(t *testing.T) {
	svc, id, dbNaam, _ := editorMetEchteDB(t)

	if _, err := svc.VoerQueryUit(id, dbNaam,
		"INSERT INTO wp_options (option_name, option_value) VALUES ('siteurl','https://x.test')", false); err != nil {
		t.Fatalf("insert: %v", err)
	}

	weergave, err := svc.Tabel(id, RijenVerzoek{Database: dbNaam, Tabel: "wp_options"})
	if err != nil {
		t.Fatalf("Tabel: %v", err)
	}
	if !weergave.Bewerkbaar {
		t.Errorf("tabel met primary key hoort bewerkbaar te zijn: %s", weergave.Reden)
	}
	if weergave.Totaal != 1 {
		t.Errorf("totaal = %d, wil 1", weergave.Totaal)
	}
	if len(weergave.Kolommen) != 3 {
		t.Errorf("kolommen = %d", len(weergave.Kolommen))
	}
	if len(weergave.Rijen.Rijen) != 1 {
		t.Fatalf("rijen = %d", len(weergave.Rijen.Rijen))
	}
}

func TestIntegratieEditorTabelZonderPKIsAlleenLezen(t *testing.T) {
	svc, id, dbNaam, _ := editorMetEchteDB(t)
	if _, err := svc.VoerQueryUit(id, dbNaam, "CREATE TABLE geen_pk (a INT, b VARCHAR(10))", true); err != nil {
		t.Fatalf("create: %v", err)
	}

	weergave, err := svc.Tabel(id, RijenVerzoek{Database: dbNaam, Tabel: "geen_pk"})
	if err != nil {
		t.Fatalf("Tabel: %v", err)
	}
	if weergave.Bewerkbaar {
		t.Error("tabel zonder primary key werd bewerkbaar gemeld")
	}
	if !strings.Contains(weergave.Reden, "primary key") {
		t.Errorf("reden = %q", weergave.Reden)
	}
}

// Het vangnet moet één keer per database aangaan, niet bij elke wijziging.
func TestIntegratieEditorVangnetEenmaligPerSessie(t *testing.T) {
	svc, id, dbNaam, dumper := editorMetEchteDB(t)

	uit, err := svc.VoegRijToe(id, RijVerzoek{
		Database: dbNaam, Tabel: "wp_options",
		Waarden: []mysqldb.NieuweWaarde{{Kolom: "option_name", Waarde: "een"}},
	})
	if err != nil {
		t.Fatalf("VoegRijToe: %v", err)
	}
	if uit.DumpPad == "" {
		t.Error("de eerste schrijfactie leverde geen dump op")
	}
	if dumper.aantal() != 1 {
		t.Fatalf("aantal dumps = %d, wil 1", dumper.aantal())
	}

	sleutel := []mysqldb.SleutelWaarde{{Kolom: "option_id", Waarde: fmt.Sprint(uit.NieuweID)}}
	if _, err := svc.ZetCel(id, CelVerzoek{
		Database: dbNaam, Tabel: "wp_options", Sleutel: sleutel,
		Kolom: "option_value", Waarde: "tweede wijziging",
	}); err != nil {
		t.Fatalf("ZetCel: %v", err)
	}
	if dumper.aantal() != 1 {
		t.Errorf("aantal dumps = %d na een tweede wijziging, wil nog steeds 1", dumper.aantal())
	}
}

// Een lees-query mag geen dump veroorzaken.
func TestIntegratieEditorLezenDumptNiet(t *testing.T) {
	svc, id, dbNaam, dumper := editorMetEchteDB(t)

	if _, err := svc.VoerQueryUit(id, dbNaam, "SELECT * FROM wp_options", false); err != nil {
		t.Fatalf("select: %v", err)
	}
	if dumper.aantal() != 0 {
		t.Errorf("een SELECT leverde %d dumps op", dumper.aantal())
	}
}

// TestIntegratieEditorMislukteDumpBlokkeertSchrijven is de andere helft van het
// vangnet: als de dump faalt, mag er niets gewijzigd worden.
func TestIntegratieEditorMislukteDumpBlokkeertSchrijven(t *testing.T) {
	svc, id, dbNaam, dumper := editorMetEchteDB(t)
	dumper.fout = fmt.Errorf("schijf vol")

	_, err := svc.VoegRijToe(id, RijVerzoek{
		Database: dbNaam, Tabel: "wp_options",
		Waarden: []mysqldb.NieuweWaarde{{Kolom: "option_name", Waarde: "mag_niet"}},
	})
	if err == nil {
		t.Fatal("schrijven werd toegestaan terwijl de dump faalde")
	}
	if !strings.Contains(err.Error(), "niets gewijzigd") {
		t.Errorf("fout = %v", err)
	}

	weergave, err := svc.Tabel(id, RijenVerzoek{Database: dbNaam, Tabel: "wp_options"})
	if err != nil {
		t.Fatal(err)
	}
	if weergave.Totaal != 0 {
		t.Errorf("er is toch een rij toegevoegd: totaal = %d", weergave.Totaal)
	}
}

func TestIntegratieEditorCelBewerkenEnVerwijderen(t *testing.T) {
	svc, id, dbNaam, _ := editorMetEchteDB(t)

	toe, err := svc.VoegRijToe(id, RijVerzoek{
		Database: dbNaam, Tabel: "wp_options",
		Waarden: []mysqldb.NieuweWaarde{
			{Kolom: "option_name", Waarde: "test"},
			{Kolom: "option_value", Waarde: "oud"},
		},
	})
	if err != nil {
		t.Fatalf("VoegRijToe: %v", err)
	}
	sleutel := []mysqldb.SleutelWaarde{{Kolom: "option_id", Waarde: fmt.Sprint(toe.NieuweID)}}

	// Een waarde met tekens die handmatige escaping zouden breken.
	gemeen := `O'Brien \ "x" 🍺`
	if _, err := svc.ZetCel(id, CelVerzoek{
		Database: dbNaam, Tabel: "wp_options", Sleutel: sleutel,
		Kolom: "option_value", Waarde: gemeen,
	}); err != nil {
		t.Fatalf("ZetCel: %v", err)
	}
	weergave, err := svc.Tabel(id, RijenVerzoek{Database: dbNaam, Tabel: "wp_options"})
	if err != nil {
		t.Fatal(err)
	}
	idx := -1
	for i, k := range weergave.Rijen.Kolommen {
		if k == "option_value" {
			idx = i
		}
	}
	if got := weergave.Rijen.Rijen[0][idx].Waarde; got != gemeen {
		t.Errorf("waarde = %q, wil %q", got, gemeen)
	}

	// Naar NULL.
	if _, err := svc.ZetCel(id, CelVerzoek{
		Database: dbNaam, Tabel: "wp_options", Sleutel: sleutel,
		Kolom: "option_value", NaarNull: true,
	}); err != nil {
		t.Fatalf("ZetCel naar NULL: %v", err)
	}
	weergave, _ = svc.Tabel(id, RijenVerzoek{Database: dbNaam, Tabel: "wp_options"})
	if !weergave.Rijen.Rijen[0][idx].Null {
		t.Error("cel is niet NULL geworden")
	}

	// Verwijderen.
	if _, err := svc.VerwijderRij(id, RijVerzoek{Database: dbNaam, Tabel: "wp_options", Sleutel: sleutel}); err != nil {
		t.Fatalf("VerwijderRij: %v", err)
	}
	weergave, _ = svc.Tabel(id, RijenVerzoek{Database: dbNaam, Tabel: "wp_options"})
	if weergave.Totaal != 0 {
		t.Errorf("totaal = %d, wil 0", weergave.Totaal)
	}
}

func TestIntegratieEditorQueryMetBevestigingVoertUit(t *testing.T) {
	svc, id, dbNaam, dumper := editorMetEchteDB(t)

	if _, err := svc.VoerQueryUit(id, dbNaam,
		"INSERT INTO wp_options (option_name) VALUES ('a'),('b'),('c')", false); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Zonder bevestiging: niets gebeurt.
	uit, err := svc.VoerQueryUit(id, dbNaam, "DELETE FROM wp_options", false)
	if err != nil {
		t.Fatal(err)
	}
	if !uit.BevestigingNodig {
		t.Fatal("geen bevestiging gevraagd")
	}
	weergave, _ := svc.Tabel(id, RijenVerzoek{Database: dbNaam, Tabel: "wp_options"})
	if weergave.Totaal != 3 {
		t.Fatalf("er is toch verwijderd: totaal = %d", weergave.Totaal)
	}

	// Met bevestiging: wel.
	uit, err = svc.VoerQueryUit(id, dbNaam, "DELETE FROM wp_options", true)
	if err != nil {
		t.Fatalf("VoerQueryUit met bevestiging: %v", err)
	}
	if uit.BevestigingNodig {
		t.Error("nog steeds bevestiging gevraagd")
	}
	if uit.Geraakt != 3 {
		t.Errorf("geraakt = %d, wil 3", uit.Geraakt)
	}
	if dumper.aantal() == 0 {
		t.Error("er is geen veiligheidsdump gemaakt vóór het verwijderen")
	}
	weergave, _ = svc.Tabel(id, RijenVerzoek{Database: dbNaam, Tabel: "wp_options"})
	if weergave.Totaal != 0 {
		t.Errorf("totaal = %d, wil 0", weergave.Totaal)
	}
}

func TestIntegratieEditorSelectGeeftResultaat(t *testing.T) {
	svc, id, dbNaam, _ := editorMetEchteDB(t)

	uit, err := svc.VoerQueryUit(id, dbNaam, "SELECT 1 AS een, NULL AS niks", false)
	if err != nil {
		t.Fatalf("VoerQueryUit: %v", err)
	}
	if uit.Resultaat == nil {
		t.Fatal("geen resultaat")
	}
	if len(uit.Resultaat.Kolommen) != 2 || uit.Resultaat.Rijen[0][0].Waarde != "1" {
		t.Errorf("resultaat = %+v", uit.Resultaat)
	}
	if !uit.Resultaat.Rijen[0][1].Null {
		t.Error("NULL werd niet als NULL gemeld")
	}
}

func TestIntegratieEditorDatabasesEnTabellen(t *testing.T) {
	svc, id, dbNaam, _ := editorMetEchteDB(t)

	namen, err := svc.Databases(id)
	if err != nil {
		t.Fatalf("Databases: %v", err)
	}
	var gevonden bool
	for _, n := range namen {
		if n == dbNaam {
			gevonden = true
		}
	}
	if !gevonden {
		t.Errorf("de testdatabase staat niet in de lijst: %v", namen)
	}

	tabellen, err := svc.Tabellen(id, dbNaam)
	if err != nil {
		t.Fatalf("Tabellen: %v", err)
	}
	if len(tabellen) != 1 || tabellen[0].Naam != "wp_options" {
		t.Errorf("tabellen = %+v", tabellen)
	}
}

func TestIntegratieEditorOnbekendeTabel(t *testing.T) {
	svc, id, dbNaam, _ := editorMetEchteDB(t)
	if _, err := svc.Tabel(id, RijenVerzoek{Database: dbNaam, Tabel: "bestaat_niet"}); err == nil {
		t.Error("onbekende tabel gaf geen fout")
	}
}
