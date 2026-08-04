package services

import (
	"fmt"
	"regexp"
	"strings"
)

// reIdentifier is wat een MySQL-tabelnaam mag zijn in onze context. Tabelnamen
// komen uit de inventaris van de server en uit .rdm.yml, dus uit twee bronnen die
// wij niet volledig beheersen; ze worden hier hoe dan ook gevalideerd voordat ze
// in SQL belanden.
var reIdentifier = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// reRolnaam is wat een WordPress-rol mag zijn. Rollen komen uit de UI.
var reRolnaam = regexp.MustCompile(`^[a-z0-9_-]+$`)

func valideerIdentifier(naam string) error {
	if !reIdentifier.MatchString(naam) {
		return fmt.Errorf("ongeldige tabelnaam %q: alleen letters, cijfers en underscores toegestaan", naam)
	}
	return nil
}

// quoteIdent zet een al gevalideerde identifier tussen backticks.
func quoteIdent(naam string) string { return "`" + naam + "`" }

// buildLocalEmptyTablesSQL empties the given tables, keeping their schema so a
// plugin does not fall over a missing table. Runs against the already-imported
// LOCAL database only.
//
// FOREIGN_KEY_CHECKS gaat er even uit: TRUNCATE weigert op een tabel waar een
// andere tabel met een foreign key naar verwijst, en dat komt in
// WooCommerce-schema's voor.
func buildLocalEmptyTablesSQL(tabellen []string) (string, error) {
	if len(tabellen) == 0 {
		return "", nil
	}
	regels := make([]string, 0, len(tabellen)+2)
	regels = append(regels, "SET FOREIGN_KEY_CHECKS=0;")
	for _, t := range tabellen {
		if err := valideerIdentifier(t); err != nil {
			return "", err
		}
		regels = append(regels, "TRUNCATE TABLE "+quoteIdent(t)+";")
	}
	regels = append(regels, "SET FOREIGN_KEY_CHECKS=1;")
	return strings.Join(regels, "\n"), nil
}

// escapeSQLLiteral hergebruikt de escaping van de kloon: backslash eerst, dan
// het aanhalingsteken.
func escapeSQLLiteral(s string) string { return escapeSQLString(s) }

// buildLocalKeptUsersPredicate builds the SQL condition that identifies the
// users who must keep their real data: the ones holding a kept role, plus the
// ones listed by login.
//
// Rollen zitten in usermeta onder een sleutel die op "capabilities" eindigt
// (wp_capabilities, en op multisite ook wp_2_capabilities), met de rolnaam
// tussen aanhalingstekens in de geserialiseerde waarde. Er wordt met RIGHT() en
// LOCATE() gezocht in plaats van met LIKE, zodat een rolnaam met een underscore
// — shop_manager — niet per ongeluk als jokerteken wordt gelezen.
func buildLocalKeptUsersPredicate(usermetaTabel string, keepRoles, keepLogins []string) (string, error) {
	if err := valideerIdentifier(usermetaTabel); err != nil {
		return "", err
	}

	var voorwaarden []string

	var rolChecks []string
	for _, r := range keepRoles {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if !reRolnaam.MatchString(r) {
			return "", fmt.Errorf("ongeldige rolnaam %q", r)
		}
		rolChecks = append(rolChecks, "LOCATE('\""+r+"\"', um.meta_value) > 0")
	}
	if len(rolChecks) > 0 {
		// De subquery zit in een derived table (SELECT ... FROM (...) AS x).
		// Dat is niet cosmetisch: dit predicaat wordt óók gebruikt in een DELETE
		// op usermeta zelf, en MySQL/MariaDB weigeren een statement waarin de
		// doeltabel rechtstreeks in een subquery voorkomt met
		// "ERROR 1093: You can't specify target table for update in FROM clause".
		// Door de wrap wordt de subquery eerst gematerialiseerd en is dat geen
		// probleem meer.
		//
		// user_id IS NOT NULL houdt het predicaat tweewaardig: een NULL in de
		// lijst zou van NOT IN (...) NULL maken, waardoor er stil niemand
		// geanonimiseerd wordt — precies het soort fout dat je niet wilt hebben
		// in code die persoonsgegevens moet verwijderen.
		voorwaarden = append(voorwaarden,
			"u.ID IN (SELECT x.user_id FROM (SELECT DISTINCT um.user_id FROM "+quoteIdent(usermetaTabel)+" um"+
				" WHERE um.user_id IS NOT NULL AND RIGHT(um.meta_key, 12) = 'capabilities'"+
				" AND ("+strings.Join(rolChecks, " OR ")+")) AS x)")
	}

	var logins []string
	for _, l := range keepLogins {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		logins = append(logins, "'"+escapeSQLLiteral(l)+"'")
	}
	if len(logins) > 0 {
		voorwaarden = append(voorwaarden, "u.user_login IN ("+strings.Join(logins, ", ")+")")
	}

	if len(voorwaarden) == 0 {
		// Niemand uitgezonderd: dan is het "kept"-predicaat onwaar voor iedereen.
		return "1=0", nil
	}
	return "(" + strings.Join(voorwaarden, " OR ") + ")", nil
}

