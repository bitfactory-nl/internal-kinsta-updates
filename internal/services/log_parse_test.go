package services

import (
	"os"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func leesFixture(t *testing.T, naam string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + naam)
	if err != nil {
		t.Fatalf("fixture %s: %v", naam, err)
	}
	return string(b)
}

func TestParseErrorLogFixture(t *testing.T) {
	entries := parseLogFile(domain.LogFileError, leesFixture(t, "kinsta_error.log"))
	if len(entries) != 11 {
		t.Fatalf("aantal entries = %d, wil 11", len(entries))
	}
	for i, e := range entries {
		if e.Kind == domain.KindUnknown {
			t.Errorf("regel %d werd niet herkend: %s", i, e.Raw)
		}
	}
}

func TestParseErrorLineBotProbe(t *testing.T) {
	regel := `2026/08/04 10:08:29 [error] 5348#5348: *11792 directory index of "/www/voorbeeld_706/public/wp-includes/images/crystal/" is forbidden, client: 51.107.184.196, server: voorbeeld.nl, request: "GET /wp-includes/images/crystal/ HTTP/2.0", host: "voorbeeld.nl:26426"`
	e, ok := parseErrorLine(regel)
	if !ok {
		t.Fatal("regel niet geparseerd")
	}
	if e.Kind != domain.KindBotProbe {
		t.Errorf("kind = %q, wil bot_probe", e.Kind)
	}
	if e.ClientIP != "51.107.184.196" {
		t.Errorf("clientIP = %q", e.ClientIP)
	}
	if e.Request != "GET /wp-includes/images/crystal/ HTTP/2.0" {
		t.Errorf("request = %q", e.Request)
	}
	if e.Level != "error" {
		t.Errorf("level = %q", e.Level)
	}
	if got := e.Time.Format("2006-01-02 15:04:05"); got != "2026-08-04 10:08:29" {
		t.Errorf("time = %q", got)
	}
	// Een bot-probe mag nooit een bestand aanwijzen: dan zou de AI-poort erop
	// kunnen aanslaan.
	if e.File != "" {
		t.Errorf("file = %q, wil leeg voor een bot-probe", e.File)
	}
}

func TestParseErrorLinePHPFatalUitFastCGI(t *testing.T) {
	regel := `2026/08/02 05:22:54 [error] 97649#97649: *211016 FastCGI sent in stderr: "PHP message: PHP Fatal error:  Uncaught Error: Undefined constant "ABSPATH" in /www/voorbeeld_706/web/public/wp-settings.php:34 Stack trace: #0 {main}   thrown in /www/voorbeeld_706/web/public/wp-settings.php on line 34" while reading response header from upstream, client: 135.119.74.228, server: voorbeeld.nl, request: "GET /wp-settings.php HTTP/2.0", upstream: "fastcgi://unix:/var/run/php8.2-fpm-voorbeeld.sock:", host: "voorbeeld.nl:26426"`
	e, ok := parseErrorLine(regel)
	if !ok {
		t.Fatal("regel niet geparseerd")
	}
	if e.Kind != domain.KindPHPFatal {
		t.Fatalf("kind = %q, wil php_fatal", e.Kind)
	}
	if e.File != "/www/voorbeeld_706/web/public/wp-settings.php" {
		t.Errorf("file = %q", e.File)
	}
	if e.Line != 34 {
		t.Errorf("line = %d, wil 34", e.Line)
	}
	// De aangehaalde constante hoort in de melding te blijven staan: dat is de
	// identiteit van de fout.
	if !strings.Contains(e.Message, `Undefined constant "ABSPATH"`) {
		t.Errorf("message = %q", e.Message)
	}
	if strings.Contains(e.Message, "Stack trace") {
		t.Errorf("stacktrace hoort niet in de melding: %q", e.Message)
	}
	if e.Stack != "#0 {main}   thrown in /www/voorbeeld_706/web/public/wp-settings.php on line 34" {
		t.Errorf("stack = %q", e.Stack)
	}
	// De metadata staat achter de payload; die mag niet uit de payload komen.
	if e.ClientIP != "135.119.74.228" || e.Request != "GET /wp-settings.php HTTP/2.0" {
		t.Errorf("metadata verkeerd: ip=%q request=%q", e.ClientIP, e.Request)
	}
}

