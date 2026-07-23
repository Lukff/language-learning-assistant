# História 3 — Renomeação padronizada do arquivo ao confirmar: design

> Fatia adicional à História 3 (`docs/fase-1-mvp.md`), sobre o fluxo de confirmação
> (`ImportService.ConfirmImport`) já implementado e descrito em
> `docs/superpowers/specs/2026-07-22-historia-3-importar-aula-design.md`. Não reabre o que já
> foi entregue naquela fatia (varredura, staging, dedupe) — só adiciona um passo novo depois da
> confirmação, e uma validação nova antes dela.

## Contexto e motivação

Hoje, `ConfirmPendingImport` grava `video_path` exatamente como o arquivo foi encontrado na
varredura — nome original do download do Cambly (ou qualquer nome que o arquivo já tivesse).
Isso deixa a raiz de armazenamento com nomes inconsistentes entre si, dificultando localizar um
arquivo específico fora do app (Explorer/Finder, backup manual, etc.).

Decisão: depois que o usuário preenche os metadados (data, horário, tutor) no modal de
confirmação, o arquivo de vídeo é renomeado *in place* (mesma pasta) para um nome padronizado
derivado desses metadados.

**Consequência acoplada:** para o nome ser derivável de forma confiável, horário deixa de ser
opcional — data, horário e tutor passam a ser **todos obrigatórios** para confirmar um
candidato. Um candidato sem algum desses continua em `pending_imports` (pendente), a
confirmação é recusada com erro claro.

## Formato do nome

```
AAAA-MM-DD_HHHMM_tutor-slug.ext
```

Exemplo: tutor "Maria José", 2026-07-23 14:30 → `2026-07-23_14H30_maria-jose.mp4`.

- Separador de hora: `H` entre hora e minuto (`14H30`), não `:` (inválido em nome de arquivo no
  Windows) nem `-` (ambíguo com o próprio separador de data).
- Separador entre data e hora: `_`.
- Extensão: preservada do arquivo original, em minúsculas.
- **Sem fallback "sem horário"**: a validação em `ConfirmImport` (ver abaixo) garante que
  `lessonDate` sempre chega aqui com data **e** horário. Não há mais candidato a confirmação sem
  horário.

### Slug do tutor

- Minúsculas.
- Acentos removidos via tabela de substituição manual (á→a, ã→a, ç→c, etc. — cobre PT/ES, os
  idiomas de code-switching do projeto); sem dependência nova (`CLAUDE.md`: dependência externa
  só com justificativa, e isso é resolvível em stdlib com uma tabela pequena).
- Qualquer sequência de caracteres fora de `[a-z0-9]` (espaços, pontuação, etc.) vira um único
  `-`; sem `-` nas pontas.
- Tutor vazio nunca chega aqui — já é validado como obrigatório em `ConfirmImport` (validação
  pré-existente, mantida).

### Colisão de nome

Duas aulas confirmadas com o mesmo instante (minuto) e mesmo tutor gerariam o mesmo nome-alvo
(raro, mas possível). Se o nome-alvo já existe no diretório **e não é o próprio arquivo sendo
renomeado**, tenta sufixos `-2`, `-3`, ... antes da extensão (`..._maria-jose-2.mp4`) até achar
um nome livre.

## Validação obrigatória em `ConfirmImport`

`services/import.go` `ConfirmImport(id, lessonDate, tutor)` já valida `lessonDate != ""` e
`tutor != ""`. Passa a validar também que `lessonDate` tem componente de horário — rejeita
`"AAAA-MM-DD"` puro (sem `T...`), não só string vazia. Mensagem de erro clara em PT-BR (ex.:
`"horário da aula é obrigatório"`).

Como `db.ConfirmPendingImport` só é chamado depois dessa validação passar, uma falha aqui
significa que a transação (insert em `lessons` + jobs, delete de `pending_imports`) **nunca
roda** — o candidato continua intacto em `pending_imports`, ou seja, continua pendente de
revisão. Isso já é o comportamento natural da função hoje para os dois campos existentes; a
mudança é só estender a checagem de "não vazio" para "não vazio e com horário", no mesmo lugar.

O input `datetime-local` do modal (`ImportConfirmModal.svelte`) já é atômico — não dá pra
submeter só a data sem hora por essa UI — então esta validação no backend é principalmente
defesa em profundidade (outros chamadores futuros, dados malformados) e uma mensagem de erro
explícita, não uma mudança de comportamento observável na UI atual.

## Arquitetura

```
internal/importer/
  naming.go        # StandardFilename(lessonDate, tutor, ext string) string — pura, sem I/O
  naming_test.go
services/
  import.go         # ConfirmImport: validação de horário + chamada ao novo passo best-effort
  import_test.go
```

`internal/importer.StandardFilename` não faz I/O (não sabe de diretórios nem de colisão em
disco) — só computa o nome a partir dos três valores. Resolver colisão exige checar o
filesystem, então fica em `services/import.go`, que já é a camada que conhece `storage_root`.

### `StandardFilename`