// piiUsermetaSleutels zijn de usermeta-velden die rechtstreeks persoonsgegevens
// bevatten. WordPress en WooCommerce vullen deze standaard.
var piiUsermetaSleutels = []string{
	"first_name", "last_name", "nickname", "description",
	"billing_address_index", "shipping_address_index",
	"_customer_billing_address", "_customer_shipping_address",
}

// buildLocalAnonymiseUsersSQL replaces the personal data of every user who is
// not kept: e-mail, login, display name and URL become derived placeholders, and
// the password hash is emptied so the account cannot be used to log in.
//
// De vervangende waarden zijn van het ID afgeleid (gebruiker-42) en dus per rij
// uniek, wat nodig is omdat user_login, user_nicename en user_email een unieke
// index hebben. Botst er toch iets, dan faalt de UPDATE zichtbaar in plaats van
// stil data te verminken.
func buildLocalAnonymiseUsersSQL(usersTabel, usermetaTabel string, keepRoles, keepLogins []string) (string, error) {
	if err := valideerIdentifier(usersTabel); err != nil {
		return "", err
	}
	if err := valideerIdentifier(usermetaTabel); err != nil {
		return "", err
	}
	kept, err := buildLocalKeptUsersPredicate(usermetaTabel, keepRoles, keepLogins)
	if err != nil {
		return "", err
	}

	u := quoteIdent(usersTabel)
	um := quoteIdent(usermetaTabel)

	sleutels := make([]string, 0, len(piiUsermetaSleutels))
	for _, k := range piiUsermetaSleutels {
		sleutels = append(sleutels, "'"+escapeSQLLiteral(k)+"'")
	}

	return strings.Join([]string{
		// 1. De gebruikersrijen zelf.
		"UPDATE " + u + " u SET",
		"  u.user_email = CONCAT('gebruiker-', u.ID, '@voorbeeld.test'),",
		"  u.user_login = CONCAT('gebruiker-', u.ID),",
		"  u.user_nicename = CONCAT('gebruiker-', u.ID),",
		"  u.display_name = CONCAT('Gebruiker ', u.ID),",
		"  u.user_url = '',",
		"  u.user_activation_key = '',",
		"  u.user_pass = ''",
		"WHERE NOT " + kept + ";",
		"",
		// 2. Persoonsgegevens in usermeta van diezelfde gebruikers.
		"DELETE " + um + " FROM " + um + " JOIN " + u + " u ON u.ID = " + um + ".user_id",
		"WHERE NOT " + kept,
		"  AND (" + um + ".meta_key IN (" + strings.Join(sleutels, ", ") + ")",
		"       OR LEFT(" + um + ".meta_key, 8) = 'billing_'",
		"       OR LEFT(" + um + ".meta_key, 9) = 'shipping_');",
		"",
		// 3. Sessies van ALLE gebruikers, ook de bewaarde: een sessietoken uit
		//    productie hoort niet op een ontwikkelmachine te blijven staan.
		"DELETE FROM " + um + " WHERE meta_key = 'session_tokens';",
	}, "\n"), nil
}

// buildLocalAnonymiseCommentsSQL strips the visitor data from comments: name,
// e-mail, website, IP address and browser string. One statement per comments
// table, because multisite has one per site (wp_comments, wp_2_comments).
func buildLocalAnonymiseCommentsSQL(commentTabellen []string) (string, error) {
	if len(commentTabellen) == 0 {
		return "", nil
	}
	regels := make([]string, 0, len(commentTabellen))
	for _, t := range commentTabellen {
		if err := valideerIdentifier(t); err != nil {
			return "", err
		}
		c := quoteIdent(t)
		regels = append(regels, strings.Join([]string{
			"UPDATE " + c + " SET",
			"  comment_author = CONCAT('Bezoeker ', comment_ID),",
			"  comment_author_email = CASE WHEN comment_author_email = '' THEN '' ELSE CONCAT('bezoeker-', comment_ID, '@voorbeeld.test') END,",
			"  comment_author_url = '',",
			"  comment_author_IP = '',",
			"  comment_agent = '';",
		}, "\n"))
	}
	return strings.Join(regels, "\n"), nil
}

// wooOrderPostTypes zijn de post-types waaronder WooCommerce orders opslaat
// zolang HPOS (de aparte wc_orders-tabellen) niet aan staat.
var wooOrderPostTypes = []string{"shop_order", "shop_order_refund", "shop_subscription"}

// wooPIIMetaSleutels zijn de losse ordermeta-velden met persoonsgegevens die
// niet onder de billing_/shipping_-prefix vallen.
var wooPIIMetaSleutels = []string{
	"_customer_ip_address", "_customer_user_agent", "_order_key",
	"_payment_method_title", "_transaction_id",
}

