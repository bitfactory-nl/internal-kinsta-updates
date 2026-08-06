package services

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Logregels bevatten persoonsgegevens. Dat is geen theoretisch risico: in een
// echt error.log stond een WP_Error-dump van een mislukte formuliermail, met de
// naam van de inzender, het e-mailadres én de volledige mailbody erin. Zodra die
// tekst naar een AI gaat, is dat een doorgifte van klantpersoonsgegevens aan een
// derde partij. Alles wat deze tool naar buiten stuurt gaat daarom eerst hier
// langs.
//
// De aanpak is bewust tweeledig: eerst de velden waar vrije tekst in zit er in
// z'n geheel uit knippen (want een naam is met een patroon niet te vinden),
// daarna de vormen die wél herkenbaar zijn maskeren.

var (
	reEmail = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	reIPv4  = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// Minstens drie dubbele punten, zodat een tijdstempel als 10:08:29 niet
	// als IPv6-adres wordt aangezien.
	reIPv6 = regexp.MustCompile(`\b(?:[0-9a-fA-F]{1,4}:){3,7}[0-9a-fA-F]{1,4}\b|::1\b|::ffff:[0-9.]+`)
	// De woordgrens hoort alleen bij de 0-variant: vóór een `+` staat nooit een
	// woordgrens, dus `\b\+31` matcht niets.
	reTelefoonNL = regexp.MustCompile(`(?:\+31[\s\-]?\(?0?\)?|\b0)(?:6[\s\-]?\d{8}|[1-9]\d[\s\-]?\d{7}|[1-9]\d{2}[\s\-]?\d{6})\b`)

	// reVrijTekstVeld vindt een print_r-sleutel waar vrije, door een bezoeker
	// ingevulde tekst achter staat.
	reVrijTekstVeld = regexp.MustCompile(`(?i)\[(message|body|content|post_content|comment_content|subject|to|from|cc|bcc|reply-to|name|naam|voornaam|achternaam|email|e-mail|phone|telefoon|address|adres|postcode|iban|bsn)\]\s*=>`)
)

// afkapMarkeringen zijn punten in een dump waarvan we weten dat er vanaf daar
// alleen nog gebruikersgegevens komen. WP_Error zet alle context onder
// error_data; die hele tak kan weg zonder de foutidentiteit te verliezen.
var afkapMarkeringen = []string{"[error_data]", "[_POST]", "[_REQUEST]", "[$_POST]"}

const weggelaten = "«weggelaten (AVG)»"

// ScrubResultaat is gescrubde tekst plus wat eruit gehaald is, zodat de UI kan
// laten zien dat er iets gemaskeerd is in plaats van het stil te doen.
type ScrubResultaat struct {
	Tekst      string
	Gemaskeerd []string
}

// HeeftPII reports whether anything was masked.
func (r ScrubResultaat) HeeftPII() bool { return len(r.Gemaskeerd) > 0 }

// scrubVoorAI removes personal data from a log line while keeping what an AI
// needs to diagnose the fault: the error text, the file path, the line number
// and the stack trace.
func scrubVoorAI(s string) ScrubResultaat {
	tellingen := map[string]int{}

	// 1. Hele takken van een dump die alleen bezoekersgegevens bevatten.
	for _, marker := range afkapMarkeringen {
		if i := strings.Index(s, marker); i >= 0 {
			s = strings.TrimSpace(s[:i]) + " " + marker + " " + weggelaten
			tellingen["dump met bezoekersgegevens"]++
			break
		}
	}

	// 2. Vrije-tekstvelden: alles achter de sleutel tot de volgende sleutel.
	s = vervangVrijeTekstVelden(s, tellingen)

	// 3. Vormen die met een patroon te vinden zijn.
	for _, stap := range []struct {
		re     *regexp.Regexp
		label  string
		masker string
	}{
		{reEmail, "e-mailadres", "«e-mail»"},
		{reTelefoonNL, "telefoonnummer", "«telefoon»"},
		{reIPv6, "ip-adres", "«ip»"},
		{reIPv4, "ip-adres", "«ip»"},
	} {
		s = stap.re.ReplaceAllStringFunc(s, func(string) string {
			tellingen[stap.label]++
			return stap.masker
		})
	}

	return ScrubResultaat{Tekst: strings.TrimSpace(s), Gemaskeerd: samenvatting(tellingen)}
}

// vervangVrijeTekstVelden replaces the value behind a known free-text key with a
// placeholder, up to the next print_r key or the end of the string. Het loopt de
// tekst één keer van voor naar achter door: een variant die na elke vervanging
// opnieuw vanaf het begin zoekt, vindt de sleutel die hij net verwerkte weer.
func vervangVrijeTekstVelden(s string, tellingen map[string]int) string {
	var b strings.Builder
	offset := 0
	for offset < len(s) {
		loc := reVrijTekstVeld.FindStringIndex(s[offset:])
		if loc == nil {
			b.WriteString(s[offset:])
			return b.String()
		}
		sleutelEind := offset + loc[1]
		b.WriteString(s[offset:sleutelEind])

		rest := s[sleutelEind:]
		eind := len(rest)
		if next := reVolgendeSleutel.FindStringIndex(rest); next != nil {
			eind = next[0]
		}
		if waarde := strings.TrimSpace(rest[:eind]); waarde != "" && waarde != weggelaten {
			tellingen["ingevuld formulierveld"]++
			b.WriteString(" " + weggelaten)
		} else {
			b.WriteString(rest[:eind])
		}
		offset = sleutelEind + eind
	}
	return b.String()
}

// reVolgendeSleutel vindt het begin van de volgende print_r-sleutel.
var reVolgendeSleutel = regexp.MustCompile(`\[[^\[\]]{1,40}\]\s*=>`)

func samenvatting(tellingen map[string]int) []string {
	if len(tellingen) == 0 {
		return nil
	}
	uit := make([]string, 0, len(tellingen))
	for label, n := range tellingen {
		if n == 1 {
			uit = append(uit, label)
			continue
		}
		uit = append(uit, fmt.Sprintf("%s (%d×)", label, n))
	}
	sort.Strings(uit)
	return uit
}
