package domain

import "time"

// ActieRow is one row in the "Acties en aandachtspunten" table.
type ActieRow struct {
	Actie string `json:"actie"`
	Wie   string `json:"wie"`
}

// MonitorRow is one row in the "Server, Uptime en TLS-monitoring" table.
type MonitorRow struct {
	Onderdeel string `json:"onderdeel"`
	Status    string `json:"status"`
	Opmerking string `json:"opmerking"`
}

// SoftwareRow is one row in the "Server software & frameworks" table.
type SoftwareRow struct {
	Component      string `json:"component"`
	Huidig         string `json:"huidig"`
	OndersteundTot string `json:"ondersteundTot"`
	Laatste        string `json:"laatste"`
	Opmerking      string `json:"opmerking"`
}

// UpdateRow is one row in the "Managed software-updates" tables (dependency
// managers and WordPress core/plugins share the same shape).
type UpdateRow struct {
	Naam       string `json:"naam"`
	Uitgevoerd string `json:"uitgevoerd"`
	Opmerking  string `json:"opmerking"`
}

// AVGRow is one free-form row in the "AVG check" table.
type AVGRow struct {
	Onderwerp string `json:"onderwerp"`
	Opmerking string `json:"opmerking"`
}

// Report is a per-project, per-period client report ("Servicecontract
// rapportage") mirroring the Bitfactory quarterly-report docx structure.
type Report struct {
	ProjectID         string        `json:"projectId"`
	Period            string        `json:"period"`
	ClientName        string        `json:"clientName"`
	WebsiteName       string        `json:"websiteName"`
	Acties            []ActieRow    `json:"acties"`
	Monitoring        []MonitorRow  `json:"monitoring"`
	Software          []SoftwareRow `json:"software"`
	DependencyUpdates []UpdateRow   `json:"dependencyUpdates"`
	WPUpdates         []UpdateRow   `json:"wpUpdates"`
	AVG               []AVGRow      `json:"avg"`
	UpdatedAt         time.Time     `json:"updatedAt"`
}