```go
// internal/importer/naming.go
package importer

// StandardFilename deriva o nome de arquivo padronizado pós-confirmação, a
// partir da data/horário e do tutor informados no modal de confirmação.
// lessonDate é o valor bruto de <input type="datetime-local">
// ("AAAA-MM-DDTHH:MM"); ConfirmImport garante esse formato antes de chamar
// esta função — não há fallback aqui para data sem horário.
func StandardFilename(lessonDate, tutor, ext string) string
```

### Passo best-effort em `services/import.go`

Mesmo padrão já estabelecido por `setDurationBestEffort` (chamado logo depois dela, dentro de
`ConfirmImport`, após `db.ConfirmPendingImport` ter sucesso):

1. Carrega a lesson (`db.FindLessonByID`) — já teria sido carregada por `setDurationBestEffort`;
   pode reaproveitar a mesma leitura em vez de duas.
2. Resolve `storage_root` (`config.Load()`, já usado nesse arquivo).
3. Calcula o nome-alvo via `importer.StandardFilename(lesson.LessonDate, lesson.Tutor,
   filepath.Ext(lesson.VideoPath))`.
4. Resolve o diretório atual do vídeo (`filepath.Dir` do path relativo) — o rename é sempre
   dentro dessa mesma pasta, nunca move de diretório.
5. Resolve colisão: se um arquivo já existe no path-alvo e não é o arquivo atual, tenta sufixos
   até achar um nome livre.
6. Se o nome-alvo (após resolver colisão) já é igual ao nome atual, não faz nada (idempotente —
   evita rename desnecessário se a função rodar de novo sobre uma lesson já padronizada).
7. `os.Rename(caminhoAbsolutoAtual, caminhoAbsolutoAlvo)`.
8. Re-`os.Stat` no novo path pra pegar tamanho/mtime atuais (não assume que rename preserva
   mtime em todo SO/filesystem).
9. `db.UpdateLessonPath(conn, lessonID, novoPathRelativo, size, mtime)` — função já existente
   (criada na História 3 original para o caso "arquivo só mudou de lugar"), reaproveitada sem
   mudança de assinatura.

Qualquer erro em qualquer um desses passos (permissão negada, I/O, colisão irresolúvel após N
tentativas) é só logado (`slog.Warn`, com `lesson_id` e o erro) — **nunca** propagado como erro
de `ConfirmImport`. A lesson já foi confirmada com sucesso na transação; o nome do arquivo é
cosmético, não crítico para o funcionamento do app (princípio de resiliência do `CLAUDE.md`).
Se o rename falhar, o vídeo continua com o nome original, plenamente funcional.

### Jobs downstream (`internal/jobs/worker.go`)

Nenhuma mudança necessária. `runExtractAudio`/`runTranscribe` resolvem `lesson.VideoPath` do
banco em tempo de execução (releem a lesson a cada job, não guardam path em cache) — se o rename
já rodou antes do worker pegar os jobs (caso comum, jobs `pending` recém-criados), eles já usam
o nome novo automaticamente. Mesmo se o worker de alguma forma rodar antes do rename best-effort
terminar (não deveria, é síncrono dentro de `ConfirmImport`, mas hipoteticamente), o pior caso é
o worker ler o path antigo numa corrida — inofensivo, porque o path é lido fresco a cada
transição de job, não uma vez só.

## Fora de escopo desta fatia

- Mover o arquivo pra estrutura de subpastas (`aulas/AAAA/AAAA-MM-DD/`) — decisão de estrutura de
  diretórios que só se aplica quando a História 3b (drag-and-drop) for desenhada; renomeação
  aqui é sempre *in place*, mesma pasta.
- Renomear retroativamente aulas já confirmadas antes desta mudança — nenhuma aula real foi
  confirmada em uso real ainda.
- Mudanças na História 3b em si — quando ela for desenhada, reaproveita
  `internal/importer.StandardFilename` sem alteração.

## Testes

- `internal/importer/naming_test.go` (tabela): tutor com acentos (PT/ES), tutor com espaços
  múltiplos/pontuação, extensão em maiúsculas normalizada, exemplo do formato completo
  (`2026-07-23T14:30` + `"Maria José"` + `.mp4` → `2026-07-23_14H30_maria-jose.mp4`).
- `services/import_test.go`:
  - `ConfirmImport` com `lessonDate` só-data (sem horário) retorna erro; candidato permanece em
    `pending_imports` (não vira lesson, nenhum job criado).
  - Rename bem-sucedido: arquivo no disco (`t.TempDir()`) renomeado, `lessons.video_path`
    atualizado, `file_size`/`file_mtime` consistentes com o arquivo no novo path.
  - Colisão: cria um arquivo pré-existente com o nome-alvo antes de confirmar; assert que o
    resultado usa o sufixo `-2`.
  - Falha simulada no rename (ex.: path-alvo é um diretório existente, causando erro de
    `os.Rename`) não quebra `ConfirmImport` — lesson continua confirmada, com o `video_path`
    original intacto.

## Critério de aceite (novo, adicionado à História 3 em `docs/fase-1-mvp.md`)

- [ ] Depois de confirmado (data/horário/tutor no modal), o arquivo de vídeo é renomeado *in
      place* para `AAAA-MM-DD_HHHMM_tutor-slug.ext`; falha no rename não impede a confirmação
      (best-effort, logada). Horário passa a ser obrigatório na confirmação — candidato sem
      data, horário ou tutor continua pendente.
