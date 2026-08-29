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
		log.Fatalf("resolve database path: %v", err)
	}
	conn, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	if err := analysis.RegisterPrompts(conn); err != nil {
		log.Fatalf("register analysis prompts: %v", err)
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
		log.Fatalf("start video server: %v", err)
	}

	app := application.New(application.Options{
		Name:        "Language Assistant",
		Description: "Archive and analysis of English lessons from Cambly",
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
		Title:            "Language Assistant",
		Width:            1200,
		Height:           760,
		BackgroundColour: application.NewRGB(20, 24, 31), // #14181F — colors.bg
		EnableFileDrop:   true,
		Linux: application.LinuxWindow{
			// Without this, the Linux field stays zero-value and WebviewGpuPolicy
			// resolves to WebviewGpuPolicyAlways (not WebviewGpuPolicyNever,
			// which is only the default inside wails.Run() — not used here, see
			// application.WebviewGpuPolicy). GPU video decoding in WebKitGTK
			// breaks with content served by the wails:// scheme
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

// startJobWorker starts the background pipeline (Story 4) in a
// goroutine. storageRoot is resolved on each job, not just once here — the
// first-run wizard hasn't run yet at this point in startup, so resolving
// it eagerly would always fail on the app's first session (see
// docs/superpowers/specs/2026-07-22-story-4-pipeline-jobs-design.md).
// Only the audio cache (which doesn't depend on the wizard) is resolved here; if that
// fails, it's a disk/permission problem and the worker doesn't start.
func startJobWorker(conn *sql.DB, storageRoot jobs.StorageRootResolver) {
	audioCacheDir, err := config.AudioCacheDir()
	if err != nil {
		log.Printf("job worker not started: %v", err)
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
			log.Printf("job worker stopped: %v", err)
		}
	}()
}
