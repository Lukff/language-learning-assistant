package main

import (
	"embed"
	"log"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/services"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	dbPath, err := config.DBPath()
	if err != nil {
		log.Fatalf("resolver caminho do banco: %v", err)
	}
	conn, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("abrir banco de dados: %v", err)
	}
	defer conn.Close()

	app := application.New(application.Options{
		Name:        "Assistente de Idiomas",
		Description: "Arquivo e análise de aulas de inglês do Cambly",
		Services: []application.Service{
			application.NewService(services.NewSetupService()),
			application.NewService(services.NewImportService(conn)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Assistente de Idiomas",
		Width:            1200,
		Height:           760,
		BackgroundColour: application.NewRGB(20, 24, 31), // #14181F — colors.bg
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
