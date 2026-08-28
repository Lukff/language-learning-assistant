package services

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"assistente-idiomas/internal/db"
)

func TestVideoAssetHandler_ServesVideoWithRangeSupport(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	storageRoot := t.TempDir()
	content := []byte("conteudo-de-video-fake-para-teste-de-range")
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), content, 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")

	handler := VideoAssetHandler(conn, func() (string, error) { return storageRoot, nil })

	req := httptest.NewRequest(http.MethodGet, "/media/lesson/"+strconv.FormatInt(lessonID, 10), nil)
	req.Header.Set("Range", "bytes=0-4")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, esperado 206 Partial Content", rec.Code)
	}
	if rec.Body.String() != string(content[:5]) {
		t.Errorf("body = %q, esperado os 5 primeiros bytes do vídeo", rec.Body.String())
	}
}

func TestVideoAssetHandler_UnknownLessonReturns404(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	handler := VideoAssetHandler(conn, func() (string, error) { return t.TempDir(), nil })

	req := httptest.NewRequest(http.MethodGet, "/media/lesson/999", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, esperado 404", rec.Code)
	}
}

func TestVideoAssetHandler_OtherPathsReturn404(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	handler := VideoAssetHandler(conn, func() (string, error) { return t.TempDir(), nil })

	req := httptest.NewRequest(http.MethodGet, "/wails/runtime.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, esperado 404 pra path fora de /media/lesson/", rec.Code)
	}
}
