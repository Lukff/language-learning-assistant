// internal/config/config_test.go
package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAppDataDir_CreatesAndReturnsPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir, err := AppDataDir()
	if err != nil {
		t.Fatalf("AppDataDir() erro inesperado: %v", err)
	}
	if filepath.Base(dir) != appDirName {
		t.Errorf("dir = %q, esperado terminar em %q", dir, appDirName)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("diretório não foi criado: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%q não é um diretório", dir)
	}
}

func TestDBPath_IsUnderAppDataDirDbSubdir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	appDir, err := AppDataDir()
	if err != nil {
		t.Fatalf("AppDataDir() erro inesperado: %v", err)
	}
	dbPath, err := DBPath()
	if err != nil {
		t.Fatalf("DBPath() erro inesperado: %v", err)
	}
	want := filepath.Join(appDir, "db", "app.db")
	if dbPath != want {
		t.Errorf("DBPath() = %q, esperado %q", dbPath, want)
	}
}

func TestLoad_MissingConfigReturnsErrNotExist(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := Load()
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load() erro = %v, esperado errors.Is(err, os.ErrNotExist)", err)
	}
}

func TestSaveThenLoad_RoundTrips(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	want := &AppConfig{StorageRoot: "/home/user/GoogleDrive/aulas"}
	if err := Save(want); err != nil {
		t.Fatalf("Save() erro inesperado: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() erro inesperado: %v", err)
	}
	if got.StorageRoot != want.StorageRoot {
		t.Errorf("StorageRoot = %q, esperado %q", got.StorageRoot, want.StorageRoot)
	}
}

func TestAudioCacheDir_IsUnderAppDataDirAudioCacheSubdirAndCreated(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	appDir, err := AppDataDir()
	if err != nil {
		t.Fatalf("AppDataDir() erro inesperado: %v", err)
	}
	cacheDir, err := AudioCacheDir()
	if err != nil {
		t.Fatalf("AudioCacheDir() erro inesperado: %v", err)
	}
	want := filepath.Join(appDir, "audio-cache")
	if cacheDir != want {
		t.Errorf("AudioCacheDir() = %q, esperado %q", cacheDir, want)
	}
	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatalf("diretório não foi criado: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%q não é um diretório", cacheDir)
	}
}
