package services

import (
	"fmt"
	"regexp"
	"strings"
)

// De query-editor voert alles uit wat je typt, maar niet per ongeluk. Deze
// classificatie bepaalt of er eerst een bevestiging komt en of het vangnet
// (een dump) aangaat.
//
// Het uitgangspunt is conservatief: als niet met zekerheid vast te stellen is dat
// een query alleen leest, geldt hij als schrijvend, en bij twijfel over de
// omvang komt er een bevestiging. Een onnodige bevestiging is hinderlijk; een
// gemiste is een verloren tabel.

// SQLSoort is de uitkomst van de beoordeling.
type SQLSoort string

const (
	// SQLLezen verandert niets.
	SQLLezen SQLSoort = "lezen"
	// SQLSchrijven verandert rijen, maar begrensd (er staat een WHERE).
	SQLSchrijven SQLSoort = "schrijven"
	// SQLBevestigen vereist expliciete toestemming.
	SQLBevestigen SQLSoort = "bevestigen"
)

// SQLBeoordeling beschrijft wat een query gaat doen.
type SQLBeoordeling struct {
	Soort SQLSoort `json:"soort"`
	// Reden legt uit waarom er bevestiging nodig is; leeg als dat niet zo is.
	Reden string `json:"reden"`
	// Sleutelwoord is het eerste commando, bv SELECT of DROP.
	Sleutelwoord string `json:"sleutelwoord"`
	// Fout is gevuld als de query hier al niet door kan (bv meerdere statements).
	Fout string `json:"fout"`
}

// Verandert reports whether de query iets aan de data wijzigt.
func (b SQLBeoordeling) Verandert() bool { return b.Soort != SQLLezen }

var (
	// leesCommandos veranderen niets.
	leesCommandos = map[string]bool{
		"SELECT": true, "SHOW": true, "DESCRIBE": true, "DESC": true,
		"EXPLAIN": true, "ANALYZE": true, "CHECK": true, "CHECKSUM": true,
	}
	// begrensdSchrijven mag zonder bevestiging, mits er een WHERE staat.
	begrensdSchrijven = map[string]bool{
		"INSERT": true, "REPLACE": true, "UPDATE": true, "DELETE": true,
	}
	// altijdBevestigen zijn commando's die structuur of rechten raken, of in één
	// keer alles weggooien.
	altijdBevestigen = map[string]string{
		"DROP":     "DROP verwijdert een tabel of database in één keer",
		"TRUNCATE": "TRUNCATE leegt de hele tabel en is niet terug te draaien met een rollback",
		"ALTER":    "ALTER wijzigt de structuur van de tabel",
		"RENAME":   "RENAME hernoemt een tabel",
		"CREATE":   "CREATE maakt een tabel, index of database aan",
		"GRANT":    "GRANT wijzigt rechten",
		"REVOKE":   "REVOKE wijzigt rechten",
		"SET":      "SET verandert serverinstellingen voor deze verbinding",
		"LOCK":     "LOCK zet de tabel op slot",
		"UNLOCK":   "UNLOCK haalt tabelsloten weg",
		"FLUSH":    "FLUSH grijpt in op de server zelf",
		"LOAD":     "LOAD leest een bestand in de tabel",
		"CALL":     "CALL voert een stored procedure uit; wat die doet is hier niet te zien",
		"HANDLER":  "HANDLER omzeilt de normale queryverwerking",
	}

	reEersteWoord = regexp.MustCompile(`^\s*([A-Za-z_]+)`)
	reWhere       = regexp.MustCompile(`(?i)\bWHERE\b`)
	reMutatie     = regexp.MustCompile(`(?i)\b(UPDATE|DELETE|INSERT|REPLACE)\b`)
)

