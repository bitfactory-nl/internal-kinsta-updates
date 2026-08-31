package mysqldb

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Deze tests praten met de echte lokale MySQL. Ze slaan zichzelf over als die er
// niet is, zodat de suite op een andere machine gewoon groen blijft.
//
// Ze bestaan omdat een fake hier niets bewijst: de hele reden dat deze adapter
// een echte driver gebruikt, is dat waarden met quotes, backslashes en emoji
// veilig door MySQL zelf worden verwerkt. Dat kun je alleen tegen MySQL testen.

func testClient(t *testing.T) (*Client, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	c, err := Open(ctx, Config{Host: "127.0.0.1", Port: 3306, User: "root", Password: "secret"})
	if err != nil {
		t.Skipf("geen lokale MySQL op 127.0.0.1:3306: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	dbNaam := fmt.Sprintf("rdm_test_%d", time.Now().UnixNano()%1_000_000)
	if _, err := c.db.ExecContext(ctx, "CREATE DATABASE `"+dbNaam+"` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("testdatabase maken: %v", err)
	}
	t.Cleanup(func() {
		opruim, annuleer := context.WithTimeout(context.Background(), 10*time.Second)
		defer annuleer()
		c.db.ExecContext(opruim, "DROP DATABASE IF EXISTS `"+dbNaam+"`")
	})
	return c, dbNaam
}

func maakTestTabel(t *testing.T, c *Client, dbNaam string) {
	t.Helper()
	ctx := context.Background()
	_, err := c.db.ExecContext(ctx, "CREATE TABLE `"+dbNaam+"`.`wp_options` ("+
		"option_id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,"+
		"option_name VARCHAR(191) NOT NULL,"+
		"option_value LONGTEXT,"+
		"blob_kolom BLOB,"+
		"PRIMARY KEY (option_id),"+
		"UNIQUE KEY option_name (option_name)"+
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")
	if err != nil {
		t.Fatalf("tabel maken: %v", err)
	}
}

// gemeneWaarden zijn precies de vormen waarop met de hand geschreven escaping
// stukloopt.
var gemeneWaarden = []struct {
	naam   string
	waarde string
}{
	{"enkele quote", `O'Brien's "site"`},
	{"backslash op het eind", `C:\pad\naar\`},
	{"dubbele backslash", `a\\'; DROP TABLE wp_options; --`},
	{"sql-injectiepoging", `'; DROP TABLE wp_options; -- `},
	{"emoji en utf8mb4", "café 🍺 日本語 —"},
	{"nieuwe regels", "regel1\nregel2\r\nregel3"},
	{"tab", "kolom1\tkolom2"},
	{"procent en underscore", "100% _wildcard_"},
	{"lange tekst", strings.Repeat("x", 5000)},
	{"leeg", ""},
}

func TestIntegratieRondritMetGemeneWaarden(t *testing.T) {
	c, dbNaam := testClient(t)
	maakTestTabel(t, c, dbNaam)
	ctx := context.Background()

	for _, gw := range gemeneWaarden {
		t.Run(gw.naam, func(t *testing.T) {
			id, err := c.VoegRijToe(ctx, dbNaam, "wp_options", []NieuweWaarde{
				{Kolom: "option_name", Waarde: "test_" + gw.naam},
				{Kolom: "option_value", Waarde: gw.waarde},
			})
			if err != nil {
				t.Fatalf("VoegRijToe: %v", err)
			}
			if id == 0 {
				t.Fatal("geen auto-increment-id teruggekregen")
			}

			// Terugleggen en vergelijken: de waarde moet er byte-voor-byte
			// hetzelfde uitkomen.
			res, err := c.Rijen(ctx, RijenOpties{
				Database: dbNaam, Tabel: "wp_options",
				ZoekKolom: "option_name", Zoek: "test_" + gw.naam,
			})
			if err != nil {
				t.Fatalf("Rijen: %v", err)
			}
			if len(res.Rijen) != 1 {
				t.Fatalf("aantal rijen = %d, wil 1", len(res.Rijen))
			}
			kolomIdx := indexVanKolom(res.Kolommen, "option_value")
			cel := res.Rijen[0][kolomIdx]

			verwacht := gw.waarde
			if len(verwacht) > maxCelTekens {
				if !cel.Afgekapt {
					t.Errorf("lange waarde had afgekapt moeten worden")
				}
				verwacht = verwacht[:maxCelTekens]
			}
			if cel.Waarde != verwacht {
				t.Errorf("waarde kwam anders terug:\ngot:  %q\nwant: %q", cel.Waarde, verwacht)
			}
			if cel.Null {
				t.Error("waarde werd als NULL gemeld")
			}

			// De tabel moet nog bestaan: als een injectiepoging was gelukt, was
			// hij weg.
			tabellen, err := c.Tables(ctx, dbNaam)
			if err != nil || len(tabellen) != 1 {
				t.Fatalf("tabel verdwenen na waarde %q (tabellen=%v, err=%v)", gw.naam, tabellen, err)
			}
		})
	}
}

func TestIntegratieNullIsIetsAndersDanLeeg(t *testing.T) {
	c, dbNaam := testClient(t)
	maakTestTabel(t, c, dbNaam)
	ctx := context.Background()

	if _, err := c.VoegRijToe(ctx, dbNaam, "wp_options", []NieuweWaarde{
		{Kolom: "option_name", Waarde: "met_null"},
		{Kolom: "option_value", Null: true},
	}); err != nil {
		t.Fatalf("VoegRijToe: %v", err)
	}
	if _, err := c.VoegRijToe(ctx, dbNaam, "wp_options", []NieuweWaarde{
		{Kolom: "option_name", Waarde: "met_leeg"},
		{Kolom: "option_value", Waarde: ""},
	}); err != nil {
		t.Fatalf("VoegRijToe: %v", err)
	}

	res, err := c.Rijen(ctx, RijenOpties{Database: dbNaam, Tabel: "wp_options", Sorteer: "option_name"})
	if err != nil {
		t.Fatalf("Rijen: %v", err)
	}
	idx := indexVanKolom(res.Kolommen, "option_value")
	if len(res.Rijen) != 2 {
		t.Fatalf("aantal rijen = %d", len(res.Rijen))
	}
	// Gesorteerd op naam: met_leeg vóór met_null.
	if res.Rijen[0][idx].Null {
		t.Error("lege string werd als NULL gemeld")
	}
	if !res.Rijen[1][idx].Null {
		t.Error("NULL werd niet als NULL gemeld")
	}
}

func TestIntegratieUpdateCelInclusiefNaarNull(t *testing.T) {
	c, dbNaam := testClient(t)
	maakTestTabel(t, c, dbNaam)
	ctx := context.Background()

	id, err := c.VoegRijToe(ctx, dbNaam, "wp_options", []NieuweWaarde{
		{Kolom: "option_name", Waarde: "siteurl"},
		{Kolom: "option_value", Waarde: "https://oud.nl"},
	})
	if err != nil {
		t.Fatalf("VoegRijToe: %v", err)
	}
	sleutel := []SleutelWaarde{{Kolom: "option_id", Waarde: fmt.Sprint(id)}}

	nieuw := `https://nieuw.test/pad?a='b'&c=\d`
	if err := c.UpdateCel(ctx, dbNaam, "wp_options", sleutel, "option_value", &nieuw); err != nil {
		t.Fatalf("UpdateCel: %v", err)
	}
	if got := leesCel(t, c, dbNaam, id, "option_value"); got.Waarde != nieuw {
		t.Errorf("waarde = %q, wil %q", got.Waarde, nieuw)
	}

	if err := c.UpdateCel(ctx, dbNaam, "wp_options", sleutel, "option_value", nil); err != nil {
		t.Fatalf("UpdateCel naar NULL: %v", err)
	}
	if got := leesCel(t, c, dbNaam, id, "option_value"); !got.Null {
		t.Errorf("waarde = %+v, wil NULL", got)
	}
}

func TestIntegratieUpdateWeigertOnbekendeRij(t *testing.T) {
	c, dbNaam := testClient(t)
	maakTestTabel(t, c, dbNaam)
	ctx := context.Background()

	nieuw := "x"
	err := c.UpdateCel(ctx, dbNaam, "wp_options",
		[]SleutelWaarde{{Kolom: "option_id", Waarde: "999999"}}, "option_value", &nieuw)
	if err == nil {
		t.Fatal("update op een niet-bestaande rij werd toegestaan")
	}
	if !strings.Contains(err.Error(), "bestaat niet meer") {
		t.Errorf("fout = %v", err)
	}
}

// TestIntegratieUpdateWeigertMeerdereRijen is de vangrail: een sleutel die op
// meer dan één rij past, mag niets wijzigen.
func TestIntegratieUpdateWeigertMeerdereRijen(t *testing.T) {
	c, dbNaam := testClient(t)
	ctx := context.Background()
	// Tabel zonder unieke sleutel, zodat één waarde op twee rijen past.
	if _, err := c.db.ExecContext(ctx, "CREATE TABLE `"+dbNaam+"`.`dubbel` (k INT, v VARCHAR(20))"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.ExecContext(ctx, "INSERT INTO `"+dbNaam+"`.`dubbel` VALUES (1,'a'),(1,'b')"); err != nil {
		t.Fatal(err)
	}

	nieuw := "gewijzigd"
	err := c.UpdateCel(ctx, dbNaam, "dubbel", []SleutelWaarde{{Kolom: "k", Waarde: "1"}}, "v", &nieuw)
	if err == nil {
		t.Fatal("update die twee rijen raakt werd toegestaan")
	}
	if !strings.Contains(err.Error(), "past op 2 rijen") {
		t.Errorf("fout = %v", err)
	}
	// En er mag niets gewijzigd zijn.
	res, err := c.Rijen(ctx, RijenOpties{Database: dbNaam, Tabel: "dubbel", Sorteer: "v"})
	if err != nil {
		t.Fatal(err)
	}
	idx := indexVanKolom(res.Kolommen, "v")
	if res.Rijen[0][idx].Waarde != "a" || res.Rijen[1][idx].Waarde != "b" {
		t.Errorf("rijen zijn toch aangepast: %v", res.Rijen)
	}
}

func TestIntegratieVerwijderRij(t *testing.T) {
	c, dbNaam := testClient(t)
	maakTestTabel(t, c, dbNaam)
	ctx := context.Background()

	id, _ := c.VoegRijToe(ctx, dbNaam, "wp_options", []NieuweWaarde{{Kolom: "option_name", Waarde: "weg"}})
	sleutel := []SleutelWaarde{{Kolom: "option_id", Waarde: fmt.Sprint(id)}}

	if err := c.VerwijderRij(ctx, dbNaam, "wp_options", sleutel); err != nil {
		t.Fatalf("VerwijderRij: %v", err)
	}
	n, err := c.Aantal(ctx, RijenOpties{Database: dbNaam, Tabel: "wp_options"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("aantal = %d, wil 0", n)
	}
	// Tweede keer moet falen in plaats van stil niets doen.
	if err := c.VerwijderRij(ctx, dbNaam, "wp_options", sleutel); err == nil {
		t.Error("tweede verwijdering werd toegestaan")
	}
}

func TestIntegratieBinaireKolom(t *testing.T) {
	c, dbNaam := testClient(t)
	maakTestTabel(t, c, dbNaam)
	ctx := context.Background()

	if _, err := c.db.ExecContext(ctx,
		"INSERT INTO `"+dbNaam+"`.`wp_options` (option_name, blob_kolom) VALUES ('bin', UNHEX('DEADBEEF00FF'))"); err != nil {
		t.Fatal(err)
	}
	res, err := c.Rijen(ctx, RijenOpties{Database: dbNaam, Tabel: "wp_options"})
	if err != nil {
		t.Fatal(err)
	}
	cel := res.Rijen[0][indexVanKolom(res.Kolommen, "blob_kolom")]
	if !cel.Binair {
		t.Errorf("blob werd niet als binair gemeld: %+v", cel)
	}
	if cel.Bytes != 6 {
		t.Errorf("bytes = %d, wil 6", cel.Bytes)
	}
}

// Een tabel zonder primary key hoort als niet-bewerkbaar te gelden.
func TestIntegratieTabelZonderPrimaryKey(t *testing.T) {
	c, dbNaam := testClient(t)
	ctx := context.Background()
	if _, err := c.db.ExecContext(ctx, "CREATE TABLE `"+dbNaam+"`.`geen_pk` (a INT, b VARCHAR(10))"); err != nil {
		t.Fatal(err)
	}
	tabellen, err := c.Tables(ctx, dbNaam)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabellen) != 1 {
		t.Fatalf("aantal tabellen = %d", len(tabellen))
	}
	if tabellen[0].Bewerkbaar() {
		t.Error("tabel zonder primary key werd als bewerkbaar gemeld")
	}
	if len(tabellen[0].PrimaryKeys) != 0 {
		t.Errorf("PrimaryKeys = %v", tabellen[0].PrimaryKeys)
	}
}

func TestIntegratieTabellenEnKolommen(t *testing.T) {
	c, dbNaam := testClient(t)
	maakTestTabel(t, c, dbNaam)
	ctx := context.Background()

	tabellen, err := c.Tables(ctx, dbNaam)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabellen) != 1 || tabellen[0].Naam != "wp_options" {
		t.Fatalf("tabellen = %+v", tabellen)
	}
	if !tabellen[0].Bewerkbaar() || tabellen[0].PrimaryKeys[0] != "option_id" {
		t.Errorf("primary key verkeerd: %+v", tabellen[0])
	}
	if tabellen[0].Engine != "InnoDB" {
		t.Errorf("engine = %q", tabellen[0].Engine)
	}

	kolommen, err := c.Columns(ctx, dbNaam, "wp_options")
	if err != nil {
		t.Fatal(err)
	}
	if len(kolommen) != 4 {
		t.Fatalf("aantal kolommen = %d", len(kolommen))
	}
	if !kolommen[0].IsPK || !kolommen[0].AutoIncr {
		t.Errorf("option_id = %+v", kolommen[0])
	}
	if kolommen[1].Nullable {
		t.Errorf("option_name hoort NOT NULL te zijn: %+v", kolommen[1])
	}
	if !kolommen[2].Nullable {
		t.Errorf("option_value hoort nullable te zijn: %+v", kolommen[2])
	}
}

func TestIntegratieKolommenVanOnbekendeTabel(t *testing.T) {
	c, dbNaam := testClient(t)
	if _, err := c.Columns(context.Background(), dbNaam, "bestaat_niet"); err == nil {
		t.Error("onbekende tabel gaf geen fout")
	}
}

// Een kolomnaam die niet in de tabel voorkomt mag nooit in een query komen, ook
// niet als hij de tekenvalidatie haalt.
func TestIntegratieWeigertOnbekendeKolom(t *testing.T) {
	c, dbNaam := testClient(t)
	maakTestTabel(t, c, dbNaam)
	ctx := context.Background()

	if _, err := c.Rijen(ctx, RijenOpties{Database: dbNaam, Tabel: "wp_options", Sorteer: "bestaat_niet"}); err == nil {
		t.Error("sorteren op een onbekende kolom werd toegestaan")
	}
	nieuw := "x"
	if err := c.UpdateCel(ctx, dbNaam, "wp_options",
		[]SleutelWaarde{{Kolom: "option_id", Waarde: "1"}}, "bestaat_niet", &nieuw); err == nil {
		t.Error("update op een onbekende kolom werd toegestaan")
	}
}

func TestIntegratiePaginering(t *testing.T) {
	c, dbNaam := testClient(t)
	maakTestTabel(t, c, dbNaam)
	ctx := context.Background()

	for i := 0; i < 25; i++ {
		if _, err := c.VoegRijToe(ctx, dbNaam, "wp_options", []NieuweWaarde{
			{Kolom: "option_name", Waarde: fmt.Sprintf("optie_%02d", i)},
		}); err != nil {
			t.Fatal(err)
		}
	}

	eerste, err := c.Rijen(ctx, RijenOpties{Database: dbNaam, Tabel: "wp_options", Sorteer: "option_name", Limiet: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(eerste.Rijen) != 10 || !eerste.Afgekapt {
		t.Errorf("eerste pagina: %d rijen, afgekapt=%v", len(eerste.Rijen), eerste.Afgekapt)
	}

	laatste, err := c.Rijen(ctx, RijenOpties{Database: dbNaam, Tabel: "wp_options", Sorteer: "option_name", Limiet: 10, Offset: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(laatste.Rijen) != 5 || laatste.Afgekapt {
		t.Errorf("laatste pagina: %d rijen, afgekapt=%v", len(laatste.Rijen), laatste.Afgekapt)
	}

	n, err := c.Aantal(ctx, RijenOpties{Database: dbNaam, Tabel: "wp_options"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 25 {
		t.Errorf("aantal = %d, wil 25", n)
	}
}

func TestIntegratieSelectEnExec(t *testing.T) {
	c, dbNaam := testClient(t)
	maakTestTabel(t, c, dbNaam)
	ctx := context.Background()

	res, err := c.Select(ctx, "SELECT 1 AS een, 'twee' AS tekst, NULL AS niks")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(res.Rijen) != 1 || len(res.Kolommen) != 3 {
		t.Fatalf("res = %+v", res)
	}
	if res.Rijen[0][0].Waarde != "1" || res.Rijen[0][1].Waarde != "twee" || !res.Rijen[0][2].Null {
		t.Errorf("rij = %+v", res.Rijen[0])
	}

	geraakt, _, err := c.Exec(ctx,
		"INSERT INTO `"+dbNaam+"`.`wp_options` (option_name) VALUES ('via_exec')")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if geraakt != 1 {
		t.Errorf("geraakt = %d, wil 1", geraakt)
	}
}

// MultiStatements staat uit, dus een tweede statement achter een puntkomma mag
// niet uitgevoerd worden.
func TestIntegratieGeenMultiStatements(t *testing.T) {
	c, dbNaam := testClient(t)
	maakTestTabel(t, c, dbNaam)
	ctx := context.Background()

	_, _, err := c.Exec(ctx, "INSERT INTO `"+dbNaam+"`.`wp_options` (option_name) VALUES ('een'); DROP TABLE `"+dbNaam+"`.`wp_options`")
	if err == nil {
		t.Error("twee statements in één aanroep werden geaccepteerd")
	}
	tabellen, _ := c.Tables(ctx, dbNaam)
	if len(tabellen) != 1 {
		t.Errorf("de tabel is verdwenen: %+v", tabellen)
	}
}

func TestIntegratieDatabasesVerbergtSysteemschemas(t *testing.T) {
	c, dbNaam := testClient(t)
	namen, err := c.Databases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var eigenGevonden bool
	for _, n := range namen {
		if systeemSchemas[n] {
			t.Errorf("systeemschema %q staat in de lijst", n)
		}
		if n == dbNaam {
			eigenGevonden = true
		}
	}
	if !eigenGevonden {
		t.Errorf("de testdatabase %q staat niet in de lijst", dbNaam)
	}
}

func leesCel(t *testing.T, c *Client, dbNaam string, id int64, kolom string) Cel {
	t.Helper()
	res, err := c.Rijen(context.Background(), RijenOpties{
		Database: dbNaam, Tabel: "wp_options",
		ZoekKolom: "option_id", Zoek: fmt.Sprint(id),
	})
	if err != nil {
		t.Fatalf("Rijen: %v", err)
	}
	if len(res.Rijen) == 0 {
		t.Fatalf("rij %d niet gevonden", id)
	}
	return res.Rijen[0][indexVanKolom(res.Kolommen, kolom)]
}

func indexVanKolom(kolommen []string, naam string) int {
	for i, k := range kolommen {
		if k == naam {
			return i
		}
	}
	return -1
}

func TestDSNWeigertNietLokaleHost(t *testing.T) {
	for _, host := range []string{"mysql.example.com", "10.0.0.5", "prod-db.kinsta.cloud", ""} {
		if _, err := (Config{Host: host, Port: 3306, User: "root"}).DSN(); err == nil {
			t.Errorf("host %q werd geaccepteerd", host)
		}
	}
	for _, host := range []string{"127.0.0.1", "localhost", "::1", "LOCALHOST"} {
		if _, err := (Config{Host: host, Port: 3306, User: "root"}).DSN(); err != nil {
			t.Errorf("host %q werd geweigerd: %v", host, err)
		}
	}
}

func TestDSNValideertPoortEnGebruiker(t *testing.T) {
	if _, err := (Config{Host: "127.0.0.1", Port: 0, User: "root"}).DSN(); err == nil {
		t.Error("poort 0 werd geaccepteerd")
	}
	if _, err := (Config{Host: "127.0.0.1", Port: 70000, User: "root"}).DSN(); err == nil {
		t.Error("poort 70000 werd geaccepteerd")
	}
	if _, err := (Config{Host: "127.0.0.1", Port: 3306, User: ""}).DSN(); err == nil {
		t.Error("lege gebruiker werd geaccepteerd")
	}
}

// De DSN moet server-side placeholders houden: zodra interpolateParams aanstaat,
// bouwt de driver zelf SQL-strings en is de escaping-garantie weg.
func TestDSNHoudtPlaceholdersServerSide(t *testing.T) {
	dsn, err := (Config{Host: "127.0.0.1", Port: 3306, User: "root", Password: "x"}).DSN()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dsn, "interpolateParams=true") {
		t.Errorf("interpolateParams staat aan: %s", dsn)
	}
	if strings.Contains(dsn, "multiStatements=true") {
		t.Errorf("multiStatements staat aan: %s", dsn)
	}
}
