# História 6 — Detalhe da aula: vídeo + transcrição sincronizada

> Spec de design. Histórias e critérios de aceite em `docs/fase-1-mvp.md`.

## Contexto

O Detalhe (História 5) hoje é um stub mínimo: cabeçalho (data/tutor) + `<video controls>`
apontando pro endpoint `/media/lesson/{id}` (que já resolveu o risco técnico 1 — range
requests). Falta o que torna essa tela o coração do MVP: a transcrição rolável ao lado,
clique-na-fala pula o vídeo, e o highlight da fala corrente acompanhando o playback. Esta
história fecha o M3 ("MVP completo") do `fase-1-mvp.md`.

Layout de referência: `docs/prototipo-app-aulas.jsx`, componente `LessonDetail` (linhas
367-568) — grade de 2 colunas (vídeo à esquerda, painel à direita), falas do aluno em azul
(borda esquerda), tutor em verde. O protótipo também mostra uma aba "Análise" e correções
inline — ambas são Fase 2, fora de escopo aqui.

## Decisões

### Papel dos speakers (aluno × tutor) não é persistido hoje

O banco nunca guardou qual `speaker_0`/`speaker_1` é aluno — isso só existe hoje como parâmetro
em memória usado por `analysis.FormatTranscript` (Fase 2). Como as gravações do Cambly têm
sempre exatamente 2 speakers, a solução é uma coluna nova, não uma tabela genérica:

- Migration `00003_student_speaker.sql`: `ALTER TABLE lessons ADD COLUMN
  student_speaker_label TEXT`. `NULL` até o usuário escolher.
- `db.SetStudentSpeaker(conn, lessonID, speakerLabel string) error` grava a escolha.
- **Rótulos antes de escolher:** "Speaker A" / "Speaker B", atribuídos pela ordem de primeira
  fala na transcrição (determinístico — não depende de ordem de mapa/iteração). Sem cor de
  papel (nem azul nem verde) até o usuário escolher.
- **Toggle:** dois botões pequenos acima do painel de transcrição ("Speaker A é você" /
  "Speaker B é você"). Ao escolher, grava via `SetStudentSpeaker` e a UI já reflete
  imediatamente (otimista): aluno em azul com borda esquerda (like protótipo) rotulado "Você",
  tutor em verde rotulado "Tutor". Persistido — reaberturas futuras já vêm coloridas.

### Acesso ao Detalhe deixa de exigir status "pronta"

`Library.svelte` (`openLesson`) hoje só navega pro Detalhe quando `status === "pronta"`, o que
por definição (`deriveStatus`, História 5) já exige `transcribe` concluído — ou seja, o
critério de aceite "aula sem transcrição ainda reproduz o vídeo normalmente" nunca seria
alcançável navegando pela Biblioteca como está hoje. Ajuste necessário: `openLesson` passa a
navegar em qualquer status. O vídeo sempre é assistível (princípio de resiliência); a
transcrição/erro são tratados dentro do próprio Detalhe (ver "Painel sem transcrição" abaixo).

Consequência: `LibraryService.GetLesson` precisa computar `Status`/`ErrorMessage` (hoje não
computa — comentário do código dizia que não valia a pena porque só se chegava lá com "pronta").
Reaproveita a mesma lógica de `ListLessonsWithStatus`/`deriveStatus` da História 5, por id único.

### Sincronização vídeo ↔ transcrição: evento `timeupdate`, sem RAF nem WebVTT

Três abordagens consideradas:

1. **Escolhida.** Handler `ontimeupdate` no `<video>` atualiza um `$state<number>` de
   `currentTime`; um `$derived` acha a última utterance com `start <= currentTime` (busca
   linear — poucas centenas de itens por aula, custo irrelevante). Clique numa fala faz
   `videoEl.currentTime = utterance.start`. Simples, idiomático em runes, sem dependência nova.
2. Loop `requestAnimationFrame` lendo `currentTime` a cada frame: mais preciso (60x/s) do que o
   necessário — o destaque é por fala inteira, não por palavra, e `timeupdate` (~4x/s no
   Chromium/WebKit) já fica dentro da tolerância de ~1s do critério de aceite.
3. Trilha WebVTT nativa (`<track>` + evento `cuechange`): aproveitaria sincronização nativa do
   browser, mas VTT renderiza como legenda sobreposta ao vídeo — não encaixa no painel lateral
   customizado (cores por papel, clique, auto-scroll) exigido pelo protótipo.

Clique numa fala só ajusta `currentTime` (seek) — não força play nem pause; se o vídeo estava
pausado, continua pausado na nova posição. Evita surpreender quem só quer conferir o timestamp.

Auto-scroll: `$effect` observando o índice destacado chama `scrollIntoView({block:"nearest"})`
na linha correspondente, mantendo a fala corrente visível sem saltos bruscos. Funciona também
pra seeks feitos pelo controle nativo do `<video>` (scrubber), já que `timeupdate` dispara
independente da origem da mudança de posição.

