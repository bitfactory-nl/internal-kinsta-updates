package services

import (
	"regexp"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

// avgTabel beschrijft één bekende tabel uit de catalogus.
type avgTabel struct {
	Categorie string
	Reden     string
}

// avgCatalogus koppelt een tabelnaam-zonder-prefix aan wat er in zit. De sleutels
// zijn de namen zoals plugins ze aanmaken; de prefix (wp_, en op multisite ook
// een blog-id als wp_2_) wordt eraf gehaald voor het opzoeken.
//
// Dit is bewust een expliciete lijst en geen slimme heuristiek: bij
// persoonsgegevens wil je kunnen nalezen waaróm iets is aangemerkt, en een
// verkeerde gok is hier duurder dan een gemiste tabel — voor het laatste zijn er
// de patronen in avgPatronen plus de volledige tabellenlijst in de UI.
var avgCatalogus = map[string]avgTabel{
	// ── Formulierinzendingen ────────────────────────────────────────────────
	"wpforms_entries":            {domain.AVGFormulieren, "WPForms-inzendingen met de ingevulde velden"},
	"wpforms_entry_fields":       {domain.AVGFormulieren, "WPForms-veldwaarden per inzending"},
	"wpforms_entry_meta":         {domain.AVGFormulieren, "WPForms-metadata per inzending"},
	"wpforms_payments":           {domain.AVGFormulieren, "WPForms-betalingen met klantgegevens"},
	"wpforms_payment_meta":       {domain.AVGFormulieren, "WPForms-betalingsmetadata"},
	"gf_entry":                   {domain.AVGFormulieren, "Gravity Forms-inzendingen"},
	"gf_entry_meta":              {domain.AVGFormulieren, "Gravity Forms-veldwaarden"},
	"gf_entry_notes":             {domain.AVGFormulieren, "Gravity Forms-notities bij inzendingen"},
	"gf_draft_submissions":       {domain.AVGFormulieren, "Gravity Forms-concepten (halve inzendingen)"},
	"rg_lead":                    {domain.AVGFormulieren, "Gravity Forms-inzendingen (oude opslag)"},
	"rg_lead_detail":             {domain.AVGFormulieren, "Gravity Forms-veldwaarden (oude opslag)"},
	"rg_lead_detail_long":        {domain.AVGFormulieren, "Gravity Forms-lange veldwaarden (oude opslag)"},
	"rg_lead_meta":               {domain.AVGFormulieren, "Gravity Forms-metadata (oude opslag)"},
	"rg_lead_notes":              {domain.AVGFormulieren, "Gravity Forms-notities (oude opslag)"},
	"frm_items":                  {domain.AVGFormulieren, "Formidable-inzendingen"},
	"frm_item_metas":             {domain.AVGFormulieren, "Formidable-veldwaarden"},
	"ninja_forms_subs":           {domain.AVGFormulieren, "Ninja Forms-inzendingen"},
	"ninja_forms_subs_meta":      {domain.AVGFormulieren, "Ninja Forms-veldwaarden"},
	"nf3_submissions":            {domain.AVGFormulieren, "Ninja Forms-inzendingen"},
	"cf7dbplugin_submits":        {domain.AVGFormulieren, "Contact Form 7-inzendingen (CFDB)"},
	"db7_forms":                  {domain.AVGFormulieren, "Contact Form 7-inzendingen (CFDB7)"},
	"cfdb7_forms":                {domain.AVGFormulieren, "Contact Form 7-inzendingen (CFDB7)"},
	"fluentform_submissions":     {domain.AVGFormulieren, "Fluent Forms-inzendingen"},
	"fluentform_entry_details":   {domain.AVGFormulieren, "Fluent Forms-veldwaarden"},
	"fluentform_submission_meta": {domain.AVGFormulieren, "Fluent Forms-metadata"},
	"forminator_form_entry":      {domain.AVGFormulieren, "Forminator-inzendingen"},
	"forminator_form_entry_meta": {domain.AVGFormulieren, "Forminator-veldwaarden"},
	"everest_forms_entries":      {domain.AVGFormulieren, "Everest Forms-inzendingen"},
	"everest_forms_entrymeta":    {domain.AVGFormulieren, "Everest Forms-veldwaarden"},

	// ── Nieuwsbrief en abonnees ─────────────────────────────────────────────
	"mailpoet_subscribers":             {domain.AVGNieuwsbrief, "MailPoet-abonnees met naam en e-mailadres"},
	"mailpoet_subscriber_custom_field": {domain.AVGNieuwsbrief, "MailPoet-extra velden per abonnee"},
	"mailpoet_subscriber_segment":      {domain.AVGNieuwsbrief, "MailPoet-lijstindeling per abonnee"},
	"mailpoet_statistics_clicks":       {domain.AVGNieuwsbrief, "MailPoet-kliks per abonnee"},
	"mailpoet_statistics_opens":        {domain.AVGNieuwsbrief, "MailPoet-opens per abonnee"},
	"mailpoet_statistics_purchases":    {domain.AVGNieuwsbrief, "MailPoet-aankopen per abonnee"},
	"mailpoet_statistics_unsubscribes": {domain.AVGNieuwsbrief, "MailPoet-afmeldingen per abonnee"},
	"mailpoet_sending_queues":          {domain.AVGNieuwsbrief, "MailPoet-verzendwachtrij met ontvangers"},
	"newsletter":                       {domain.AVGNieuwsbrief, "Newsletter-abonnees met naam en e-mailadres"},
	"newsletter_stats":                 {domain.AVGNieuwsbrief, "Newsletter-statistieken per abonnee"},
	"newsletter_user_logs":             {domain.AVGNieuwsbrief, "Newsletter-logboek per abonnee"},

	// ── Webshop (klant- en orderdata) ───────────────────────────────────────
	"wc_orders":                                    {domain.AVGWebshop, "WooCommerce-orders (HPOS) met klantgegevens"},
	"wc_order_addresses":                           {domain.AVGWebshop, "WooCommerce-factuur- en verzendadressen"},
	"wc_orders_meta":                               {domain.AVGWebshop, "WooCommerce-ordermetadata"},
	"wc_order_operational_data":                    {domain.AVGWebshop, "WooCommerce-operationele orderdata"},
	"wc_customer_lookup":                           {domain.AVGWebshop, "WooCommerce-klantenoverzicht met e-mailadressen"},
	"wc_order_stats":                               {domain.AVGWebshop, "WooCommerce-orderstatistieken per klant"},
	"wc_order_product_lookup":                      {domain.AVGWebshop, "WooCommerce-orderregels per klant"},
	"wc_order_coupon_lookup":                       {domain.AVGWebshop, "WooCommerce-couponsgebruik per order"},
	"wc_order_tax_lookup":                          {domain.AVGWebshop, "WooCommerce-belasting per order"},
	"woocommerce_order_items":                      {domain.AVGWebshop, "WooCommerce-orderregels"},
	"woocommerce_order_itemmeta":                   {domain.AVGWebshop, "WooCommerce-orderregelmetadata"},
	"woocommerce_sessions":                         {domain.AVGWebshop, "WooCommerce-sessies met winkelwagens van bezoekers"},
	"woocommerce_downloadable_product_permissions": {domain.AVGWebshop, "WooCommerce-downloadrechten per klant"},
	"wc_download_log":                              {domain.AVGWebshop, "WooCommerce-downloadlogboek met IP-adressen"},
	"wc_reserved_stock":                            {domain.AVGWebshop, "WooCommerce-gereserveerde voorraad per order"},

	// ── Logboeken met IP-adressen en gedrag ─────────────────────────────────
	"wfhits":                  {domain.AVGLogboek, "Wordfence-bezoeklogboek met IP-adressen"},
	"wflogins":                {domain.AVGLogboek, "Wordfence-inlogpogingen met IP-adressen"},
	"wfblocks7":               {domain.AVGLogboek, "Wordfence-blokkades op IP-adres"},
	"wfcrawlers":              {domain.AVGLogboek, "Wordfence-crawlers op IP-adres"},
	"wfnotifications":         {domain.AVGLogboek, "Wordfence-meldingen"},
	"wflivetraffichuman":      {domain.AVGLogboek, "Wordfence-live traffic per bezoeker"},
	"wfls_2fa_secrets":        {domain.AVGLogboek, "Wordfence 2FA-geheimen per gebruiker"},
	"itsec_logs":              {domain.AVGLogboek, "iThemes Security-logboek met IP-adressen"},
	"itsec_lockouts":          {domain.AVGLogboek, "iThemes Security-blokkades op IP-adres"},
	"itsec_temp":              {domain.AVGLogboek, "iThemes Security-tijdelijke IP-registratie"},
	"itsec_fingerprints":      {domain.AVGLogboek, "iThemes Security-apparaatvingerafdrukken"},
	"itsec_dashboard_events":  {domain.AVGLogboek, "iThemes Security-gebeurtenissen"},
	"aiowps_events":           {domain.AVGLogboek, "All In One WP Security-gebeurtenissen met IP-adressen"},
	"aiowps_failed_logins":    {domain.AVGLogboek, "All In One WP Security-mislukte inlogpogingen"},
	"aiowps_login_activity":   {domain.AVGLogboek, "All In One WP Security-inlogactiviteit"},
	"aiowps_login_lockdown":   {domain.AVGLogboek, "All In One WP Security-blokkades"},
	"aiowps_permanent_block":  {domain.AVGLogboek, "All In One WP Security-permanente IP-blokkades"},
	"simple_history":          {domain.AVGLogboek, "Simple History-activiteitenlogboek met gebruikers"},
	"simple_history_contexts": {domain.AVGLogboek, "Simple History-context per gebeurtenis"},
	"statistics_visitor":      {domain.AVGLogboek, "WP Statistics-bezoekers met IP-adressen"},
	"statistics_useronline":   {domain.AVGLogboek, "WP Statistics-bezoekers online"},
	"statistics_search":       {domain.AVGLogboek, "WP Statistics-zoekopdrachten van bezoekers"},
	"redirection_logs":        {domain.AVGLogboek, "Redirection-logboek met IP-adressen"},
	"redirection_404":         {domain.AVGLogboek, "Redirection 404-logboek met IP-adressen"},
	"wpmailsmtp_emails":       {domain.AVGLogboek, "WP Mail SMTP-verzonden e-mail met ontvangers"},
	"wpmailsmtp_email_log":    {domain.AVGLogboek, "WP Mail SMTP-e-maillogboek"},
	"email_log":               {domain.AVGLogboek, "E-maillogboek met ontvangers en inhoud"},
	"actionscheduler_logs":    {domain.AVGLogboek, "Action Scheduler-logboek (kan e-mailadressen in taakargumenten bevatten)"},
}

// avgPatronen vangen tabellen van dezelfde plugin die niet in de lijst staan —
// een plugin-update die er een tabel bij zet, blijft zo toch opvallen.
var avgPatronen = []struct {
	Prefix    string
	Categorie string
	Reden     string
}{
	{"wpforms_", domain.AVGFormulieren, "WPForms-tabel (mogelijk inzendingen)"},
	{"gf_", domain.AVGFormulieren, "Gravity Forms-tabel (mogelijk inzendingen)"},
	{"frm_", domain.AVGFormulieren, "Formidable-tabel (mogelijk inzendingen)"},
	{"fluentform_", domain.AVGFormulieren, "Fluent Forms-tabel (mogelijk inzendingen)"},
	{"forminator_", domain.AVGFormulieren, "Forminator-tabel (mogelijk inzendingen)"},
	{"mailpoet_", domain.AVGNieuwsbrief, "MailPoet-tabel (mogelijk abonneegegevens)"},
	{"wf", domain.AVGLogboek, "Wordfence-tabel (mogelijk IP-adressen)"},
	{"aiowps_", domain.AVGLogboek, "All In One WP Security-tabel (mogelijk IP-adressen)"},
	{"itsec_", domain.AVGLogboek, "iThemes Security-tabel (mogelijk IP-adressen)"},
	{"statistics_", domain.AVGLogboek, "WP Statistics-tabel (mogelijk IP-adressen)"},
}

// reBlogPrefix matcht de blog-id die WordPress op multisite tussen de prefix en
// de tabelnaam zet: wp_2_options.
var reBlogPrefix = regexp.MustCompile(`^\d+_`)

// stripTabelPrefix haalt de installatieprefix en een eventuele multisite-blog-id
// van een tabelnaam af, zodat "wp_2_wpforms_entries" op "wpforms_entries" uitkomt.
func stripTabelPrefix(tabel, prefix string) string {
	rest := tabel
	if prefix != "" && strings.HasPrefix(rest, prefix) {
		rest = rest[len(prefix):]
	}
	return reBlogPrefix.ReplaceAllString(rest, "")
}

// classificeerTabel zegt of een tabel persoonsgegevens bevat, en waarom.
func classificeerTabel(tabel, prefix string) (avgTabel, bool) {
	kern := stripTabelPrefix(tabel, prefix)
	if t, ok := avgCatalogus[kern]; ok {
		return t, true
	}
	for _, p := range avgPatronen {
		if strings.HasPrefix(kern, p.Prefix) {
			return avgTabel{p.Categorie, p.Reden}, true
		}
	}
	return avgTabel{}, false
}

// vindGevoeligeTabellen filters an inventory down to the tables that hold
// personal data, in a stable order so the UI does not shuffle between runs.
func vindGevoeligeTabellen(inventaris []tabelRij, prefix string) []domain.SensitiveTable {
	uit := make([]domain.SensitiveTable, 0, 16)
	for _, rij := range inventaris {
		t, ok := classificeerTabel(rij.Naam, prefix)
		if !ok {
			continue
		}
		uit = append(uit, domain.SensitiveTable{
			Name:     rij.Naam,
			Category: t.Categorie,
			Reason:   t.Reden,
			Rows:     rij.Rijen,
		})
	}
	sorteerGevoelig(uit)
	return uit
}

// sorteerGevoelig ordent op categorie en daarbinnen op naam: voorspelbaar, en de
// grote groepen (formulieren, webshop) staan bij elkaar.
func sorteerGevoelig(rijen []domain.SensitiveTable) {
	orde := map[string]int{
		domain.AVGFormulieren: 0,
		domain.AVGWebshop:     1,
		domain.AVGNieuwsbrief: 2,
		domain.AVGLogboek:     3,
		domain.AVGOverig:      4,
	}
	for i := 1; i < len(rijen); i++ {
		for j := i; j > 0; j-- {
			a, b := rijen[j-1], rijen[j]
			if orde[a.Category] < orde[b.Category] || (a.Category == b.Category && a.Name <= b.Name) {
				break
			}
			rijen[j-1], rijen[j] = b, a
		}
	}
}

// vindTabellenOpSuffix zoekt in de inventaris alle tabellen die op een bepaalde
// kernnaam eindigen — nodig omdat comments en options op multisite per site
// bestaan (wp_comments, wp_2_comments).
func vindTabellenOpSuffix(inventaris []tabelRij, prefix, kern string) []string {
	var uit []string
	for _, rij := range inventaris {
		if stripTabelPrefix(rij.Naam, prefix) == kern {
			uit = append(uit, rij.Naam)
		}
	}
	return uit
}
