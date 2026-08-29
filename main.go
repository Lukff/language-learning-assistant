package main

import (
	"context"
	"database/sql"
	"embed"
	"log"

	"assistente-idiomas/internal/analysis"
	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/jobs"
	"assistente-idiomas/internal/media"
	"assistente-idiomas/internal/stt"
	"assistente-idiomas/services"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
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

	if err := analysis.RegisterPrompts(conn); err != nil {
		log.Fatalf("registrar prompts de análise: %v", err)
	}

	storageRoot := func() (string, error) {
		cfg, err := config.Load()
		if err != nil {
			return "", err
		}
		return cfg.StorageRoot, nil
	}

	analysisProviderFactory := func() (analysis.Provider, error) {
		apiKey, err := config.GetAnalysisAPIKey()
		if err != nil {
			return nil, err
		}
		return analysis.NewDeepSeekProvider(apiKey)
	}

	startJobWorker(conn, storageRoot)

	importService := services.NewImportService(conn)

	videoServer, err := services.NewVideoServerService(conn, storageRoot)
	if err != nil {
		log.Fatalf("subir servidor de vídeo: %v", err)
	}

	app := application.New(application.Options{
		Name:        "Assistente de Idiomas",
		Description: "Arquivo e análise de aulas de inglês do Cambly",
		Services: []application.Service{
			application.NewService(services.NewSetupService()),
			application.NewService(importService),
			application.NewService(services.NewLibraryService(conn, storageRoot)),
			application.NewService(services.NewQueueService(conn)),
			application.NewService(services.NewSettingsService(conn, storageRoot)),
			application.NewService(services.NewAnalysisService(conn, analysisProviderFactory)),
			application.NewService(services.NewTeacherService(conn)),
			application.NewService(services.NewTopicsService(conn)),
			application.NewService(videoServer),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Assistente de Idiomas",
		Width:            1200,
		Height:           760,
		BackgroundColour: application.NewRGB(20, 24, 31), // #14181F — colors.bg
		EnableFileDrop:   true,
		Linux: application.LinuxWindow{
			// Sem isso, o campo Linux fica zero-value e WebviewGpuPolicy
			// resolve pra WebviewGpuPolicyAlways (não WebviewGpuPolicyNever,
			// que só é o default dentro de wails.Run() — não usado aqui, ver
			// application.WebviewGpuPolicy). Decodificação de vídeo via GPU
			// no WebKitGTK quebra com conteúdo servido pelo scheme wails://
			// (https://github.com/wailsapp/wails/issues/2977).
			WebviewGpuPolicy: application.WebviewGpuPolicyNever,
		},
	})
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		importService.DropImport(event.Context().DroppedFiles())
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// startJobWorker inicia o pipeline em background (História 4) numa
// goroutine. storageRoot é resolvido a cada job, não uma vez só aqui — o
// wizard de primeira execução ainda não rodou neste ponto do startup, então
// resolvê-lo antecipadamente falharia sempre na primeira sessão do app (ver
// docs/superpowers/specs/2026-07-22-story-4-pipeline-jobs-design.md).
// Só o cache de áudio (que não depende do wizard) é resolvido aqui; se isso
// falhar, é um problema de disco/permissão e o worker não inicia.
func startJobWorker(conn *sql.DB, storageRoot jobs.StorageRootResolver) {
	audioCacheDir, err := config.AudioCacheDir()
	if err != nil {
		log.Printf("worker de jobs não iniciado: %v", err)
		return
	}
	sttFactory := func() (stt.Provider, error) {
		apiKey, err := config.GetSTTAPIKey()
		if err != nil {
			return nil, err
		}
		return stt.NewElevenLabsProvider(apiKey)
	}
	worker := jobs.NewWorker(conn, storageRoot, audioCacheDir, media.ExtractAudio, sttFactory, services.WailsJobNotifier{})
	go func() {
		if err := worker.Run(context.Background()); err != nil {
			log.Printf("worker de jobs encerrado: %v", err)
		}
	}()
}
