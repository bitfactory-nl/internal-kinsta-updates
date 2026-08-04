package services

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

// TestAnonymiseSQLNeverTouchesRemote bevestigt dat dit bestand alleen lokale SQL
// bouwt: elke functie hoort met "buildLocal" of "valideer"/"quote" te beginnen,
// zodat de bestaande guard-tests dit bestand als lokaal-only behandelen en er
// nooit per ongeluk een remote commando in belandt.
func TestAnonymiseSQLNeverTouchesRemote(t *testing.T) {
	raw, err := os.ReadFile("anonymise_sql.go")
	if err != nil {
		t.Fatalf("anonymise_sql.go lezen: %v", err)
	}
	src := zonderGoCommentaar(string(raw))

	for _, verboden := range []string{"wp ", "RunCommand", "DownloadCommand", "ssh", "tar "} {
		if strings.Contains(src, verboden) {
			t.Errorf("anonymise_sql.go bevat %q; dit bestand mag alleen lokale SQL bouwen", verboden)
		}
	}
	for naam := range functieTeksten(t, src) {
		if !strings.HasPrefix(naam, "buildLocal") && !strings.HasPrefix(naam, "valideer") &&
			!strings.HasPrefix(naam, "quote") && !strings.HasPrefix(naam, "escape") {
			t.Errorf("functie %s: geef lokale SQL-bouwers een buildLocal-prefix, anders slaan de guard-tests hem over", naam)
		}
	}
}

// reDeleteDoel haalt de doeltabel uit een "DELETE `x` FROM ..."-statement.
var reDeleteDoel = regexp.MustCompile("DELETE\\s+`([A-Za-z0-9_]+)`\\s+FROM")

// TestGegenereerdeSQLNoemtDoeltabelNietInSubquery vangt een hele klasse fouten
// af die geen enkele test met een nagemaakte database kan zien: MySQL en MariaDB
// weigeren een UPDATE of DELETE waarin de doeltabel óók in een subquery van
// hetzelfde statement voorkomt, met "ERROR 1093: You can't specify target table
// for update in FROM clause".
//
// Dat is hier echt gebeurd: het predicaat dat bepaalt welke gebruikers hun
// gegevens houden bevatte een subquery op usermeta, en datzelfde predicaat werd
// gebruikt in een DELETE op usermeta. De unit tests draaiden tegen een fake die
// SQL nooit uitvoert, dus alles was groen terwijl de feature in het
// hoofdscenario (een rol behouden) bij elke echte kloon zou klappen. De oplossing
// is de subquery in een derived table wikkelen; deze test controleert dat dat zo
// blijft.
func TestGegenereerdeSQLNoemtDoeltabelNietInSubquery(t *testing.T) {
	// Alle statements die deze feature echt naar de database stuurt, met de
	// configuratie waarin het misging: een rol behouden.
	gevallen := map[string]func() (string, error){
		"gebruikers met rol behouden": func() (string, error) {
			return buildLocalAnonymiseUsersSQL("wp_users", "wp_usermeta", []string{"administrator"}, nil)
		},
		"gebruikers met rol en login behouden": func() (string, error) {
			return buildLocalAnonymiseUsersSQL("wp_users", "wp_usermeta", []string{"administrator", "shop_manager"}, []string{"jeffrey"})
		},
		"woocommerce-orders": func() (string, error) {
			return buildLocalAnonymiseWooLegacyOrdersSQL([][2]string{{"wp_posts", "wp_postmeta"}})
		},
	}

	for naam, bouw := range gevallen {
		sql, err := bouw()
		if err != nil {
			t.Fatalf("%s: %v", naam, err)
		}
		for _, statement := range strings.Split(sql, ";") {
			m := reDeleteDoel.FindStringSubmatch(statement)
			if m == nil {
				continue
			}
			doel := m[1]
			// Alles ná de eerste FROM-clausule met de doeltabel: als de doeltabel
			// binnen een subquery (tussen haakjes na SELECT) nog eens voorkomt,
			// weigert MySQL het statement.
			idx := strings.Index(statement, "SELECT")
			if idx < 0 {
				continue
			}
			subquery := statement[idx:]
			if strings.Contains(subquery, "`"+doel+"`") && !strings.Contains(subquery, ") AS x") {
				t.Errorf("%s: DELETE op %s heeft die tabel ook in een subquery zonder derived-table-wrap;"+
					" MySQL weigert dat met ERROR 1093. Statement:\n%s", naam, doel, statement)
			}
		}
	}
}

