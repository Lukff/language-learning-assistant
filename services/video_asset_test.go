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

func TestVideoAssetMiddleware_ServesVideoWithRangeSupport(t *testing.T) {
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

	handler := VideoAssetMiddleware(conn, func() (string, error) { return storageRoot, nil })(http.NotFoundHandler())

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

func TestVideoAssetMiddleware_UnknownLessonReturns404(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	handler := VideoAssetMiddleware(conn, func() (string, error) { return t.TempDir(), nil })(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/media/lesson/999", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, esperado 404", rec.Code)
	}
}

func TestVideoAssetMiddleware_OtherPathsDelegateToNext(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalled = true })
	handler := VideoAssetMiddleware(conn, func() (string, error) { return t.TempDir(), nil })(next)

	req := httptest.NewRequest(http.MethodGet, "/wails/runtime.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("path fora de /media/lesson/ deveria ser delegado a next, mas next não foi chamado")
	}
}
