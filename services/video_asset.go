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

// videoAssetPrefix is the base path of the endpoint that serves a
// lesson's .mp4. Served by a real http.Server on loopback (video_server.go),
// not by the AssetServer's wails:// scheme: on Linux (WebKitGTK/GStreamer),
// Range requests through that scheme reach the handler but the webview's
// media pipeline fails (FormatError) without ever requesting the rest of the
// file — the same video plays fine outside the app (Chrome/Firefox), so
// it's not a codec/file problem. Real loopback HTTP is the path
// that GStreamer knows how to stream natively.
const videoAssetPrefix = "/media/lesson/"

// VideoAssetHandler serves GET /media/lesson/{id} with lesson id's video
// (Range support via stdlib's http.ServeFile, which already handles that for
// free); any other path returns 404. storageRoot is re-evaluated on every
// request, not just once when the handler is created — same reason as
// internal/jobs.Worker: storage_root only exists after the first-run
// wizard, which runs after the app is already up.
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
			http.Error(w, "storage_root not configured", http.StatusInternalServerError)
			return
		}
		videoPath := filepath.Join(root, filepath.FromSlash(lesson.VideoPath))
		http.ServeFile(w, r, videoPath)
	})
}
