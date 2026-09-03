package services

import (
	"log"
	"os"
	"time"
)

const (
	// updateCheckInterval is 6 uur: vier controles per etmaal terwijl de app
	// open staat.
	updateCheckInterval = 6 * time.Hour

	// initialUpdateCheckDelay houdt de eerste controle uit het opstartpad: de
	// app moet eerst zichtbaar en bruikbaar zijn, en pas daarna het netwerk op.
	initialUpdateCheckDelay = 20 * time.Second
)

// Start begint de achtergrondloop en ruimt een achtergebleven back-upbundle op.
// No-op wanneer de loop al draait, in een dev-build, of als automatisch
// controleren uit staat.
func (s *UpdateService) Start() {
	s.cleanupBackupBundle()

	if !s.enabled() || !s.cfg.Updates.AutoCheckEnabled() {
		return
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stop = make(chan struct{})
	stop := s.stop
	initial := s.initialDelay
	interval := s.interval
	s.mu.Unlock()

	if initial <= 0 {
		initial = initialUpdateCheckDelay
	}
	if interval <= 0 {
		interval = updateCheckInterval
	}

	go func() {
		timer := time.NewTimer(initial)
		defer timer.Stop()
		for {
			select {
			case <-stop:
				return
			case <-timer.C:
				// De toggle wordt bij elke ronde opnieuw gelezen, zodat hem
				// uitzetten in Instellingen direct werkt zonder herstart.
				if s.cfg.Updates.AutoCheckEnabled() {
					if _, err := s.Check(); err != nil {
						log.Printf("update-check: %v", err)
					}
				}
				timer.Reset(interval)
			}
		}
	}()
}

// Stop halts the background check loop.
func (s *UpdateService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		close(s.stop)
		s.running = false
	}
}

// cleanupBackupBundle verwijdert de <naam>.app.bak die het update-script
// achterlaat. Dat dit lukt is tegelijk het bewijs dat de nieuwe build start:
// blijft de back-up staan, dan is er nog een werkende versie terug te zetten.
func (s *UpdateService) cleanupBackupBundle() {
	if s.bundlePath == "" {
		return
	}
	bak := s.bundlePath + ".bak"
	if _, err := os.Stat(bak); err != nil {
		return
	}
	if err := os.RemoveAll(bak); err != nil {
		log.Printf("oude app-back-up opruimen: %v", err)
		return
	}
	log.Printf("oude app-back-up opgeruimd: %s", bak)
}