// TestKeptUsersPredicateGebruiktDerivedTable maakt de eis uit de vorige test
// expliciet op de plek waar hij ontstaat.
func TestKeptUsersPredicateGebruiktDerivedTable(t *testing.T) {
	pred, err := buildLocalKeptUsersPredicate("wp_usermeta", []string{"administrator"}, nil)
	if err != nil {
		t.Fatalf("buildLocalKeptUsersPredicate: %v", err)
	}
	if !strings.Contains(pred, ") AS x)") {
		t.Errorf("predicaat mist de derived-table-wrap die ERROR 1093 voorkomt: %s", pred)
	}
	// Zonder deze check zou een NULL in de lijst van NOT IN(...) het hele
	// predicaat NULL maken, en dan wordt er stil niemand geanonimiseerd.
	if !strings.Contains(pred, "um.user_id IS NOT NULL") {
		t.Errorf("predicaat sluit NULL-user_id niet uit: %s", pred)
	}
}

func TestBuildLocalAnonymiseWooLegacyOrdersSQL(t *testing.T) {
	sql, err := buildLocalAnonymiseWooLegacyOrdersSQL([][2]string{
		{"wp_posts", "wp_postmeta"},
		{"wp_2_posts", "wp_2_postmeta"}, // multisite
	})
	if err != nil {
		t.Fatalf("buildLocalAnonymiseWooLegacyOrdersSQL: %v", err)
	}
	// Orders zonder HPOS staan als post met hun klantgegevens in postmeta; die
	// worden door geen enkele TRUNCATE geraakt.
	if !strings.Contains(sql, "LEFT(`wp_postmeta`.meta_key, 9) = '_billing_'") {
		t.Errorf("factuurgegevens worden niet verwijderd: %s", sql)
	}
	if !strings.Contains(sql, "'_customer_ip_address'") {
		t.Error("het IP-adres van de klant wordt niet verwijderd")
	}
	if !strings.Contains(sql, "'shop_order'") {
		t.Error("verwacht dat alleen orders worden geraakt, niet alle posts")
	}
	// De klantnotitie staat in post_excerpt.
	if !strings.Contains(sql, "SET post_excerpt = ''") {
		t.Error("de klantnotitie bij een order wordt niet gewist")
	}
	// Per site een set statements.
	if !strings.Contains(sql, "`wp_2_postmeta`") {
		t.Error("de multisite-variant mist")
	}
}

func TestBuildLocalAnonymiseWooLegacyOrdersSQLWeigertOngeldigeTabel(t *testing.T) {
	if _, err := buildLocalAnonymiseWooLegacyOrdersSQL([][2]string{{"wp_posts; DROP", "wp_postmeta"}}); err == nil {
		t.Fatal("verwachtte een fout op een ongeldige tabelnaam")
	}
}

func TestBuildLocalAnonymiseOptionsSQL(t *testing.T) {
	sql, err := buildLocalAnonymiseOptionsSQL([]string{"wp_options", "wp_2_options"})
	if err != nil {
		t.Fatalf("buildLocalAnonymiseOptionsSQL: %v", err)
	}
	if strings.Count(sql, "UPDATE") != 2 {
		t.Errorf("verwacht een statement per site: %s", sql)
	}
	if !strings.Contains(sql, "'admin_email'") || !strings.Contains(sql, "'new_admin_email'") {
		t.Errorf("verwacht beide beheerders-e-mailopties: %s", sql)
	}
	// Een lege optie hoeft niet gevuld te worden met een nepadres.
	if !strings.Contains(sql, "option_value <> ''") {
		t.Error("verwacht dat lege opties worden overgeslagen")
	}
}

func TestValideerIdentifierWeigertInjectie(t *testing.T) {
	geldig := []string{"wp_users", "wp_2_options", "wpforms_entries", "A1_b2"}
	for _, n := range geldig {
		if err := valideerIdentifier(n); err != nil {
			t.Errorf("valideerIdentifier(%q) = %v, wil toegestaan", n, err)
		}
	}
	ongeldig := []string{
		"wp_users; DROP TABLE x", "wp_users`", "wp users", "wp-users",
		"`wp_users`", "wp_users--", "", "wp_users'",
	}
	for _, n := range ongeldig {
		if err := valideerIdentifier(n); err == nil {
			t.Errorf("valideerIdentifier(%q) toegestaan, wil geweigerd", n)
		}
	}
}

