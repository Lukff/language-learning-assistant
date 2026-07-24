// Package importer varre a pasta de armazenamento em busca de vídeos de
// aula que ainda não estão no banco (História 3, docs/fase-1-mvp.md). Não
// assume nenhuma estrutura de subpastas: identificação e dedupe são sempre
// por nome do arquivo + SHA-256, nunca por convenção de path. Não importa
// nada do Wails (camada fina).
package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// videoExtensions lista as extensões reconhecidas como vídeo de aula.
// Fatia deliberadamente curta nesta fatia da História 3 (só .mp4, o formato
// mais comum do Cambly/navegador) — estender é só adicionar aqui.
var videoExtensions = []string{".mp4"}

// Candidate é um vídeo novo achado pela varredura, ainda sem lesson
// registrada nem candidato pendente com o mesmo hash.
type Candidate struct {
	Path          string // relativo à raiz da varredura, sempre com "/" (filepath.ToSlash)
	Size          int64
	MTime         string // RFC3339 (UTC)
	SHA256        string
	SuggestedDate string // AAAA-MM-DD, extraído do nome do arquivo ou do mtime
}

// Summary resume o resultado de uma varredura.
type Summary struct {
	New     int // candidatos novos gravados em pending_imports
	Updated int // lessons existentes com o path atualizado (arquivo movido/renomeado)
	Skipped int // arquivos já conhecidos (lesson ou pending existente), nada mudou
	Errors  int // arquivos que falharam ao ler/statar/hashear — não interrompem a varredura
}

// Repo é o que Scan precisa do banco. Implementado por um adaptador sobre
// internal/db em services/import.go; nos testes deste pacote, por um fake
// em memória — Scan nunca importa internal/db nem database/sql diretamente.
type Repo interface {
	// StatMatch indica se já existe uma lesson registrada exatamente neste
	// path, com este tamanho e mtime — se sim, o arquivo é conhecido e
	// inalterado, e Scan pula sem calcular hash.
	StatMatch(path string, size int64, mtime string) (bool, error)

	// LessonByHash retorna o path de uma lesson já registrada com este
	// hash, se existir.
	LessonByHash(hash string) (path string, found bool, err error)

	// UpdateLessonPath atualiza o path/tamanho/mtime da lesson com este
	// hash — usado quando o arquivo só mudou de lugar/nome.
	UpdateLessonPath(hash string, path string, size int64, mtime string) error

	// PendingExists indica se já existe um candidato pendente com este
	// hash (de uma varredura anterior ainda não confirmada).
	PendingExists(hash string) (bool, error)

	// InsertPending grava um candidato novo.
	InsertPending(c Candidate) error
}

// Scan caminha root recursivamente, filtra por videoExtensions e decide,
// para cada arquivo, se é conhecido (ignora), mudou de lugar (atualiza o
// path da lesson) ou é candidato novo (grava em pending_imports via
// repo.InsertPending). Erro ao processar um arquivo específico não aborta a
// varredura — é contado em Summary.Errors e o restante continua.
func Scan(root string, repo Repo) (Summary, error) {
	var sum Summary

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			sum.Errors++
			return nil
		}
		if d.IsDir() || !HasVideoExtension(path) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			sum.Errors++
			return nil
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			sum.Errors++
			return nil
		}
		relPath = filepath.ToSlash(relPath)
		size := info.Size()
		mtime := info.ModTime().UTC().Format(time.RFC3339)

		matched, err := repo.StatMatch(relPath, size, mtime)
		if err != nil {
			sum.Errors++
			return nil
		}
		if matched {
			sum.Skipped++
			return nil
		}

		hash, err := HashFile(path)
		if err != nil {
			sum.Errors++
			return nil
		}

		existingPath, found, err := repo.LessonByHash(hash)
		if err != nil {
			sum.Errors++
			return nil
		}
		if found {
			if existingPath == relPath {
				sum.Skipped++
				return nil
			}
			if err := repo.UpdateLessonPath(hash, relPath, size, mtime); err != nil {
				sum.Errors++
				return nil
			}
			sum.Updated++
			return nil
		}

		pending, err := repo.PendingExists(hash)
		if err != nil {
			sum.Errors++
			return nil
		}
		if pending {
			sum.Skipped++
			return nil
		}

		candidate := Candidate{
			Path:          relPath,
			Size:          size,
			MTime:         mtime,
			SHA256:        hash,
			SuggestedDate: SuggestDate(filepath.Base(path), info.ModTime()),
		}
		if err := repo.InsertPending(candidate); err != nil {
			sum.Errors++
			return nil
		}
		sum.New++
		return nil
	})
	if err != nil {
		return sum, fmt.Errorf("varrer pasta de armazenamento: %w", err)
	}
	return sum, nil
}

// HasVideoExtension indica se path tem uma extensão de vídeo reconhecida
// (hoje só .mp4). Exportada porque services.ImportService.DropImport
// (História 3b) precisa da mesma checagem pra um arquivo solto via
// drag-and-drop, fora da varredura da pasta.
func HasVideoExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, want := range videoExtensions {
		if ext == want {
			return true
		}
	}
	return false
}

// HashFile calcula o SHA-256 do arquivo em path. Exportada pelo mesmo
// motivo de HasVideoExtension — DropImport hasheia um arquivo fora da
// varredura da pasta.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("abrir arquivo: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("ler arquivo: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var isoDateInName = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})`)

// SuggestDate tenta achar uma data AAAA-MM-DD no nome do arquivo — nesse
// caso o horário é desconhecido, sugerido como 00:00. Na falta de data no
// nome, usa data e horário do mtime do arquivo (aproximação razoável: o
// arquivo normalmente é baixado logo depois da aula). Formato compatível
// com <input type="datetime-local"> (AAAA-MM-DDTHH:MM). É só um palpite
// pré-preenchido no modal de confirmação — o usuário sempre pode corrigir
// data e horário.
func SuggestDate(filename string, mtime time.Time) string {
	if m := isoDateInName.FindString(filename); m != "" {
		return m + "T00:00"
	}
	return mtime.Format("2006-01-02T15:04")
}
