package services

import (
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

// anonProject zet een project neer met een AVG-configuratie in .rdm.yml.
func anonProject(t *testing.T, dir string, cfg *domain.AnonymiseCfg) (*DBCloneService, *fakeDockerExec, *fakeDBSSH) {
	t.Helper()
	writeEnvFile(t, dir)

	dump := gzipBytes(t, "-- fake dump --\n")
	ssh := &fakeDBSSH{downloadContent: dump}
	docker := &fakeDockerExec{
		tableNames: []string{
			"wp_options", "wp_posts", "wp_users", "wp_usermeta", "wp_comments",
			"wp_wpforms_entries", "wp_mailpoet_subscribers",
		},
		siteURL:       "https://vanluykennl.test",
		userCount:     1200,
		keptUserCount: 3,
		commentCount:  450,
	}
	svc, ps := newDBCloneService(t, ssh, docker, dir)

	// De AVG-instellingen komen uit .rdm.yml, niet uit het verzoek: zo kan een
	// kloon niet per ongeluk zonder anonimisatie lopen.
	p, _ := ps.Get("p1")
	p.Config.Migration = &domain.MigrationCfg{Anonymise: cfg}
	ps.UpdateProjectConfig("p1", p.Config)

	return svc, docker, ssh
}

func standaardVerzoek() domain.DBCloneRequest {
	return domain.DBCloneRequest{
		ProdSiteURL: "https://vanluyken.nl",
		LocalURL:    "https://vanluykennl.test",
		LocalDBName: "dev_vanluykennl",
		LocalDBHost: "mysql",
		TablePrefix: "wp_",
	}
}

func TestCloneAnonymiseertVolgensConfig(t *testing.T) {
	svc, docker, _ := anonProject(t, t.TempDir(), &domain.AnonymiseCfg{
		Enabled:           true,
		AnonymiseUsers:    true,
		AnonymiseComments: true,
		KeepRoles:         []string{"administrator"},
		EmptyTables:       []string{"wp_wpforms_entries", "wp_mailpoet_subscribers"},
	})

	res, err := svc.Clone("p1", "env-1", standaardVerzoek())
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if res.Anonymise == nil {
		t.Fatal("geen anonimisatie-rapport in het resultaat")
	}
	if res.Anonymise.Skipped {
		t.Error("Skipped = true terwijl anonimisatie aanstond")
	}
	if len(res.Anonymise.TablesEmptied) != 2 {
		t.Errorf("TablesEmptied = %v, wil beide tabellen", res.Anonymise.TablesEmptied)
	}
	if res.Anonymise.UsersKept != 3 || res.Anonymise.UsersAnonymised != 1197 {
		t.Errorf("gebruikers: bewaard=%d geanonimiseerd=%d (wil 3 / 1197)",
			res.Anonymise.UsersKept, res.Anonymise.UsersAnonymised)
	}
	if res.Anonymise.CommentsAnonymised != 450 {
		t.Errorf("CommentsAnonymised = %d, wil 450", res.Anonymise.CommentsAnonymised)
	}

	// Controleer wat er werkelijk naar de lokale database is gestuurd.
	var alleSQL []string
	for _, c := range docker.snapshot() {
		alleSQL = append(alleSQL, strings.Join(c.args, " "))
	}
	samen := strings.Join(alleSQL, "\n")

	if !strings.Contains(samen, "TRUNCATE TABLE `wp_wpforms_entries`") {
		t.Error("formulierinzendingen zijn niet geleegd")
	}
	if !strings.Contains(samen, "u.user_email = CONCAT('gebruiker-'") {
		t.Error("gebruikers zijn niet geanonimiseerd")
	}
	if !strings.Contains(samen, "comment_author_IP = ''") {
		t.Error("reacties zijn niet geanonimiseerd")
	}
}

// TestCloneStoptHardAlsAnonimiseerenFaalt is de belangrijkste test van deze
// feature: als het anonimiseren mislukt, mag de kloon niet als geslaagd worden
// gerapporteerd — dan zou je denken dat er geen persoonsgegevens meer staan.
func TestCloneStoptHardAlsAnonimiseerenFaalt(t *testing.T) {
	dir := t.TempDir()
	svc, docker, _ := anonProject(t, dir, &domain.AnonymiseCfg{
		Enabled:     true,
		EmptyTables: []string{"wp_wpforms_entries"},
	})
	docker.failOn = func(args []string) bool {
		return strings.Contains(strings.Join(args, " "), "TRUNCATE TABLE")
	}

	_, err := svc.Clone("p1", "env-1", standaardVerzoek())
	if err == nil {
		t.Fatal("Clone slaagde terwijl het anonimiseren faalde")
	}
	// De melding moet onomwonden zeggen dat er nog persoonsgegevens staan.
	if !strings.Contains(err.Error(), "persoonsgegevens") {
		t.Errorf("foutmelding = %v; wil een expliciete waarschuwing dat de lokale database nog persoonsgegevens bevat", err)
	}
}

func TestCloneWaarschuwtAlsAnonimisatieUitstaat(t *testing.T) {
	svc, _, _ := anonProject(t, t.TempDir(), &domain.AnonymiseCfg{Enabled: false})

	res, err := svc.Clone("p1", "env-1", standaardVerzoek())
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if res.Anonymise == nil || !res.Anonymise.Skipped {
		t.Fatalf("verwacht een rapport met Skipped=true, kreeg %+v", res.Anonymise)
	}
	gevonden := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "anonimisatie staat UIT") {
			gevonden = true
		}
	}
	if !gevonden {
		t.Errorf("verwacht een duidelijke waarschuwing in het resultaat, kreeg: %v", res.Warnings)
	}
}