func TestBuildLocalEmptyTablesSQL(t *testing.T) {
	sql, err := buildLocalEmptyTablesSQL([]string{"wp_wpforms_entries", "wp_gf_entry"})
	if err != nil {
		t.Fatalf("buildLocalEmptyTablesSQL: %v", err)
	}
	if !strings.Contains(sql, "TRUNCATE TABLE `wp_wpforms_entries`;") {
		t.Errorf("verwacht een TRUNCATE met backticks, kreeg: %s", sql)
	}
	// TRUNCATE weigert op tabellen waar een foreign key naar verwijst; dat komt
	// in WooCommerce-schema's voor, dus de check moet er even uit.
	if !strings.HasPrefix(sql, "SET FOREIGN_KEY_CHECKS=0;") || !strings.HasSuffix(sql, "SET FOREIGN_KEY_CHECKS=1;") {
		t.Errorf("verwacht dat FK-checks eromheen uit en weer aan gaan, kreeg: %s", sql)
	}
}

func TestBuildLocalEmptyTablesSQLWeigertOngeldigeNaam(t *testing.T) {
	if _, err := buildLocalEmptyTablesSQL([]string{"wp_users; DROP DATABASE dev"}); err == nil {
		t.Fatal("verwachtte een fout op een tabelnaam met SQL erin")
	}
}

func TestBuildLocalEmptyTablesSQLLeegIsLeeg(t *testing.T) {
	sql, err := buildLocalEmptyTablesSQL(nil)
	if err != nil {
		t.Fatalf("onverwachte fout: %v", err)
	}
	if sql != "" {
		t.Errorf("verwacht lege SQL zonder tabellen, kreeg: %q", sql)
	}
}

func TestBuildLocalKeptUsersPredicateRollenEnLogins(t *testing.T) {
	pred, err := buildLocalKeptUsersPredicate("wp_usermeta", []string{"administrator", "shop_manager"}, []string{"jeffrey"})
	if err != nil {
		t.Fatalf("buildLocalKeptUsersPredicate: %v", err)
	}
	// LOCATE in plaats van LIKE: een rolnaam met een underscore (shop_manager)
	// zou als LIKE-patroon een jokerteken bevatten en te veel matchen.
	if strings.Contains(pred, "LIKE '%\"shop_manager\"%'") {
		t.Errorf("rolzoekactie gebruikt LIKE; underscore is daar een jokerteken: %s", pred)
	}
	if !strings.Contains(pred, `LOCATE('"shop_manager"', um.meta_value) > 0`) {
		t.Errorf("verwacht LOCATE voor de rolnaam, kreeg: %s", pred)
	}
	if !strings.Contains(pred, "RIGHT(um.meta_key, 12) = 'capabilities'") {
		t.Errorf("verwacht een exacte capabilities-suffixcheck, kreeg: %s", pred)
	}
	if !strings.Contains(pred, "u.user_login IN ('jeffrey')") {
		t.Errorf("verwacht de login-uitzondering, kreeg: %s", pred)
	}
}

func TestBuildLocalKeptUsersPredicateZonderUitzonderingen(t *testing.T) {
	// Niemand bewaren betekent: het predicaat is voor iedereen onwaar. Als dit
	// per ongeluk "1=1" zou worden, werd er niemand geanonimiseerd.
	pred, err := buildLocalKeptUsersPredicate("wp_usermeta", nil, nil)
	if err != nil {
		t.Fatalf("onverwachte fout: %v", err)
	}
	if pred != "1=0" {
		t.Errorf("predicaat = %q, wil 1=0 (niemand uitgezonderd)", pred)
	}
}

func TestBuildLocalKeptUsersPredicateWeigertRareRol(t *testing.T) {
	if _, err := buildLocalKeptUsersPredicate("wp_usermeta", []string{`admin"' OR 1=1 --`}, nil); err == nil {
		t.Fatal("verwachtte een fout op een rolnaam met SQL erin")
	}
}

