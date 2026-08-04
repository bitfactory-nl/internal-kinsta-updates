package services

import (
	"os"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

// TestMigrationCommandsNeverMutateRemote is dezelfde garantie als bij de
// database-kloon: alles wat dit bestand naar de klantserver stuurt is
// alleen-lezen. Een media-pull mag nooit iets op productie aanraken.
func TestMigrationCommandsNeverMutateRemote(t *testing.T) {
	raw, err := os.ReadFile("migration_commands.go")
	if err != nil {
		t.Fatalf("migration_commands.go lezen: %v", err)
	}
	src := zonderGoCommentaar(string(raw))
	functies := functieTeksten(t, src)

	verboden := []string{
		" rm ", "rm -", "unlink", "mv ", "chmod", "chown",
		" UPDATE ", " DELETE ", "INSERT INTO", "DROP ", "TRUNCATE",
		"wp db import", "wp option update", "wp search-replace",
		// tar mag alleen inpakken naar stdout, nooit uitpakken op de server.
		"tar xzf", "tar -x", "tar xf",
	}

	getest := 0
	for naam, tekst := range functies {
		if strings.HasPrefix(naam, "buildLocal") || strings.HasPrefix(naam, "parse") {
			continue
		}
		getest++
		for _, v := range verboden {
			if strings.Contains(tekst, v) {
				t.Errorf("functie %s bevat verboden %q — dit zou iets op de productieserver kunnen wijzigen", naam, v)
			}
		}
	}
	if getest == 0 {
		t.Fatal("geen remote-commandbouwers gevonden om te controleren — guard-test dekt niets")
	}
}

// TestMigrationServiceOnlyStreamsWithKnownSafeBuilders dwingt af dat elke
// remote aanroep in migration_service.go via een bekende, door de guard-test
// gedekte commandbouwer loopt — de guard hierboven scant alleen
// migration_commands.go, niet de aanroeppunten.
func TestMigrationServiceOnlyStreamsWithKnownSafeBuilders(t *testing.T) {
	raw, err := os.ReadFile("migration_service.go")
	if err != nil {
		t.Fatalf("migration_service.go lezen: %v", err)
	}
	src := zonderGoCommentaar(string(raw))

	veilig := map[string]bool{
		"buildUploadsListCommand":         true,
		"buildUploadsTarCommand":          true,
		"buildUploadsRootFilesTarCommand": true,
	}
	for _, m := range reRunCommandCall.FindAllStringSubmatch(src, -1) {
		if !veilig[m[1]] {
			t.Errorf("RunCommand met %s(...): geen bekende, gedekte commandbouwer", m[1])
		}
	}
	// De pull geeft het commando als variabele door aan DownloadCommand, dus
	// controleer dat die variabele alleen uit de veilige bouwers wordt gevuld.
	for naam := range veilig {
		if !strings.Contains(src, naam+"(") {
			t.Errorf("verwachte bouwer %s wordt niet gebruikt; is de service herschreven zonder de guard bij te werken?", naam)
		}
	}
	if strings.Contains(src, "DownloadCommand(ctx, tgt.SSH, \"") {
		t.Error("DownloadCommand krijgt een letterlijke string als commando; dat omzeilt de gedekte bouwers")
	}
}

func TestBuildUploadsListCommand(t *testing.T) {
	cmd := buildUploadsListCommand("/www/site/public")
	if !strings.Contains(cmd, "wp-content/uploads") {
		t.Errorf("verwacht het uploads-pad, kreeg: %s", cmd)
	}
	if !strings.Contains(cmd, "-maxdepth 1") || !strings.Contains(cmd, "-type d") {
		t.Errorf("verwacht een find op alleen de eerste maplaag, kreeg: %s", cmd)
	}
	if !strings.Contains(cmd, "du -sk") {
		t.Error("verwacht du -sk voor de mapgrootte")
	}
}

func TestParseUploadFolders(t *testing.T) {
	out := strings.Join([]string{
		"wat ruis van een plugin",
		"RDM-DIR:4096\t2025",
		"RDM-DIR:81920\t2026",
		"RDM-DIR:512\tcache",
		"RDM-DIR:2048\tsites/2",
		"RDM-ROOTFILES:3",
	}, "\n")

	folders := parseUploadFolders(out)
	if len(folders) != 4 {
		t.Fatalf("aantal mappen = %d, wil 4: %+v", len(folders), folders)
	}
	if folders[1].Name != "2026" || folders[1].Bytes != 81920*1024 {
		t.Errorf("tweede map = %+v", folders[1])
	}
	if folders[3].Name != "sites/2" {
		t.Errorf("verwacht dat een map met een schuine streep heel blijft, kreeg %q", folders[3].Name)
	}
	if n := parseUploadRootFileCount(out); n != 3 {
		t.Errorf("losse bestanden = %d, wil 3", n)
	}
}

func TestParseUploadFoldersLeegBijGeenUitvoer(t *testing.T) {
	if got := parseUploadFolders("RDM-ERR:geen wp-config.php gevonden"); len(got) != 0 {
		t.Errorf("verwacht geen mappen, kreeg %+v", got)
	}
}

func TestBuildUploadsTarCommandQuoteertMapnaam(t *testing.T) {
	cmd := buildUploadsTarCommand("/www/site/public", "2026")
	if !strings.Contains(cmd, "tar czf - '2026'") {
		t.Errorf("verwacht een gequote mapnaam, kreeg: %s", cmd)
	}
	if !strings.Contains(cmd, "nice -n 19") {
		t.Error("verwacht nice, zodat het inpakken de klantcontainer niet opslokt")
	}

	// Een mapnaam met een aanhalingsteken mag de shell niet kunnen breken.
	raar := buildUploadsTarCommand("/www/site/public", "map'; rm -rf /")
	if strings.Contains(raar, "; rm -rf /'") && !strings.Contains(raar, `'\''`) {
		t.Errorf("mapnaam niet veilig gequote: %s", raar)
	}
}

func TestBuildLocalMultisiteDomainPairsSQL(t *testing.T) {
	sql := buildLocalMultisiteDomainPairsSQL("wp_", []domain.DomainPair{
		{Prod: "vanluyken.nl", Local: "vanluykennl.test"},
		{Prod: "anderdomein.nl", Local: "anderdomein.test"},
	})
	if strings.Count(sql, "UPDATE wp_blogs") != 2 || strings.Count(sql, "UPDATE wp_site") != 2 {
		t.Errorf("verwacht per paar een UPDATE op beide tabellen, kreeg: %s", sql)
	}
	if !strings.Contains(sql, "'anderdomein.nl', 'anderdomein.test'") {
		t.Errorf("het extra paar mist: %s", sql)
	}
}

func TestBuildLocalMultisiteDomainPairsSQLSlaatLegeParenOver(t *testing.T) {
	// Een leeg zoekdomein zou met REPLACE elke rij raken; zulke paren horen
	// niet in de SQL terecht te komen.
	sql := buildLocalMultisiteDomainPairsSQL("wp_", []domain.DomainPair{
		{Prod: "", Local: "lokaal.test"},
		{Prod: "prod.nl", Local: ""},
		{Prod: "  ", Local: "  "},
	})
	if strings.TrimSpace(sql) != "" {
		t.Errorf("verwacht geen SQL voor lege paren, kreeg: %s", sql)
	}
}

func TestBuildLocalMultisiteDomainFixSQLBlijftWerken(t *testing.T) {
	// De oude enkelvoudige vorm is nu een wrapper; die moet identiek gedrag houden.
	sql := buildLocalMultisiteDomainFixSQL("wp_", "vanluyken.nl", "vanluykennl.test")
	if !strings.Contains(sql, "UPDATE wp_blogs SET domain = REPLACE(domain, 'vanluyken.nl', 'vanluykennl.test')") {
		t.Errorf("onverwachte SQL: %s", sql)
	}
}
