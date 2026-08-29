// services/video_server.go
package services

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"

	"assistente-idiomas/internal/jobs"
)

// VideoServerService starts a real http.Server on 127.0.0.1 (a free
// port chosen by the OS) just to serve video — see the comment on
// videoAssetPrefix (video_asset.go) about why the AssetServer's wails:// scheme
// isn't enough. The frontend's <video src> points to
// `${BaseURL()}${path}`, no longer to the relative path served by the app.
type VideoServerService struct {
	baseURL string
}

// NewVideoServerService opens the port and starts the server in a goroutine.
// An error here is fatal for the app: without this port no lesson can play video.
func NewVideoServerService(conn *sql.DB, storageRoot jobs.StorageRootResolver) (*VideoServerService, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("open local video server port: %w", err)
	}

	server := &http.Server{Handler: VideoAssetHandler(conn, storageRoot)}
	go server.Serve(ln)

	return &VideoServerService{baseURL: "http://" + ln.Addr().String()}, nil
}

// BaseURL returns the video server's origin (e.g. http://127.0.0.1:54219)
// for the frontend to build a lesson's full URL.
func (s *VideoServerService) BaseURL() string {
	return s.baseURL
}
