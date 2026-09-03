package domain

import "time"

// Soorten wijzigingen zoals ze in de release-notes gegroepeerd staan.
const (
	ChangeNieuw    = "nieuw"
	ChangeOpgelost = "opgelost"
	ChangeOverig   = "overig"
)

// Fasen van een installatie, gebruikt in UpdateProgress.Phase.
const (
	PhaseDownload  = "download"
	PhaseUitpakken = "uitpakken"
	PhaseVervangen = "vervangen"
)

// ChangeEntry is één regel uit de release-notes.
type ChangeEntry struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// AvailableUpdate beschrijft een release die nieuwer is dan de draaiende versie.
type AvailableUpdate struct {
	Version   string        `json:"version"`
	Changes   []ChangeEntry `json:"changes"`
	Skipped   bool          `json:"skipped"`
	SizeBytes int64         `json:"sizeBytes"`
}

// UpdateStatus is wat de frontend nodig heeft om de badge, de popup en de
// sectie in Instellingen te tonen.
type UpdateStatus struct {
	CurrentVersion string           `json:"currentVersion"`
	Enabled        bool             `json:"enabled"`
	AutoCheck      bool             `json:"autoCheck"`
	LastCheck      time.Time        `json:"lastCheck"`
	LastError      string           `json:"lastError"`
	Available      *AvailableUpdate `json:"available"`
}

// UpdateProgress wordt tijdens het installeren naar de frontend gestuurd.
// Total is 0 wanneer de omvang van een fase niet bekend is.
type UpdateProgress struct {
	Phase string `json:"phase"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}
