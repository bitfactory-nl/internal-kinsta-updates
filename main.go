package main

import (
	"embed"
	"fmt"
	"log"
	"os"

	"github.com/rdm/sites-tool/internal/app"
	"github.com/rdm/sites-tool/internal/version"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Zonder venster te openen de versie melden: hiermee is te controleren of de
	// build daadwerkelijk een versiestempel heeft meegekregen.
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version.Version)
		return
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	services := app.NewServices(cfg)

	a := application.New(application.Options{
		Name:        "Kinsta Updater",
		Description: "Git & deployment dashboard for your projects",
		Services:    services.Wails(),
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// Inject app reference so services can open dialogs / emit events
	services.Project.SetApp(a)
	services.SSH.SetApp(a)
	services.Report.SetApp(a)
	services.DBClone.SetApp(a)
	services.Migration.SetApp(a)
	services.LogFix.SetApp(a)
	services.OrgSync.SetApp(a)
	services.BulkUpdate.SetApp(a)
	services.Update.SetApp(a)

	// Start the background vulnerability scan loop (no-op if alerts disabled).
	services.VulnScan.Start()

	// Controleer op een nieuwere versie van de tool zelf: kort na het opstarten
	// en daarna elke 6 uur. No-op in dev-builds.
	services.Update.Start()

	a.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Kinsta Updater",
		Width:  1280,
		Height: 800,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		URL: "/",
	})

	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}