// buildLocalAnonymiseWooLegacyOrdersSQL strips the personal data from
// WooCommerce orders in the OLD storage: as posts in wp_posts with their
// details in wp_postmeta.
//
// Dit is een echt gat als je het overslaat: pas met HPOS staan orders in eigen
// tabellen (wc_orders en verwanten, die de catalogus dekt). Een webshop die nog
// niet gemigreerd is, houdt _billing_email, _billing_address_1, _billing_phone
// en het IP-adres van de klant gewoon in postmeta — daar komt geen enkele
// TRUNCATE aan.
//
// De orders zelf blijven staan; alleen de persoonsgegevens eruit. Zo kun je
// lokaal nog steeds met een gevulde orderlijst werken. Er wordt per site een
// statement gebouwd, want op multisite bestaat elk paar posts/postmeta apart.
func buildLocalAnonymiseWooLegacyOrdersSQL(paren [][2]string) (string, error) {
	if len(paren) == 0 {
		return "", nil
	}

	types := make([]string, 0, len(wooOrderPostTypes))
	for _, t := range wooOrderPostTypes {
		types = append(types, "'"+escapeSQLLiteral(t)+"'")
	}
	sleutels := make([]string, 0, len(wooPIIMetaSleutels))
	for _, k := range wooPIIMetaSleutels {
		sleutels = append(sleutels, "'"+escapeSQLLiteral(k)+"'")
	}

	regels := make([]string, 0, len(paren)*2)
	for _, paar := range paren {
		postsTabel, postmetaTabel := paar[0], paar[1]
		if err := valideerIdentifier(postsTabel); err != nil {
			return "", err
		}
		if err := valideerIdentifier(postmetaTabel); err != nil {
			return "", err
		}
		p := quoteIdent(postsTabel)
		pm := quoteIdent(postmetaTabel)

		// DELETE met een JOIN op een ándere tabel mag wel; alleen de doeltabel in
		// een subquery van zichzelf is verboden (zie buildLocalKeptUsersPredicate).
		regels = append(regels,
			"DELETE "+pm+" FROM "+pm+" JOIN "+p+" p ON p.ID = "+pm+".post_id\n"+
				"WHERE p.post_type IN ("+strings.Join(types, ", ")+")\n"+
				"  AND (LEFT("+pm+".meta_key, 9) = '_billing_'\n"+
				"       OR LEFT("+pm+".meta_key, 10) = '_shipping_'\n"+
				"       OR "+pm+".meta_key IN ("+strings.Join(sleutels, ", ")+"));")

		// De klantnotitie bij een order staat in post_excerpt.
		regels = append(regels,
			"UPDATE "+p+" SET post_excerpt = '' WHERE post_type IN ("+strings.Join(types, ", ")+");")
	}
	return strings.Join(regels, "\n"), nil
}

// beheerEmailSleutels zijn de optienamen waarin WordPress het e-mailadres van de
// beheerder bewaart.
var beheerEmailSleutels = []string{"admin_email", "new_admin_email"}

// buildLocalAnonymiseOptionsSQL replaces the site administrator's e-mail address
// in wp_options. One statement per options table, because multisite has one per
// site.
//
// Dit is een klein maar echt puntje: admin_email is het e-mailadres van een
// persoon bij de klant, en het is bovendien het adres waar een lokale WordPress
// ongevraagd mail naartoe stuurt zodra je iets test.
func buildLocalAnonymiseOptionsSQL(optionsTabellen []string) (string, error) {
	if len(optionsTabellen) == 0 {
		return "", nil
	}
	namen := make([]string, 0, len(beheerEmailSleutels))
	for _, n := range beheerEmailSleutels {
		namen = append(namen, "'"+escapeSQLLiteral(n)+"'")
	}

	regels := make([]string, 0, len(optionsTabellen))
	for _, t := range optionsTabellen {
		if err := valideerIdentifier(t); err != nil {
			return "", err
		}
		regels = append(regels,
			"UPDATE "+quoteIdent(t)+" SET option_value = 'beheer@voorbeeld.test'"+
				" WHERE option_name IN ("+strings.Join(namen, ", ")+") AND option_value <> '';")
	}
	return strings.Join(regels, "\n"), nil
}

// buildLocalCountSQL is een losse telling voor de rapportage na de anonimisatie.
func buildLocalCountSQL(tabel, waar string) (string, error) {
	if err := valideerIdentifier(tabel); err != nil {
		return "", err
	}
	sql := "SELECT COUNT(*) FROM " + quoteIdent(tabel)
	if waar != "" {
		sql += " WHERE " + waar
	}
	return sql + ";", nil
}

// buildLocalKeptUserCountSQL counts the users that keep their real data, for the
// report shown after a clone.
func buildLocalKeptUserCountSQL(usersTabel, usermetaTabel string, keepRoles, keepLogins []string) (string, error) {
	if err := valideerIdentifier(usersTabel); err != nil {
		return "", err
	}
	kept, err := buildLocalKeptUsersPredicate(usermetaTabel, keepRoles, keepLogins)
	if err != nil {
		return "", err
	}
	return "SELECT COUNT(*) FROM " + quoteIdent(usersTabel) + " u WHERE " + kept + ";", nil
}
