package db

import (
	"database/sql"
	"fmt"
	"time"
)

// PendingImport é um vídeo achado pela varredura da pasta de armazenamento
// que ainda não foi confirmado (data/tutor) pelo usuário — ver História 3
// em docs/phase-1-mvp.md.
type PendingImport struct {
	ID            int64
	Path          string
	FileSize      int64
	FileMTime     string
	SHA256        string
	SuggestedDate string
}

// FindPendingImportByHash indica se já existe um candidato pendente com
// este hash — evita duplicar a mesma varredura em execuções sucessivas.
func FindPendingImportByHash(conn *sql.DB, hash string) (bool, error) {
	var id int64
	err := conn.QueryRow(`SELECT id FROM pending_imports WHERE sha256 = ?`, hash).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("buscar pending_import por hash: %w", err)
	}
	return true, nil
}

// InsertPendingImport grava um candidato novo achado pela varredura (ou
// por um drop manual, História 3b) e retorna o id da linha criada — o
// chamador precisa dele pra montar o PendingImport exposto ao frontend
// sem uma segunda consulta.
func InsertPendingImport(conn *sql.DB, p PendingImport) (int64, error) {
	res, err := conn.Exec(
		`INSERT INTO pending_imports (path, file_size, file_mtime, sha256, suggested_date, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.Path, p.FileSize, p.FileMTime, p.SHA256, p.SuggestedDate, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("inserir pending_import: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("obter id do pending_import: %w", err)
	}
	return id, nil
}

// ListPendingImports lista os candidatos aguardando revisão, mais recentes
// primeiro — é o que a Biblioteca lê pra montar a seção "aguardando revisão".
func ListPendingImports(conn *sql.DB) ([]PendingImport, error) {
	rows, err := conn.Query(
		`SELECT id, path, file_size, file_mtime, sha256, COALESCE(suggested_date, '') FROM pending_imports ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listar pending_imports: %w", err)
	}
	defer rows.Close()

	var out []PendingImport
	for rows.Next() {
		var p PendingImport
		if err := rows.Scan(&p.ID, &p.Path, &p.FileSize, &p.FileMTime, &p.SHA256, &p.SuggestedDate); err != nil {
			return nil, fmt.Errorf("ler pending_import: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar pending_imports: %w", err)
	}
	return out, nil
}

// ConfirmPendingImport transforma o candidato id numa lesson real: insere em
// lessons (com lessonDate/teacherName informados pelo usuário, o segundo
// resolvido para um teacher_id via GetOrCreateTeacherByName), cria os jobs
// extract_audio e transcribe como pending, e remove o candidato de
// pending_imports — tudo numa única transação. Se qualquer passo falhar, o
// candidato continua intacto em pending_imports para o usuário tentar de
// novo.
func ConfirmPendingImport(conn *sql.DB, id int64, lessonDate string, teacherName string) (int64, error) {
	tx, err := conn.Begin()
	if err != nil {
		return 0, fmt.Errorf("iniciar transação: %w", err)
	}
	defer tx.Rollback()

	var p PendingImport
	err = tx.QueryRow(
		`SELECT id, path, file_size, file_mtime, sha256 FROM pending_imports WHERE id = ?`,
		id,
	).Scan(&p.ID, &p.Path, &p.FileSize, &p.FileMTime, &p.SHA256)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("candidato %d não encontrado (já foi confirmado ou removido?)", id)
	}
	if err != nil {
		return 0, fmt.Errorf("buscar pending_import: %w", err)
	}

	teacherID, err := getOrCreateTeacherByName(tx, teacherName)
	if err != nil {
		return 0, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, video_hash, file_size, file_mtime, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		lessonDate, teacherID, p.Path, p.SHA256, p.FileSize, p.FileMTime, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("inserir lesson: %w", err)
	}
	lessonID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("obter id da lesson: %w", err)
	}

	for _, kind := range []string{"extract_audio", "transcribe"} {
		if _, err := tx.Exec(
			`INSERT INTO jobs (lesson_id, kind, status, created_at, updated_at) VALUES (?, ?, 'pending', ?, ?)`,
			lessonID, kind, now, now,
		); err != nil {
			return 0, fmt.Errorf("criar job %s: %w", kind, err)
		}
	}

	if _, err := tx.Exec(`DELETE FROM pending_imports WHERE id = ?`, id); err != nil {
		return 0, fmt.Errorf("remover pending_import: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("confirmar transação: %w", err)
	}
	return lessonID, nil
}
