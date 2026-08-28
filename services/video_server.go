// services/video_server.go
package services

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"

	"assistente-idiomas/internal/jobs"
)

// VideoServerService sobe um http.Server de verdade em 127.0.0.1 (porta
// livre escolhida pelo SO) só pra servir vídeo — ver o comentário em
// videoAssetPrefix (video_asset.go) sobre por que o scheme wails:// do
// AssetServer não basta. O <video src> do frontend aponta pra
// `${BaseURL()}${caminho}`, não mais pro path relativo servido pelo app.
type VideoServerService struct {
	baseURL string
}

// NewVideoServerService abre a porta e sobe o servidor numa goroutine.
// Erro aqui é fatal pro app: sem essa porta nenhuma lesson toca vídeo.
func NewVideoServerService(conn *sql.DB, storageRoot jobs.StorageRootResolver) (*VideoServerService, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("abrir porta local do servidor de vídeo: %w", err)
	}

	server := &http.Server{Handler: VideoAssetHandler(conn, storageRoot)}
	go server.Serve(ln)

	return &VideoServerService{baseURL: "http://" + ln.Addr().String()}, nil
}

// BaseURL devolve a origem do servidor de vídeo (ex.: http://127.0.0.1:54219)
// pro frontend montar a URL completa de uma lesson.
func (s *VideoServerService) BaseURL() string {
	return s.baseURL
}
