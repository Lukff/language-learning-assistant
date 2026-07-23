// Package importer — ver importer.go. Este arquivo cobre a padronização do
// nome do arquivo de vídeo pós-confirmação (História 3, critério adicional
// registrado em docs/fase-1-mvp.md e desenhado em
// docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md).
package importer

import (
	"fmt"
	"regexp"
	"strings"
)

// accentReplacer remove os acentos mais comuns em nomes próprios PT/ES —
// evita depender de uma lib de transliteração só pra isso (CLAUDE.md: sem
// dependência nova sem justificativa). Espera-se que a entrada já esteja em
// minúsculas (strings.ToLower já normaliza a maioria das formas
// maiúsculas/acentuadas correspondentes).
var accentReplacer = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "ý", "y",
)

var nonSlugRun = regexp.MustCompile(`[^a-z0-9]+`)

// slugify normaliza um nome de tutor pra uso em nome de arquivo: minúsculas,
// sem acento, qualquer sequência de caracteres fora de [a-z0-9] vira um
// único "-", sem "-" nas pontas.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = accentReplacer.Replace(s)
	s = nonSlugRun.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// StandardFilename deriva o nome de arquivo padronizado pós-confirmação, no
// formato "AAAA-MM-DD_HHHMM_tutor-slug.ext" (ex.: "2026-07-23_14H30_maria-jose.mp4").
// lessonDate é o valor bruto de <input type="datetime-local">
// ("AAAA-MM-DDTHH:MM"); o chamador (services/import.go ConfirmImport) já
// garante esse formato antes de chamar esta função — não há fallback aqui
// para data sem horário. ext inclui o ponto (ex.: ".mp4"), como retornado
// por filepath.Ext, e é normalizada para minúsculas.
func StandardFilename(lessonDate, tutor, ext string) string {
	datePart, timePart, _ := strings.Cut(lessonDate, "T")
	timePart = strings.ReplaceAll(timePart, ":", "H")
	slug := slugify(tutor)
	return fmt.Sprintf("%s_%s_%s%s", datePart, timePart, slug, strings.ToLower(ext))
}