func TestCloneZonderAVGBlokWaarschuwtOok(t *testing.T) {
	// Een project waar nog nooit AVG-instellingen zijn ingevuld mag niet stil
	// zonder anonimisatie klonen.
	dir := t.TempDir()
	writeEnvFile(t, dir)
	dump := gzipBytes(t, "-- dump --\n")
	docker := &fakeDockerExec{tableNames: []string{"wp_options"}, siteURL: "https://vanluykennl.test"}
	svc, _ := newDBCloneService(t, &fakeDBSSH{downloadContent: dump}, docker, dir)

	res, err := svc.Clone("p1", "env-1", standaardVerzoek())
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if res.Anonymise == nil || !res.Anonymise.Skipped {
		t.Error("verwacht dat een ontbrekend AVG-blok als 'niet geanonimiseerd' wordt gemeld")
	}
}

func TestAnonymiseMeldtOntbrekendeTabellen(t *testing.T) {
	svc, _, _ := anonProject(t, t.TempDir(), &domain.AnonymiseCfg{
		Enabled: true,
		// wp_gf_entry staat niet in de tabellenlijst van de fake.
		EmptyTables: []string{"wp_wpforms_entries", "wp_gf_entry"},
	})

	res, err := svc.Clone("p1", "env-1", standaardVerzoek())
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if len(res.Anonymise.TablesMissing) != 1 || res.Anonymise.TablesMissing[0] != "wp_gf_entry" {
		t.Errorf("TablesMissing = %v; een geconfigureerde maar afwezige tabel hoort gemeld te worden, niet stil overgeslagen",
			res.Anonymise.TablesMissing)
	}
	if len(res.Anonymise.TablesEmptied) != 1 {
		t.Errorf("TablesEmptied = %v", res.Anonymise.TablesEmptied)
	}
}

func TestAnonymiseWaarschuwtAlsNiemandBewaardBlijft(t *testing.T) {
	dir := t.TempDir()
	svc, docker, _ := anonProject(t, dir, &domain.AnonymiseCfg{
		Enabled:        true,
		AnonymiseUsers: true,
		// Geen rollen of logins bewaard.
	})
	docker.keptUserCount = 0

	res, err := svc.Clone("p1", "env-1", standaardVerzoek())
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	gevonden := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "niet inloggen") {
			gevonden = true
		}
	}
	if !gevonden {
		t.Errorf("verwacht een waarschuwing dat lokaal inloggen onmogelijk wordt, kreeg: %v", res.Warnings)
	}
}

func TestAnonymiseFaaltAlsGebruikerstabelMist(t *testing.T) {
	dir := t.TempDir()
	svc, docker, _ := anonProject(t, dir, &domain.AnonymiseCfg{
		Enabled:        true,
		AnonymiseUsers: true,
	})
	// Geen users/usermeta in de geïmporteerde database.
	docker.tableNames = []string{"wp_options", "wp_posts"}

	_, err := svc.Clone("p1", "env-1", standaardVerzoek())
	if err == nil {
		t.Fatal("verwachtte een harde fout: zonder gebruikerstabel kan niet geanonimiseerd worden")
	}
	if !strings.Contains(err.Error(), "persoonsgegevens") {
		t.Errorf("foutmelding = %v; wil de expliciete waarschuwing", err)
	}
}

func TestInspectSensitiveData(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir)
	ssh := &fakeDBSSHWithFixedOutput{out: strings.Join([]string{
		"RDM-PREFIX:wp_",
		"RDM-TBL:5000\twp_posts",
		"RDM-TBL:1200\twp_users",
		"RDM-TBL:12000\twp_wpforms_entries",
		"RDM-TBL:250000\twp_wfhits",
		"RDM-ROLE:administrator",
		"RDM-ROLE:editor",
	}, "\n")}
	svc, _ := newDBCloneService(t, ssh, &fakeDockerExec{}, dir)

	rapport, err := svc.InspectSensitiveData("p1", "env-1")
	if err != nil {
		t.Fatalf("InspectSensitiveData: %v", err)
	}
	if rapport.TablePrefix != "wp_" {
		t.Errorf("TablePrefix = %q", rapport.TablePrefix)
	}
	if len(rapport.Tables) != 2 {
		t.Fatalf("verwacht 2 gevoelige tabellen, kreeg %+v", rapport.Tables)
	}
	if rapport.UserCount != 1200 {
		t.Errorf("UserCount = %d, wil 1200", rapport.UserCount)
	}
	if len(rapport.Roles) != 2 || rapport.Roles[0] != "administrator" {
		t.Errorf("Roles = %v", rapport.Roles)
	}
	if len(rapport.AllTables) != 4 {
		t.Errorf("AllTables = %v; de volledige lijst hoort mee te komen zodat de UI ook onbekende tabellen kan tonen", rapport.AllTables)
	}
	// Formulieren staan voor logboeken: de volgorde is voorspelbaar gesorteerd.
	if rapport.Tables[0].Name != "wp_wpforms_entries" {
		t.Errorf("eerste tabel = %q, wil de formulierinzendingen bovenaan", rapport.Tables[0].Name)
	}
}

func TestInspectSensitiveDataMeldtServerfout(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir)
	ssh := &fakeDBSSHWithFixedOutput{out: "RDM-ERR:geen wp-config.php gevonden"}
	svc, _ := newDBCloneService(t, ssh, &fakeDockerExec{}, dir)

	if _, err := svc.InspectSensitiveData("p1", "env-1"); err == nil {
		t.Fatal("verwachtte een duidelijke serverfout")
	}
}
