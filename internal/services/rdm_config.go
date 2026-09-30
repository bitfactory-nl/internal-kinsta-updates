package services

import (
	"fmt"

	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// bewaarRdmConfig schrijft cfg naar .rdm/config.yml, maar het kinsta-blok komt
// van schijf. In het geheugen kan dat blok de koppeling van de WordPress-site
// bevatten (zie projectForSite); die hoort in wp-sites.yml, niet in de repo van
// de klant. Anders zou "geen Kinsta-site" bij de site later niet meer werken,
// omdat .rdm dan de oude koppeling teruggeeft. zetKinsta (mag nil zijn) past het
// kinsta-blok van schijf bewust aan, zoals KinstaService.LinkSite doet.
func bewaarRdmConfig(path string, cfg domain.ProjectConfig, zetKinsta func(*domain.KinstaProjectCfg)) error {
	disk, err := config.LoadProject(path)
	if err != nil {
		return fmt.Errorf("lees .rdm/config.yml: %w", err)
	}
	uit := cfg
	uit.Kinsta = nil
	if disk.Kinsta != nil || zetKinsta != nil {
		k := domain.KinstaProjectCfg{}
		if disk.Kinsta != nil {
			k = *disk.Kinsta
		}
		if zetKinsta != nil {
			zetKinsta(&k)
		}
		uit.Kinsta = &k
	}
	return config.SaveProject(path, uit)
}