// BeoordeelSQL kijkt wat een query gaat doen.
func BeoordeelSQL(tekst string) SQLBeoordeling {
	kaal := stripCommentaarEnLiteralen(tekst)
	statements := splitsStatements(kaal)

	if len(statements) == 0 {
		return SQLBeoordeling{Soort: SQLLezen, Fout: "geen query ingevuld"}
	}
	if len(statements) > 1 {
		// De driver staat maar één statement per aanroep toe; dat expliciet
		// melden is duidelijker dan MySQL's syntaxfout.
		return SQLBeoordeling{
			Soort: SQLBevestigen,
			Fout:  fmt.Sprintf("er staan %d statements in het venster; voer ze één voor één uit", len(statements)),
		}
	}

	stmt := statements[0]
	m := reEersteWoord.FindStringSubmatch(stmt)
	if m == nil {
		return SQLBeoordeling{Soort: SQLBevestigen, Reden: "de query begint niet met een commando dat ik herken"}
	}
	woord := strings.ToUpper(m[1])
	b := SQLBeoordeling{Sleutelwoord: woord}

	// WITH kan in MySQL 8 vooraf gaan aan een SELECT, maar ook aan een UPDATE of
	// DELETE; dan bepaalt de mutatie het oordeel.
	if woord == "WITH" {
		if !reMutatie.MatchString(stmt) {
			b.Soort = SQLLezen
			return b
		}
		woord = "UPDATE"
		b.Sleutelwoord = "WITH … UPDATE/DELETE"
	}

	if reden, ok := altijdBevestigen[woord]; ok {
		b.Soort = SQLBevestigen
		b.Reden = reden
		return b
	}
	if leesCommandos[woord] {
		b.Soort = SQLLezen
		return b
	}
	if begrensdSchrijven[woord] {
		// INSERT raakt per definitie alleen nieuwe rijen; UPDATE en DELETE zonder
		// WHERE raken de hele tabel.
		if woord == "UPDATE" || woord == "DELETE" {
			if !reWhere.MatchString(stmt) {
				b.Soort = SQLBevestigen
				b.Reden = woord + " zonder WHERE raakt élke rij in de tabel"
				return b
			}
		}
		b.Soort = SQLSchrijven
		return b
	}

	b.Soort = SQLBevestigen
	b.Reden = fmt.Sprintf("%s is een commando dat ik niet kan inschatten", woord)
	return b
}

// stripCommentaarEnLiteralen vervangt commentaar en stringliteralen door
// spaties, zodat een DROP in een tekstwaarde niet als commando telt.
//
// Bewust vervangen door een spatie en niet weglaten: anders kunnen twee woorden
// aan elkaar plakken en verandert de betekenis.
func stripCommentaarEnLiteralen(s string) string {
	var uit strings.Builder
	uit.Grow(len(s))

	const (
		normaal = iota
		inEnkel
		inDubbel
		inBacktick
		inRegelCommentaar
		inBlokCommentaar
	)
	staat := normaal

	for i := 0; i < len(s); i++ {
		c := s[i]
		volgende := byte(0)
		if i+1 < len(s) {
			volgende = s[i+1]
		}

		switch staat {
		case normaal:
			switch {
			case c == '\'':
				staat, _ = inEnkel, uit.WriteByte(' ')
			case c == '"':
				staat, _ = inDubbel, uit.WriteByte(' ')
			case c == '`':
				staat, _ = inBacktick, uit.WriteByte(' ')
			case c == '-' && volgende == '-':
				staat = inRegelCommentaar
				uit.WriteByte(' ')
				i++
			case c == '#':
				staat = inRegelCommentaar
				uit.WriteByte(' ')
			case c == '/' && volgende == '*':
				staat = inBlokCommentaar
				uit.WriteByte(' ')
				i++
			default:
				uit.WriteByte(c)
			}

		case inEnkel, inDubbel:
			sluiter := byte('\'')
			if staat == inDubbel {
				sluiter = '"'
			}
			switch {
			case c == '\\':
				// Backslash-escape: het volgende teken hoort bij de string.
				i++
			case c == sluiter && volgende == sluiter:
				// Verdubbeld aanhalingsteken binnen de string ('' of "").
				i++
			case c == sluiter:
				staat = normaal
				uit.WriteByte(' ')
			}

		case inBacktick:
			if c == '`' && volgende == '`' {
				i++
			} else if c == '`' {
				staat = normaal
				uit.WriteByte(' ')
			}

		case inRegelCommentaar:
			if c == '\n' {
				staat = normaal
				uit.WriteByte('\n')
			}

		case inBlokCommentaar:
			if c == '*' && volgende == '/' {
				staat = normaal
				uit.WriteByte(' ')
				i++
			}
		}
	}
	return uit.String()
}

// splitsStatements deelt op puntkomma's en laat lege delen weg.
func splitsStatements(kaal string) []string {
	var uit []string
	for _, deel := range strings.Split(kaal, ";") {
		if strings.TrimSpace(deel) != "" {
			uit = append(uit, deel)
		}
	}
	return uit
}
