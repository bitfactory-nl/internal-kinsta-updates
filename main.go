package main

import (
	"embed"
	"log"

	"github.com/rdm/sites-tool/internal/app"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	services := app.NewServices(cfg)

	a := application.New(application.Options{
		Name:        "RDM Sites Tool",
		Description: "Git & deployment dashboard for your projects",
		Services:    services.Wails(),
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// Inject app reference so services can open dialogs
	services.Project.SetApp(a)

	a.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "RDM Sites Tool",
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