Granularidade: só no nível de utterance (fala inteira), não de palavra — não expor `Words` ao
frontend nesta história (a estrutura `stt.Utterance.Words` já existe no banco pra uma eventual
Fase 2 de highlight palavra-a-palavra, mas não há critério de aceite pra isso agora).

### Painel sem transcrição

Como o Detalhe agora abre em qualquer status, o painel à direita do vídeo trata os casos onde
não há linha em `transcripts`:

- `status === "processando"` → "Transcrição em processamento…".
- `status === "erro"` → mensagem de erro (`ErrorMessage` do `GetLesson`) + botão "Reprocessar",
  reaproveitando `LibraryService.RetryLesson` (já usado na Biblioteca desde a História 5).
- `status === "pronta"` sem transcrição encontrada (não deveria acontecer dado `deriveStatus`,
  mas defensivo contra corrida/estado inconsistente) → mesmo texto de "processando".

O vídeo sempre renderiza e toca normalmente nos três casos — só o painel muda.

### Sem abas no painel (Análise fica pra Fase 2)

O protótipo tem abas "Transcrição"/"Análise", mas Análise LLM na UI é explicitamente fora de
escopo da Fase 1 (`fase-1-mvp.md`). Construir a barra de abas agora seria UI pra uma função que
ainda não existe. O painel mostra a transcrição direto, sem barra de abas — a aba de Análise
entra quando a Fase 2 chegar.

## Mudanças por camada

### `internal/db`
- Migration `00003_student_speaker.sql`.
- `FindTranscriptByLessonID(conn *sql.DB, lessonID int64) (*Transcript, error)` — lê
  `raw_json_path`/`utterances`, desserializa `utterances` em `[]stt.Utterance`. Retorna `nil,
  nil` se não houver linha (não é erro — estado normal enquanto `transcribe` não concluiu).
- `SetStudentSpeaker(conn *sql.DB, lessonID int64, speakerLabel string) error`.
- `FindLessonByID` (ou uma variante) passa a trazer também status/erro derivados — reaproveita
  `deriveStatus`/join de jobs já existente em `ListLessonsWithStatus`, adaptado pra um único id.

### `services/library.go`
- `Lesson` ganha `StudentSpeakerLabel *string`.
- `GetLesson`: agora preenche `Status`/`ErrorMessage` (antes deixava zerado por não ter
  chamador que precisasse).
- Novo `Transcript`/`Utterance` (tipos expostos ao frontend): `Utterance{ Speaker, Text string;
  StartSeconds, EndSeconds float64 }` — conversão de `time.Duration` pra segundos feita aqui,
  na borda do serviço (frontend trabalha só em segundos, mesma unidade de `video.currentTime`).
- Novo `GetTranscript(lessonID int64) (*Transcript, error)`: `nil, nil` se não houver
  transcrição ainda.
- Novo `SetStudentSpeaker(lessonID int64, speakerLabel string) error`.

### Frontend
- `frontend/src/lib/screens/Library.svelte`: `openLesson` navega em qualquer status (remove a
  checagem `status === "pronta"`).
- `frontend/src/lib/screens/LessonDetail.svelte`: grade 2 colunas; `<video bind:this>` com
  `ontimeupdate`; painel de transcrição com toggle de speaker, linhas clicáveis
  (`videoEl.currentTime = utterance.startSeconds`), highlight + auto-scroll da fala corrente,
  estados de "sem transcrição" (processando/erro com Reprocessar).
- Bindings Wails regeneradas (`wails3 generate bindings -ts -i ./...`) refletindo
  `GetTranscript`/`SetStudentSpeaker`/novos campos de `Lesson`.

## Fora de escopo (não implementar aqui)

Aba de Análise e correções inline (Fase 2); highlight/clique no nível de palavra; tela de Fila
e badge de contagem de jobs ativos (História 7); qualquer mudança em `RetryLesson`/worker além
do reaproveitamento já existente.

## Testes

- `internal/db`: round-trip `SetStudentSpeaker`; `FindTranscriptByLessonID` com fixture
  sintética de utterances (conversão de `time.Duration` incluída), `nil, nil` quando não há
  transcrição; leitura de lesson com status derivado incluindo o novo campo.
- `services`: `GetLesson` retornando status/erro corretos em todas as combinações; `GetTranscript`
  com/sem transcrição; `SetStudentSpeaker` refletido em `GetLesson` subsequente.
- Frontend: sem suíte de testes JS configurada neste projeto — validação via `npm run
  check`/`build` limpos; verificação visual (janela real: clique pula vídeo, highlight
  acompanha playback, toggle de speaker) segue pendente nas máquinas Windows/Linux, mesmo
  padrão das histórias anteriores.
