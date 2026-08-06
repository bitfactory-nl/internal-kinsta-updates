package app

import (
	"github.com/rdm/sites-tool/internal/adapters/browser"
	"github.com/rdm/sites-tool/internal/adapters/endoflife"
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
	Plugin   *services.PluginService
	SSH      *services.SSHService
	VulnScan *services.VulnScanService
	Security *services.SecurityService
	Test     *services.TestService
	Report   *services.ReportService

	Wordfence       *services.WordfenceService
	WordfenceUpdate *services.WordfenceUpdateService
	Inventory       *services.InventoryService
	WPCoreUpdate    *services.WPCoreUpdateService
	Media           *services.MediaService
	DBClone         *services.DBCloneService
	Migration       *services.MigrationService
	Logs            *services.LogService
	LogFix          *services.LogFixService
}

func NewServices(cfg Config) *Services {
	project := services.NewProjectService(cfg.Global.ProjectsRoots)
	kinsta := services.NewKinstaService(&cfg.Global, project)
	notify := services.NewNotifyService()
	security := services.NewSecurityService(&cfg.Global, project)

	runner := browser.NewRunner(services.SidecarScriptPath())
	runStore := services.NewRunStore(services.DefaultRunHistoryDir())
	testSvc := services.NewTestService(project, &cfg.Global, runStore, runner)

	pdfRunner := browser.NewPDFRunner(services.PDFScriptPath())
	reportStore := services.NewReportStore(services.DefaultReportsDir())
	reportSvc := services.NewReportService(project, kinsta, security, reportStore, pdfRunner, endoflife.NewClient(), services.GitRepoFiles{})

	git := services.NewGitService(project)
	logs := services.NewLogService(project, kinsta)
	wordfence := services.NewWordfenceService(&cfg.Global, project)
	wordfenceUpdate := services.NewWordfenceUpdateService(git, project)

	return &Services{
		Project:  project,
		Git:      git,
		Editor:   services.NewEditorService(&cfg.Global),
		Kinsta:   kinsta,
		Batch:    services.NewBatchService(project),
		Notify:   notify,
		Search:   services.NewSearchService(project),
		Settings: services.NewSettingsService(&cfg.Global),
		Make:     services.NewMakeService(project),
		Plugin:   services.NewPluginService(&cfg.Global, kinsta, project, git),
		SSH:      services.NewSSHService(),
		VulnScan: services.NewVulnScanService(&cfg.Global, project, kinsta, notify),
		Security: security,
		Test:     testSvc,
		Report:   reportSvc,

		Wordfence:       wordfence,
		WordfenceUpdate: wordfenceUpdate,
		Inventory:       services.NewInventoryService(project, &cfg.Global),
		WPCoreUpdate:    services.NewWPCoreUpdateService(project, &cfg.Global),
		Media:           services.NewMediaService(project, kinsta, services.NewMediaScanStore(services.DefaultMediaScanDir())),
		DBClone:         services.NewDBCloneService(project, kinsta),
		Migration:       services.NewMigrationService(project, kinsta),
		Logs:            logs,
		LogFix:          services.NewLogFixService(project, logs, &cfg.Global),
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
		application.NewService(s.Plugin),
		application.NewService(s.SSH),
		application.NewService(s.VulnScan),
		application.NewService(s.Security),
		application.NewService(s.Test),
		application.NewService(s.Report),
		application.NewService(s.Wordfence),
		application.NewService(s.WordfenceUpdate),
		application.NewService(s.Inventory),
		application.NewService(s.WPCoreUpdate),
		application.NewService(s.Media),
		application.NewService(s.DBClone),
		application.NewService(s.Migration),
		application.NewService(s.Logs),
		application.NewService(s.LogFix),
	}
}