func TestParseErrorLinePHPWarningGebruiktOnLineVorm(t *testing.T) {
	regel := `2026/08/03 09:14:02 [error] 99731#99731: *2801 FastCGI sent in stderr: "PHP message: PHP Warning:  Undefined array key "listing_price" in /www/voorbeeld_706/public/wp-content/themes/voorbeeld/inc/listing-card.php on line 88" while reading response header from upstream, client: 88.159.12.4, server: voorbeeld.nl, request: "GET /listings/ HTTP/2.0", upstream: "fastcgi://unix:/var/run/php8.2-fpm-voorbeeld.sock:", host: "www.voorbeeld.nl:26426"`
	e, _ := parseErrorLine(regel)
	if e.Kind != domain.KindPHPWarning {
		t.Errorf("kind = %q, wil php_warning", e.Kind)
	}
	if e.File != "/www/voorbeeld_706/public/wp-content/themes/voorbeeld/inc/listing-card.php" || e.Line != 88 {
		t.Errorf("bestand/regel = %q:%d", e.File, e.Line)
	}
}

func TestParseErrorLineDeprecated(t *testing.T) {
	regel := `2026/08/03 11:02:41 [error] 99731#99731: *3120 FastCGI sent in stderr: "PHP message: PHP Deprecated:  strlen(): Passing null to parameter #1 ($string) of type string is deprecated in /www/voorbeeld_706/public/wp-content/plugins/oud-plugin/inc/helper.php on line 12" while reading response header from upstream, client: 88.159.12.4, server: voorbeeld.nl, request: "GET / HTTP/2.0", upstream: "fastcgi://unix:/var/run/php8.2-fpm-voorbeeld.sock:", host: "www.voorbeeld.nl:26426"`
	e, _ := parseErrorLine(regel)
	if e.Kind != domain.KindPHPDeprecated {
		t.Errorf("kind = %q, wil php_deprecated", e.Kind)
	}
	if e.Line != 12 {
		t.Errorf("line = %d, wil 12", e.Line)
	}
}

func TestParseErrorLineFatalMetStacktracePaktDeThrownInRegel(t *testing.T) {
	// De stacktrace noemt meerdere bestanden; het bestand waar de fout opdook
	// (thrown in) is het bestand dat de AI moet krijgen — niet class-wp-hook.php.
	regel := `2026/08/05 14:22:30 [error] 42251#42251: *37990 FastCGI sent in stderr: "PHP message: PHP Fatal error:  Uncaught TypeError: count(): Argument #1 ($value) must be of type Countable|array, null given in /www/voorbeeld_706/public/wp-content/plugins/eigen-plugin/src/Widget.php:214 Stack trace: #0 /www/voorbeeld_706/public/wp-content/plugins/eigen-plugin/src/Widget.php(88): EigenPlugin\Widget->render() #1 /www/voorbeeld_706/public/wp-includes/class-wp-hook.php(324): EigenPlugin\Widget->hook() #2 {main}   thrown in /www/voorbeeld_706/public/wp-content/plugins/eigen-plugin/src/Widget.php on line 214" while reading response header from upstream, client: 88.159.12.4, server: voorbeeld.nl, request: "GET /over-ons/ HTTP/2.0", upstream: "fastcgi://unix:/var/run/php8.2-fpm-voorbeeld.sock:", host: "www.voorbeeld.nl:26426"`
	e, _ := parseErrorLine(regel)
	if e.File != "/www/voorbeeld_706/public/wp-content/plugins/eigen-plugin/src/Widget.php" {
		t.Errorf("file = %q", e.File)
	}
	if e.Line != 214 {
		t.Errorf("line = %d, wil 214", e.Line)
	}
	if !strings.Contains(e.Stack, "class-wp-hook.php") {
		t.Errorf("stack mist context: %q", e.Stack)
	}
}

