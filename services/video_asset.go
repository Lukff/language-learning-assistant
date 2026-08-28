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
)

// videoAssetPrefix é o path base do endpoint que serve o .mp4 de uma
// lesson. Servido por um http.Server real em loopback (video_server.go),
// não pelo scheme wails:// do AssetServer: no Linux (WebKitGTK/GStreamer),
// requisições Range por esse scheme chegam ao handler mas o pipeline de
// mídia do webview falha (FormatError) sem nunca pedir o restante do
// arquivo — o mesmo vídeo toca normal fora do app (Chrome/Firefox), então
// não é problema de codec/arquivo. HTTP de loopback de verdade é o caminho
// que o GStreamer sabe streamar nativamente.
const videoAssetPrefix = "/media/lesson/"

// VideoAssetHandler serve GET /media/lesson/{id} com o vídeo da lesson id
// (suporte a Range via http.ServeFile da stdlib, que já trata isso de
// graça); qualquer outro path devolve 404. storageRoot é reavaliado a cada
// requisição, não uma vez só na criação do handler — mesma razão de
// internal/jobs.Worker: storage_root só existe depois do wizard de
// primeira execução, que roda depois do app já estar de pé.
func VideoAssetHandler(conn *sql.DB, storageRoot jobs.StorageRootResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idStr, ok := strings.CutPrefix(r.URL.Path, videoAssetPrefix)
		if !ok {
			http.NotFound(w, r)
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
