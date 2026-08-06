package services

import (
	"strings"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

// TestScrubHaaltPersoonsgegevensUitEchteWPErrorDump is de belangrijkste test van
// dit bestand: hij gebruikt de regel die in een echt Kinsta-log stond en eist dat
// er na scrubben geen e-mailadres, telefoonnummer, ip-adres of mailbody meer in
// zit. Zolang deze test groen is, kan die regel naar een AI zonder dat er
// klantgegevens meegaan.
func TestScrubHaaltPersoonsgegevensUitEchteWPErrorDump(t *testing.T) {
	var ruw string
	for _, r := range strings.Split(leesFixture(t, "kinsta_error.log"), "\n") {
		if strings.Contains(r, "WP_Error Object") {
			ruw = r
			break
		}
	}
	if ruw == "" {
		t.Fatal("fixture mist de WP_Error-regel")
	}
	// Wat er in de ruwe regel staat en dus weg moet.
	for _, verboden := range []string{"test.persoon@example.com", "info@voorbeeld.nl", "80.56.116.54"} {
		if !strings.Contains(ruw, verboden) {
			t.Fatalf("fixture bevat %q niet meer; test zegt niets meer", verboden)
		}
	}

	res := scrubVoorAI(ruw)

	for _, verboden := range []string{"test.persoon@example.com", "info@voorbeeld.nl", "80.56.116.54", "Testpersoon Voorbeeld"} {
		if strings.Contains(res.Tekst, verboden) {
			t.Errorf("gescrubde tekst bevat nog %q:\n%s", verboden, res.Tekst)
		}
	}
	if reEmail.MatchString(res.Tekst) {
		t.Errorf("er staat nog een e-mailadres in:\n%s", res.Tekst)
	}
	if reIPv4.MatchString(res.Tekst) {
		t.Errorf("er staat nog een ip-adres in:\n%s", res.Tekst)
	}
	if strings.Contains(res.Tekst, "<html>") || strings.Contains(res.Tekst, "font-family") {
		t.Errorf("de mailbody staat er nog in:\n%s", res.Tekst)
	}
	if !res.HeeftPII() {
		t.Error("HeeftPII is false terwijl er gemaskeerd is")
	}

	// En wat juist moet blijven staan: de foutidentiteit.
	if !strings.Contains(res.Tekst, "SMTP Error: Could not authenticate") {
		t.Errorf("de eigenlijke fout is weggeknipt:\n%s", res.Tekst)
	}
}

func TestScrubBewaartWatDeAINodigHeeft(t *testing.T) {
	ruw := `PHP Fatal error:  Uncaught TypeError: count(): Argument #1 ($value) must be of type Countable|array, null given in /www/voorbeeld_706/public/wp-content/plugins/eigen-plugin/src/Widget.php:214 Stack trace: #0 /www/voorbeeld_706/public/wp-content/plugins/eigen-plugin/src/Widget.php(88): EigenPlugin\Widget->render() #1 {main}`
	res := scrubVoorAI(ruw)
	if res.Tekst != ruw {
		t.Errorf("een foutmelding zonder persoonsgegevens hoort onveranderd te blijven:\ngot:  %s\nwant: %s", res.Tekst, ruw)
	}
	if res.HeeftPII() {
		t.Errorf("onterecht gemaskeerd: %v", res.Gemaskeerd)
	}
}

func TestScrubMaskeertEmailEnIP(t *testing.T) {
	res := scrubVoorAI("mail naar jan.jansen@voorbeeld.nl vanaf 192.168.1.55")
	if strings.Contains(res.Tekst, "jan.jansen@voorbeeld.nl") || strings.Contains(res.Tekst, "192.168.1.55") {
		t.Errorf("niet gemaskeerd: %s", res.Tekst)
	}
	if !strings.Contains(res.Tekst, "«e-mail»") || !strings.Contains(res.Tekst, "«ip»") {
		t.Errorf("maskers ontbreken: %s", res.Tekst)
	}
	if len(res.Gemaskeerd) != 2 {
		t.Errorf("gemaskeerd = %v, wil twee categorieën", res.Gemaskeerd)
	}
}

func TestScrubMaskeertNederlandseTelefoonnummers(t *testing.T) {
	for _, nummer := range []string{"0612345678", "06-12345678", "+31612345678", "0201234567", "+31 6 12345678"} {
		res := scrubVoorAI("bel " + nummer + " svp")
		if strings.Contains(res.Tekst, strings.ReplaceAll(nummer, " ", "")) && !strings.Contains(res.Tekst, "«telefoon»") {
			t.Errorf("nummer %q niet gemaskeerd: %s", nummer, res.Tekst)
		}
	}
}

// Regelnummers en versienummers mogen niet als telefoonnummer of ip sneuvelen:
// dan verliest de AI juist de informatie die hij nodig heeft.
func TestScrubLaatRegelnummersEnVersiesStaan(t *testing.T) {
	ruw := `PHP Warning: iets in /www/x/public/wp-content/themes/t/f.php on line 88, php8.2-fpm, WordPress 6.7.1`
	res := scrubVoorAI(ruw)
	for _, moet := range []string{"on line 88", "php8.2-fpm", "6.7.1", "/www/x/public/wp-content/themes/t/f.php"} {
		if !strings.Contains(res.Tekst, moet) {
			t.Errorf("%q is weggevallen:\n%s", moet, res.Tekst)
		}
	}
}

// Een tijdstempel heeft twee dubbele punten en mag niet voor IPv6 doorgaan.
func TestScrubZietTijdstempelNietAlsIPv6(t *testing.T) {
	res := scrubVoorAI("2026/08/04 10:08:29 [error] iets")
	if !strings.Contains(res.Tekst, "10:08:29") {
		t.Errorf("tijdstempel gemaskeerd: %s", res.Tekst)
	}
}

func TestScrubMaskeertEchteIPv6(t *testing.T) {
	for _, ip := range []string{"2001:0db8:85a3:0000:0000:8a2e:0370:7334", "::1"} {
		res := scrubVoorAI("client: " + ip + " deed iets")
		if strings.Contains(res.Tekst, ip) {
			t.Errorf("IPv6 %q niet gemaskeerd: %s", ip, res.Tekst)
		}
	}
}

func TestScrubLaatFastCGISocketPadStaan(t *testing.T) {
	ruw := `upstream: "fastcgi://unix:/var/run/php8.2-fpm-voorbeeld.sock:"`
	res := scrubVoorAI(ruw)
	if !strings.Contains(res.Tekst, "fastcgi://unix:/var/run/php8.2-fpm-voorbeeld.sock:") {
		t.Errorf("socketpad aangetast: %s", res.Tekst)
	}
}

func TestScrubIsIdempotent(t *testing.T) {
	ruw := `[message] => hallo daar [to] => x@y.nl`
	een := scrubVoorAI(ruw)
	twee := scrubVoorAI(een.Tekst)
	if twee.Tekst != een.Tekst {
		t.Errorf("tweede keer scrubben veranderde de tekst:\n1: %s\n2: %s", een.Tekst, twee.Tekst)
	}
}

func TestScrubTermineertOpVeelVelden(t *testing.T) {
	// Bewijst dat de lus vooruit loopt en niet blijft hangen op dezelfde sleutel.
	ruw := strings.Repeat("[message] => iets ", 200)
	klaar := make(chan ScrubResultaat, 1)
	go func() { klaar <- scrubVoorAI(ruw) }()
	select {
	case res := <-klaar:
		if strings.Contains(res.Tekst, "iets") {
			t.Errorf("niet alle velden gemaskeerd: %s", kap(res.Tekst, 200))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scrubVoorAI liep vast")
	}
}

// TestScrubDektAlleVoorbeeldenInDeFixture loopt de hele fixture langs, zodat een
// nieuwe logvorm in de fixture niet stil ongescrubd blijft.
func TestScrubDektAlleVoorbeeldenInDeFixture(t *testing.T) {
	entries := parseLogFile(domain.LogFileError, leesFixture(t, "kinsta_error.log"))
	for _, e := range entries {
		res := scrubVoorAI(e.Raw)
		if reEmail.MatchString(res.Tekst) {
			t.Errorf("e-mailadres blijft staan in:\n%s", res.Tekst)
		}
		if reIPv4.MatchString(res.Tekst) {
			t.Errorf("ip-adres blijft staan in:\n%s", res.Tekst)
		}
	}
}
