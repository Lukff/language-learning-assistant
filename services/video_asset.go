// services/video_asset.go
package services

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/jobs"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// videoAssetPrefix é o path base do endpoint que serve o .mp4 de uma
// lesson ao webview — resolve o risco técnico 1 do fase-1-mvp.md (vídeo
// local com suporte a range requests) via http.ServeFile da stdlib, que já
// trata Range de graça. Ver
// docs/superpowers/specs/2026-07-22-historia-5-biblioteca-real-design.md.
const videoAssetPrefix = "/media/lesson/"

// VideoAssetMiddleware serve GET /media/lesson/{id} com o vídeo da lesson
// id; qualquer outro path é delegado a next (o AssetServer padrão do
// Wails — embedded em produção, proxy pro dev server em `wails3 dev`,
// confirmado lendo internal/assetserver/build_dev.go da dependência: o
// webview sempre fala com o servidor Go, que só faz proxy pro Vite
// internamente quando FRONTEND_DEVSERVER_URL está setado).
// storageRoot é reavaliado a cada requisição, não uma vez só na criação do
// middleware — mesma razão de internal/jobs.Worker: storage_root só existe
// depois do wizard de primeira execução, que roda depois do app já estar
// de pé.
func VideoAssetMiddleware(conn *sql.DB, storageRoot jobs.StorageRootResolver) application.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			idStr, ok := strings.CutPrefix(r.URL.Path, videoAssetPrefix)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			lesson, err := db.FindLessonByID(conn, id)
			if err != nil || lesson == nil {
				http.NotFound(w, r)
				return
			}
			root, err := storageRoot()
			if err != nil {
				http.Error(w, "storage_root não configurado", http.StatusInternalServerError)
				return
			}
			videoPath := filepath.Join(root, filepath.FromSlash(lesson.VideoPath))
			http.ServeFile(w, r, videoPath)
		})
	}
}
