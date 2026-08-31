// Package mysqldb praat met een lokale MySQL via een echte driver in plaats van
// via de mysql-CLI.
//
// De reden is veiligheid, niet gemak. Een tabel-editor zet gebruikersdata in
// UPDATE- en INSERT-statements: quotes, backslashes, emoji, NULL versus lege
// string, binaire blobs. Dat met de hand escapen is precies de fout die in dit
// project al eerder is gemaakt (een siteurl die op een backslash eindigde kon uit
// een SQL-literal ontsnappen). Met placeholders gaat waardedata nooit door
// string-escaping heen.
//
// Wat níét als placeholder kan, zijn identifiers — een tabel- of kolomnaam. Die
// worden daarom dubbel afgeschermd: eerst op tekens gevalideerd, en daarna
// gecontroleerd tegen wat information_schema werkelijk teruggeeft. Een naam die
// de frontend verzint komt zo nooit in een query terecht.
package mysqldb

import (
	"fmt"
	"strings"
)

// maxIdentLengte is de MySQL-grens voor tabel- en kolomnamen.
const maxIdentLengte = 64

// ValideerIdent controleert of naam als identifier gebruikt mag worden.
//
// Bewust een witte lijst en geen zwarte: MySQL staat in backticks bijna elk
// teken toe, dus filteren op "gevaarlijke" tekens dekt nooit alles. WordPress-
// en plugintabellen komen met deze tekenset ruim uit.
func ValideerIdent(naam string) error {
	if naam == "" {
		return fmt.Errorf("lege naam")
	}
	if len(naam) > maxIdentLengte {
		return fmt.Errorf("naam %q is langer dan %d tekens", naam, maxIdentLengte)
	}
	for _, r := range naam {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '$' || r == '-':
		default:
			return fmt.Errorf("naam %q bevat een teken dat hier niet mag: %q", naam, r)
		}
	}
	return nil
}

// QuoteIdent zet een gevalideerde naam tussen backticks.
//
// De backtick-verdubbeling is een tweede slot: ValideerIdent laat geen backtick
// door, dus dit kan in de praktijk niets meer doen. Het staat er zodat een
// toekomstige verruiming van de tekenset hier niet stil een injectie oplevert.
func QuoteIdent(naam string) (string, error) {
	if err := ValideerIdent(naam); err != nil {
		return "", err
	}
	return "`" + strings.ReplaceAll(naam, "`", "``") + "`", nil
}

// QuoteIdentPad quotet een reeks namen als `a`.`b`.
func QuoteIdentPad(delen ...string) (string, error) {
	uit := make([]string, 0, len(delen))
	for _, d := range delen {
		q, err := QuoteIdent(d)
		if err != nil {
			return "", err
		}
		uit = append(uit, q)
	}
	return strings.Join(uit, "."), nil
}
