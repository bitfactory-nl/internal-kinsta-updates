package domain

import "time"

// OrgSyncRepo is één repository uit de GitHub-organisatie, verrijkt met de
// classificatie uit deploy_conf.json en (indien aanwezig) de match met een
// lokale checkout.
type OrgSyncRepo struct {
	Name          string `json:"name"`
	FullName      string `json:"fullName"`
	HTMLURL       string `json:"htmlUrl"`
	DeployType    string `json:"deployType"`
	IsWordPress   bool   `json:"isWordPress"`
	Archived      bool   `json:"archived"`
	Fork          bool   `json:"fork"`
	HasDeployConf bool   `json:"hasDeployConf"`
	PushedAt      string `json:"pushedAt"`
	// LocalProjectID/LocalPath zijn leeg wanneer de repo niet lokaal is uitgecheckt.
	LocalProjectID string `json:"localProjectId"`
	LocalPath      string `json:"localPath"`
}

// OrgSyncLocalOnly is een lokale checkout waarvan de remote naar de org wijst,
// maar waarvan de repo niet (meer) in de org-repolijst voorkomt (bijv.
// hernoemd, verwijderd, of buiten de org verplaatst).
type OrgSyncLocalOnly struct {
	ProjectID   string `json:"projectId"`
	DisplayName string `json:"displayName"`
	Path        string `json:"path"`
	Remote      string `json:"remote"`
}

// OrgSyncTotals vat OrgSyncResult samen voor de UI.
type OrgSyncTotals struct {
	Repos            int `json:"repos"`
	WordPress        int `json:"wordpress"`
	WordPressLocal   int `json:"wordpressLocal"`
	WordPressMissing int `json:"wordpressMissing"`
	Archived         int `json:"archived"`
}

// OrgSyncResult is het resultaat van één organisatie-sync.
type OrgSyncResult struct {
	Org       string             `json:"org"`
	FetchedAt time.Time          `json:"fetchedAt"`
	Repos     []OrgSyncRepo      `json:"repos"`
	LocalOnly []OrgSyncLocalOnly `json:"localOnly"`
	Totals    OrgSyncTotals      `json:"totals"`
	// Scanned is het aantal repos waarvoor deploy_conf.json daadwerkelijk is
	// opgehaald tijdens deze sync; FromCache is het aantal dat ongewijzigd bleef.
	Scanned   int      `json:"scanned"`
	FromCache int      `json:"fromCache"`
	Warnings  []string `json:"warnings"`
}

// OrgSyncProgress is het voortgangsevent tijdens een sync.
type OrgSyncProgress struct {
	Phase string `json:"phase"`
	Repo  string `json:"repo"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

// Clone-statussen van één repo in een OrgCloneResult.
const (
	OrgCloneCloned  = "cloned"
	OrgCloneSkipped = "skipped"
	OrgCloneFailed  = "failed"
)

// OrgCloneOutcome is de uitkomst van één poging om een org-repo lokaal te
// clonen. Message is leeg bij OrgCloneCloned en bevat bij skipped/failed de
// reden (bestaande map, git-stderr, ...).
type OrgCloneOutcome struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// OrgCloneResult vat een clone-run samen. Eén mislukte repo laat de rest van de
// run doorgaan, dus Failed > 0 betekent niet dat er niets is gelukt.
type OrgCloneResult struct {
	Root     string            `json:"root"`
	Outcomes []OrgCloneOutcome `json:"outcomes"`
	Cloned   int               `json:"cloned"`
	Skipped  int               `json:"skipped"`
	Failed   int               `json:"failed"`
}

// OrgCloneProgress is het voortgangsevent tijdens een clone-run. Anders dan bij
// een sync komt dit per repo: een clone duurt seconden tot minuten, dus elke
// stap is er één die de gebruiker wil zien.
type OrgCloneProgress struct {
	Phase string `json:"phase"`
	Repo  string `json:"repo"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}