func TestBuildLocalKeptUsersPredicateEscapeertLogin(t *testing.T) {
	pred, err := buildLocalKeptUsersPredicate("wp_usermeta", nil, []string{"o'brien"})
	if err != nil {
		t.Fatalf("onverwachte fout: %v", err)
	}
	if !strings.Contains(pred, "'o''brien'") {
		t.Errorf("aanhalingsteken in login niet ge-escaped: %s", pred)
	}
}

func TestBuildLocalAnonymiseUsersSQL(t *testing.T) {
	sql, err := buildLocalAnonymiseUsersSQL("wp_users", "wp_usermeta", []string{"administrator"}, nil)
	if err != nil {
		t.Fatalf("buildLocalAnonymiseUsersSQL: %v", err)
	}

	// De vervangende waarden moeten van het ID zijn afgeleid: user_login,
	// user_nicename en user_email hebben een unieke index, dus een vaste waarde
	// zou bij de tweede rij al klappen.
	for _, wil := range []string{
		"CONCAT('gebruiker-', u.ID, '@voorbeeld.test')",
		"u.user_login = CONCAT('gebruiker-', u.ID)",
		"u.user_nicename = CONCAT('gebruiker-', u.ID)",
	} {
		if !strings.Contains(sql, wil) {
			t.Errorf("verwacht %q in de SQL, kreeg: %s", wil, sql)
		}
	}
	// De wachtwoordhash is een credential: die hoort niet op een dev-machine te
	// blijven staan voor accounts die we juist anonimiseren.
	if !strings.Contains(sql, "u.user_pass = ''") {
		t.Error("verwacht dat de wachtwoordhash wordt geleegd voor geanonimiseerde accounts")
	}
	if !strings.Contains(sql, "WHERE NOT (") {
		t.Errorf("verwacht dat alleen niet-bewaarde gebruikers worden geraakt, kreeg: %s", sql)
	}
	// Adresgegevens uit WooCommerce zitten in usermeta met een billing_/shipping_-prefix.
	if !strings.Contains(sql, "'billing_'") || !strings.Contains(sql, "'shipping_'") {
		t.Error("verwacht dat billing_/shipping_-metavelden worden verwijderd")
	}
	// Sessietokens van ALLE gebruikers, ook de bewaarde.
	if !strings.Contains(sql, "meta_key = 'session_tokens'") {
		t.Error("verwacht dat sessietokens uit productie worden verwijderd")
	}
}

func TestBuildLocalAnonymiseUsersSQLWeigertOngeldigeTabel(t *testing.T) {
	if _, err := buildLocalAnonymiseUsersSQL("wp_users`; DROP", "wp_usermeta", nil, nil); err == nil {
		t.Fatal("verwachtte een fout op een ongeldige tabelnaam")
	}
}

func TestBuildLocalAnonymiseCommentsSQLPerTabel(t *testing.T) {
	// Multisite heeft een comments-tabel per site; er hoort dus per tabel een
	// statement te komen.
	sql, err := buildLocalAnonymiseCommentsSQL([]string{"wp_comments", "wp_2_comments"})
	if err != nil {
		t.Fatalf("buildLocalAnonymiseCommentsSQL: %v", err)
	}
	if strings.Count(sql, "UPDATE") != 2 {
		t.Errorf("verwacht twee UPDATE's (een per site), kreeg: %s", sql)
	}
	for _, wil := range []string{"comment_author_IP = ''", "comment_agent = ''", "comment_author_url = ''"} {
		if !strings.Contains(sql, wil) {
			t.Errorf("verwacht %q, kreeg: %s", wil, sql)
		}
	}
	// Een leeg e-mailveld moet leeg blijven in plaats van een nepadres te krijgen.
	if !strings.Contains(sql, "CASE WHEN comment_author_email = '' THEN ''") {
		t.Error("verwacht dat een leeg e-mailadres leeg blijft")
	}
}

func TestBuildLocalKeptUserCountSQL(t *testing.T) {
	sql, err := buildLocalKeptUserCountSQL("wp_users", "wp_usermeta", []string{"administrator"}, nil)
	if err != nil {
		t.Fatalf("buildLocalKeptUserCountSQL: %v", err)
	}
	if !strings.HasPrefix(sql, "SELECT COUNT(*) FROM `wp_users` u WHERE ") {
		t.Errorf("onverwachte telling-SQL: %s", sql)
	}
}