func TestParseErrorLineWPErrorDumpIsPHPOther(t *testing.T) {
	regels := strings.Split(leesFixture(t, "kinsta_error.log"), "\n")
	var e domain.LogEntry
	for _, r := range regels {
		if strings.Contains(r, "WP_Error Object") {
			e, _ = parseErrorLine(r)
			break
		}
	}
	if e.Kind != domain.KindPHPOther {
		t.Fatalf("kind = %q, wil php_other", e.Kind)
	}
	if !strings.Contains(e.Message, "SMTP Error") {
		t.Errorf("message = %q", e.Message)
	}
}

func TestParseErrorLineOpenFailedIsGeenBotProbe(t *testing.T) {
	// Een ontbrekend bestand kan een echt probleem zijn, dus dit mag niet als
	// ruis worden weggezet.
	regel := `2026/08/05 13:01:12 [error] 42251#42251: *37801 open() "/www/voorbeeld_706/public/robots.txt" failed (2: No such file or directory), client: 74.248.33.65, server: voorbeeld.nl, request: "GET /robots.txt HTTP/2.0", host: "voorbeeld.nl:26426"`
	e, _ := parseErrorLine(regel)
	if e.Kind != domain.KindNginx {
		t.Errorf("kind = %q, wil nginx", e.Kind)
	}
	if !strings.Contains(e.Message, "No such file or directory") {
		t.Errorf("message = %q", e.Message)
	}
	if strings.Contains(e.Message, "client:") {
		t.Errorf("metadata hoort niet in de melding: %q", e.Message)
	}
}

func TestParseErrorLineWeigertOnzin(t *testing.T) {
	for _, regel := range []string{"", "gewoon tekst", "2026/08/05 kapot"} {
		if _, ok := parseErrorLine(regel); ok {
			t.Errorf("regel %q werd onterecht geparseerd", regel)
		}
	}
}

func TestGroepeerBotProbesVallenSamen(t *testing.T) {
	entries := parseLogFile(domain.LogFileError, leesFixture(t, "kinsta_error.log"))
	groepen := groepeerEntries(entries)

	var bot *domain.LogGroup
	for i := range groepen {
		if groepen[i].Kind == domain.KindBotProbe {
			if bot != nil {
				t.Fatalf("meer dan één bot-probe-groep: %q en %q", bot.Title, groepen[i].Title)
			}
			bot = &groepen[i]
		}
	}
	if bot == nil {
		t.Fatal("geen bot-probe-groep gevonden")
	}
	if bot.Count != 3 {
		t.Errorf("bot-probe count = %d, wil 3", bot.Count)
	}
	if !strings.Contains(bot.Title, `"…"`) {
		t.Errorf("titel hoort het pad te generaliseren: %q", bot.Title)
	}
}

func TestGroepeerPHPFoutenGroeperenOpBestandEnRegel(t *testing.T) {
	entries := parseLogFile(domain.LogFileError, leesFixture(t, "kinsta_error.log"))
	groepen := groepeerEntries(entries)

	tellingen := map[string]int{}
	for _, g := range groepen {
		if g.Kind.IsPHP() {
			tellingen[g.Title] += g.Count
		}
	}
	// De twee ABSPATH-fatals en de twee listing_price-warnings horen elk één
	// groep te vormen.
	var abspath, listing int
	for _, g := range groepen {
		switch {
		case strings.Contains(g.Title, "ABSPATH"):
			abspath = g.Count
		case strings.Contains(g.Title, "listing_price"):
			listing = g.Count
		}
	}
	if abspath != 2 {
		t.Errorf("ABSPATH-groep count = %d, wil 2", abspath)
	}
	if listing != 2 {
		t.Errorf("listing_price-groep count = %d, wil 2", listing)
	}
}

