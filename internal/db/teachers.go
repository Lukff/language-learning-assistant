// internal/db/teachers.go
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Teacher é uma linha de teachers — a entidade que substitui a antiga
// coluna livre lessons.tutor (ver História 9). Nome é único: renomear é um
// UPDATE de uma linha só, refletido em todas as aulas via JOIN.
type Teacher struct {
	ID   int64
	Name string
}

// ListTeachers lista os professores cadastrados em ordem alfabética —
// alimenta tanto o combobox de importação/edição quanto o painel
// "Professores" de Configurações.
func ListTeachers(conn *sql.DB) ([]Teacher, error) {
	rows, err := conn.Query(`SELECT id, name FROM teachers ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("listar professores: %w", err)
	}
	defer rows.Close()

	out := make([]Teacher, 0)
	for rows.Next() {
		var t Teacher
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("ler professor: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar professores: %w", err)
	}
	return out, nil
}

// execer é satisfeito tanto por *sql.DB quanto por *sql.Tx — permite
// getOrCreateTeacherByName rodar tanto solto (GetOrCreateTeacherByName)
// quanto dentro de uma transação já aberta (ConfirmPendingImport, que
// precisa que a criação do professor, se for novo, faça parte da mesma
// transação da lesson). Mesmo padrão de rowScanner em lesson_status.go.
type execer interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

// getOrCreateTeacherByName é a implementação compartilhada por
// GetOrCreateTeacherByName e por ConfirmPendingImport (via tx). name é
// aparado (TrimSpace) antes de qualquer busca/gravação — evita que espaço
// em branco perdido no combobox (ex.: "Sarah M. " vs "Sarah M.") vire um
// professor duplicado, exatamente o problema de grafia divergente que a
// entidade teachers existe para evitar (ver Contexto do design da
// História 9).
func getOrCreateTeacherByName(q execer, name string) (int64, error) {
	name = strings.TrimSpace(name)
	var id int64
	err := q.QueryRow(`SELECT id FROM teachers WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("buscar professor por nome: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := q.Exec(
		`INSERT INTO teachers (name, created_at, updated_at) VALUES (?, ?, ?)`,
		name, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("criar professor: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("obter id do professor criado: %w", err)
	}
	return id, nil
}

// GetOrCreateTeacherByName resolve o nome livre digitado no combobox
// (importação ou edição de aula) para um teacher_id: retorna o id
// existente se o nome já está cadastrado, senão cria um professor novo.
func GetOrCreateTeacherByName(conn *sql.DB, name string) (int64, error) {
	return getOrCreateTeacherByName(conn, name)
}

// RenameTeacher renomeia o professor id — reflete em todas as aulas dele
// automaticamente (JOIN, não há cópia do nome em lessons). Colisão com um
// nome já usado por outro professor (UNIQUE) vira um erro legível, sem
// mesclar registros (fora de escopo da História 9).
func RenameTeacher(conn *sql.DB, id int64, newName string) error {
	newName = strings.TrimSpace(newName)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := conn.Exec(`UPDATE teachers SET name = ?, updated_at = ? WHERE id = ?`, newName, now, id)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("já existe um professor com esse nome")
		}
		return fmt.Errorf("renomear professor: %w", err)
	}
	return nil
}

// isUniqueConstraintError detecta violação de UNIQUE do driver SQLite —
// isolado aqui (única dependência de driver específico na camada de
// repositório, CLAUDE.md) pra RenameTeacher poder devolver uma mensagem
// legível em vez do erro cru do SQLite.
func isUniqueConstraintError(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