// TestAnonymiseCatalogusHerkentEchteTabellen loopt langs een tabellenlijst zoals
// die op een echte klantsite voorkomt.
func TestAnonymiseCatalogusHerkentEchteTabellen(t *testing.T) {
	inventaris := []tabelRij{
		{Naam: "wp_posts", Rijen: 5000},
		{Naam: "wp_options", Rijen: 800},
		{Naam: "wp_users", Rijen: 1200},
		{Naam: "wp_wpforms_entries", Rijen: 12000},
		{Naam: "wp_gf_entry", Rijen: 450},
		{Naam: "wp_mailpoet_subscribers", Rijen: 9000},
		{Naam: "wp_wc_orders", Rijen: 3400},
		{Naam: "wp_wfhits", Rijen: 250000},
		{Naam: "wp_2_wpforms_entries", Rijen: 60}, // multisite subsite
		{Naam: "wp_termmeta", Rijen: 20},
	}
	gevonden := vindGevoeligeTabellen(inventaris, "wp_")

	namen := map[string]domain.SensitiveTable{}
	for _, g := range gevonden {
		namen[g.Name] = g
	}
	for _, wil := range []string{
		"wp_wpforms_entries", "wp_gf_entry", "wp_mailpoet_subscribers",
		"wp_wc_orders", "wp_wfhits", "wp_2_wpforms_entries",
	} {
		if _, ok := namen[wil]; !ok {
			t.Errorf("%s niet als gevoelig herkend", wil)
		}
	}
	for _, nietWil := range []string{"wp_posts", "wp_options", "wp_termmeta", "wp_users"} {
		if _, ok := namen[nietWil]; ok {
			t.Errorf("%s is als gevoelig aangemerkt; die hoort apart behandeld te worden, niet geleegd", nietWil)
		}
	}
	// De multisite-variant moet dezelfde categorie en reden krijgen als de
	// hoofdsite-tabel.
	if namen["wp_2_wpforms_entries"].Category != domain.AVGFormulieren {
		t.Errorf("multisite-variant kreeg categorie %q", namen["wp_2_wpforms_entries"].Category)
	}
	if namen["wp_wpforms_entries"].Rows != 12000 {
		t.Errorf("rijaantal niet doorgegeven: %+v", namen["wp_wpforms_entries"])
	}
}

func TestStripTabelPrefix(t *testing.T) {
	cases := map[string]string{
		"wp_wpforms_entries":   "wpforms_entries",
		"wp_2_wpforms_entries": "wpforms_entries",
		"wp_12_options":        "options",
		"wp_users":             "users",
	}
	for in, wil := range cases {
		if got := stripTabelPrefix(in, "wp_"); got != wil {
			t.Errorf("stripTabelPrefix(%q) = %q, wil %q", in, got, wil)
		}
	}
}

func TestVindTabellenOpSuffixVindtAlleSites(t *testing.T) {
	inventaris := []tabelRij{
		{Naam: "wp_comments"}, {Naam: "wp_2_comments"}, {Naam: "wp_5_comments"},
		{Naam: "wp_commentmeta"}, {Naam: "wp_posts"},
	}
	got := vindTabellenOpSuffix(inventaris, "wp_", "comments")
	if len(got) != 3 {
		t.Errorf("verwacht 3 comments-tabellen (multisite), kreeg %v", got)
	}
}

func TestParseTableInventory(t *testing.T) {
	out := strings.Join([]string{
		"ruis van een plugin",
		"RDM-PREFIX:wp_",
		"RDM-TBL:5000\twp_posts",
		"RDM-TBL:12000\twp_wpforms_entries",
		"RDM-TBL:0\twp_options",
	}, "\n")
	rijen := parseTableInventory(out)
	if len(rijen) != 3 {
		t.Fatalf("aantal = %d: %+v", len(rijen), rijen)
	}
	if rijen[1].Naam != "wp_wpforms_entries" || rijen[1].Rijen != 12000 {
		t.Errorf("tweede rij = %+v", rijen[1])
	}
}

func TestParseRoles(t *testing.T) {
	out := "ruis\nRDM-ROLE:administrator\nRDM-ROLE:editor\nRDM-ROLE:shop_manager\nRDM-ROLE:editor\n"
	rollen := parseRoles(out)
	if len(rollen) != 3 {
		t.Fatalf("verwacht 3 unieke rollen, kreeg %v", rollen)
	}
	if rollen[0] != "administrator" || rollen[2] != "shop_manager" {
		t.Errorf("rollen = %v", rollen)
	}
}
