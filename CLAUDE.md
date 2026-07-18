# CLAUDE.md

## O projeto

Assistente de aprendizagem de idioma: app desktop **pessoal** (1 usuário, 1 dev, sem servidor
próprio) que arquiva gravações de aulas de inglês do Cambly e gera transcrições diarizadas
(aluno × tutor), análises via LLM (correções, vocabulário, expressões do tutor) e visão de
progresso. UI e análises em PT-BR; as aulas são em inglês com code-switching ocasional (PT/ES).

**Fontes da verdade** (ler antes de decidir qualquer coisa):
- `docs/decisoes-tecnologia.md` — escolhas de tecnologia vigentes. Não contrariar silenciosamente; se uma escolha precisar mudar, propor a atualização do documento.
- `docs/fase-0-validacao.md` — histórias e tracking da fase atual.

## Fase atual: Fase 0 (spike de validação)

CLI em Go, **sem Wails e sem banco de dados**. Objetivo: validar STT (4 candidatos: Deepgram,
AssemblyAI, ElevenLabs Scribe, Gladia) e análise LLM com aulas reais, antes de qualquer UI.

Regras de escopo do spike — recusar over-engineering ativamente:
- Sem flags elaboradas, sem paralelismo, sem retry sofisticado. Paths hardcoded e `go run` são aceitáveis no `main.go`.
- Salvar sempre a resposta bruta (JSON) de cada provedor em disco, por aula/provedor.
- O `main.go` do CLI é descartável; os packages `media`, `stt` e `analysis` são **definitivos** — qualidade de produção neles.

## Arquitetura — princípio da camada fina

Todo o core vive em packages Go puros, sem dependência de framework de UI:

```
cmd/spike/          # CLI da Fase 0 (descartável)
internal/media/     # extração de áudio (ffmpeg via os/exec)
internal/stt/       # interface única + uma implementação por provedor
internal/analysis/  # análise via LLM; saída JSON estruturada
prompts/            # prompts versionados (analyze-v1.md, ...)
docs/               # decisoes-tecnologia.md, fase-0-validacao.md
testdata/           # JSONs brutos dos provedores como fixtures
```

Fases futuras (não implementar agora, mas não bloquear): Wails v3 (alpha, versão pinada) como
casca, com frontend **Svelte 5**; SQLite via `modernc.org/sqlite` com migrations `goose`; fila de
jobs em tabela + worker único; sync pull-work-push com snapshot via `VACUUM INTO`.

## Stack e convenções

- Go recente; preferir **stdlib**: `net/http` para APIs, `os/exec` para ffmpeg, `log/slog` para logs, `encoding/json`.
- Dependências externas só com justificativa (as aprovadas para fases futuras estão no `decisoes-tecnologia.md`).
- **Credenciais:** somente variáveis de ambiente na Fase 0 (`DEEPGRAM_API_KEY`, `ASSEMBLYAI_API_KEY`, `ELEVENLABS_API_KEY`, `GLADIA_API_KEY`, `ANTHROPIC_API_KEY`/`OPENAI_API_KEY`). Nunca hardcoded, nunca commitadas. No app final: keyring nativo do SO.
- **SQL portável** quando o banco existir: nada específico de driver na camada de repositório.
- **Frontend (fases futuras): Svelte 5 com runes, sempre.** Nunca usar sintaxe legada do Svelte 3/4 (stores com `$:`, `export let`, etc.) — usar `$state`, `$derived`, `$effect`, `$props`. Se houver dúvida entre padrão antigo e novo, parar e perguntar.
- Código e identificadores em inglês; documentação, mensagens de erro voltadas ao usuário e textos de análise em PT-BR.
- STT sempre com diarização + timestamps por palavra + configuração multilíngue/code-switching do provedor (documentar no código a configuração usada e por quê).
- No prompt de análise: palavra em PT/ES na fala do aluno é recurso ao idioma nativo (candidata a vocabulário), **não** erro de inglês.

## Comandos

```bash
go run ./cmd/spike        # roda o pipeline do spike
go test ./...             # testes (fixtures em testdata/)
go vet ./...              # antes de commitar
```

## Commits

Mensagens de commit devem ser **uma linha só**, no formato semântico (`tipo: descrição`) — ex.:
`feat: adiciona extração de áudio via ffmpeg`, `fix: corrige parsing de timestamp da Gladia`,
`docs: registra decisão de STT`. Tipos usuais: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`.

## Privacidade

O repositório é **público** (ou pode vir a ser a qualquer momento — tratar como público desde já).
As gravações e transcrições são dados pessoais do dev e de terceiros (tutores):

- Vídeos, áudios e **qualquer JSON/transcrição de aula real** nunca entram no repo. Manter no `.gitignore` os diretórios de trabalho (ex.: `local/`, `*.mp4`, `*.wav`, saídas brutas dos provedores).
- Fixtures em `testdata/` devem ser **sintéticas ou anonimizadas**: conversas inventadas no mesmo formato de resposta de cada provedor, sem nomes reais, sem trechos de aulas reais.
- Nunca citar nomes de tutores, IDs de conta ou dados de billing em código, comentários, commits ou documentação.
- Chaves de API somente em variáveis de ambiente; conferir que nenhum exemplo de `.env` commitado contém valores reais.