func TestGroepeerZetErnstigsteBoven(t *testing.T) {
	entries := parseLogFile(domain.LogFileError, leesFixture(t, "kinsta_error.log"))
	groepen := groepeerEntries(entries)
	if len(groepen) == 0 {
		t.Fatal("geen groepen")
	}
	if groepen[0].Kind != domain.KindPHPFatal {
		t.Errorf("eerste groep = %q, wil php_fatal bovenaan ondanks dat botruis vaker voorkomt", groepen[0].Kind)
	}
	if groepen[len(groepen)-1].Kind != domain.KindBotProbe {
		t.Errorf("laatste groep = %q, wil bot_probe onderaan", groepen[len(groepen)-1].Kind)
	}
}

func TestGroepeerVultEerstEnLaatst(t *testing.T) {
	entries := parseLogFile(domain.LogFileError, leesFixture(t, "kinsta_error.log"))
	for _, g := range groepeerEntries(entries) {
		if g.First.IsZero() || g.Last.IsZero() {
			t.Errorf("groep %q mist eerst/laatst", g.Title)
		}
		if g.Last.Before(g.First) {
			t.Errorf("groep %q: laatst (%v) ligt voor eerst (%v)", g.Title, g.Last, g.First)
		}
		if len(g.Samples) == 0 || len(g.Samples) > maxVoorbeelden {
			t.Errorf("groep %q heeft %d voorbeelden", g.Title, len(g.Samples))
		}
	}
}

func TestVingerafdrukIsStabielEnHex(t *testing.T) {
	a := vingerafdruk("php_fatal|/x.php|3|iets")
	b := vingerafdruk("php_fatal|/x.php|3|iets")
	if a != b {
		t.Errorf("niet stabiel: %q vs %q", a, b)
	}
	if a == vingerafdruk("php_fatal|/x.php|4|iets") {
		t.Error("verschillende sleutels leveren dezelfde vingerafdruk")
	}
	// De vingerafdruk wordt in een branchnaam gebruikt, dus alleen hex.
	for _, r := range a {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("vingerafdruk %q bevat een niet-hex teken %q", a, r)
		}
	}
}

func TestParseAccessLog(t *testing.T) {
	regel := `www.voorbeeld.nl 85.17.146.126 [06/Aug/2026:07:12:08 +0000] GET "/listing-category-sitemap.xml" HTTP/2.0 200 "https://voorbeeld.nl/sitemap_index.xml" "Mozilla/5.0" 85.17.146.126 "/index.php" - - 1496 0.139 0.140`
	e, ok := parseAccessLine(regel)
	if !ok {
		t.Fatal("access-regel niet geparseerd")
	}
	if e.Kind != domain.KindAccess {
		t.Errorf("kind = %q", e.Kind)
	}
	if e.Host != "www.voorbeeld.nl" || e.ClientIP != "85.17.146.126" {
		t.Errorf("host=%q ip=%q", e.Host, e.ClientIP)
	}
	if !strings.Contains(e.Request, "/listing-category-sitemap.xml") {
		t.Errorf("request = %q", e.Request)
	}
	if got := e.Time.Format("2006-01-02 15:04:05"); got != "2026-08-06 07:12:08" {
		t.Errorf("time = %q", got)
	}
}

func TestParseCachePerfLog(t *testing.T) {
	regel := `[06/Aug/2026:07:12:08 +0000] BYPASS KINSTAWP 85.17.146.126 GET "/listing-category-sitemap.xml" HTTP/2.0 200 200 0 0.140`
	e, ok := parseCachePerfLine(regel)
	if !ok {
		t.Fatal("cache-perf-regel niet geparseerd")
	}
	if e.ClientIP != "85.17.146.126" {
		t.Errorf("ip = %q", e.ClientIP)
	}
	if !strings.Contains(e.Message, "BYPASS") {
		t.Errorf("message mist cachestatus: %q", e.Message)
	}
}

func TestKapBlijftGeldigUTF8(t *testing.T) {
	s := strings.Repeat("é", 50)
	uit := kap(s, 11)
	if !utf8Valid(uit) {
		t.Fatalf("kap leverde ongeldige UTF-8: %q", uit)
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
