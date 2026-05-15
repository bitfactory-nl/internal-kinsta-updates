package app

import (
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/services"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type Config struct {
	Global config.Global
}

func LoadConfig() (Config, error) {
	g, err := config.LoadGlobal()
	if err != nil {
		return Config{}, err
	}
	return Config{Global: g}, nil
}

type Services struct {
	Project  *services.ProjectService
	Git      *services.GitService
	Editor   *services.EditorService
	Kinsta   *services.KinstaService
	Batch    *services.BatchService
	Notify   *services.NotifyService
	Search   *services.SearchService
	Settings *services.SettingsService
	Make     *services.MakeService
}

func NewServices(cfg Config) *Services {
	project := services.NewProjectService(cfg.Global.ProjectsRoots)
	return &Services{
		Project:  project,
		Git:      services.NewGitService(project),
		Editor:   services.NewEditorService(&cfg.Global),
		Kinsta:   services.NewKinstaService(&cfg.Global, project),
		Batch:    services.NewBatchService(project),
		Notify:   services.NewNotifyService(),
		Search:   services.NewSearchService(project),
		Settings: services.NewSettingsService(&cfg.Global),
		Make:     services.NewMakeService(project),
	}
}

func (s *Services) Wails() []application.Service {
	return []application.Service{
		application.NewService(s.Project),
		application.NewService(s.Git),
		application.NewService(s.Editor),
		application.NewService(s.Kinsta),
		application.NewService(s.Batch),
		application.NewService(s.Notify),
		application.NewService(s.Search),
		application.NewService(s.Settings),
		application.NewService(s.Make),
	}
}
