# Agents.md

## O projeto

Assistente de aprendizagem de idioma: app desktop **pessoal** (1 usuário, 1 dev, sem servidor
próprio) que arquiva gravações de aulas de inglês do Cambly e gera transcrições diarizadas
(aluno × tutor), análises via LLM (correções, vocabulário, expressões do tutor) e visão de
progresso. UI e análises em PT-BR; as aulas são em inglês com code-switching ocasional (PT/ES).

**Fontes da verdade** (ler antes de decidir qualquer coisa):
- `docs/decisoes-tecnologia.md` — escolhas de tecnologia vigentes. Não contrariar silenciosamente; se uma escolha precisar mudar, propor a atualização do documento.
- `docs/fase-1-mvp.md` — histórias e tracking da fase atual.
- `docs/fase-0-validacao.md` — fase concluída (validação das APIs); mantido como histórico.

## Fase atual: Fase 1 (MVP — importar → transcrever → assistir)

App Wails v3 (alpha, **versão pinada** no `go.mod`; upgrade de alpha é tarefa deliberada, nunca no
meio de uma feature) + Svelte 5, com banco SQLite. Escopo, histórias e marcos em
`docs/fase-1-mvp.md` — segui-lo, incluindo a lista de **fora de escopo** (análise na UI, tags,
progresso, sync, FTS ficam para fases seguintes) e os **riscos técnicos a atacar primeiro**
(vídeo com range requests no asset handler; drag-and-drop no v3; keyring no Linux).

Regras da fase:
- Os packages da Fase 0 (`media`, `stt`, `analysis`) são reaproveitados como estão — não copiar, não reescrever.
- O CLI `cmd/spike` e os providers de STT descartados na comparação (Gladia, AssemblyAI, Deepgram) foram removidos: cumpriram o papel de validação da Fase 0 e não têm mais chamador no app. `internal/stt` mantém só o ElevenLabs.
- STT do app: ElevenLabs Scribe (`scribe_v2`, diarização, detecção multilíngue) — configuração registrada no `decisoes-tecnologia.md`.
- Princípio de resiliência: falha de transcrição/análise nunca impede assistir ao vídeo.

## Arquitetura — princípio da camada fina

Todo o core vive em packages Go puros, **sem imports de Wails** — o Wails entra apenas como casca
(janela, bindings, eventos). Isso é inegociável: torna barato recuar de versão do framework.

```
main.go             # entrada do app Wails v3 (casca)
frontend/           # Svelte 5 (runes) — UI portada do protótipo React
internal/media/     # extração de áudio (ffmpeg via os/exec)
internal/stt/       # interface Provider + implementação ElevenLabs (única ativa)
internal/analysis/  # análise via LLM (usada de fato na Fase 2)
internal/db/        # SQLite (modernc.org/sqlite, WAL) + migrations goose (embed.FS)
internal/jobs/      # fila em tabela + worker único (estados, retry, idempotência)
internal/config/    # config local da máquina (paths, keyring)
prompts/            # prompts versionados (analyze-v1.md, ...)
docs/               # decisoes-tecnologia.md, fase-1-mvp.md, fase-0-validacao.md
testdata/           # fixtures sintéticas/anonimizadas
```

Fases futuras (não implementar agora, mas não bloquear no schema/desenho): análise na UI e
prompts versionados ativos; tags + FTS5; sync pull-work-push entre máquinas com snapshot via
`VACUUM INTO` (nunca copiar o arquivo do banco com conexões abertas) e manifesto sha256.

## Stack e convenções

- Go recente; preferir **stdlib**: `net/http` para APIs, `os/exec` para ffmpeg, `log/slog` para logs, `encoding/json`.
- Dependências externas só com justificativa (as aprovadas estão no `decisoes-tecnologia.md`).
- **Credenciais:** via `zalando/go-keyring` (armazenamento nativo do SO) — nunca em texto plano, nunca na pasta sincronizada. Nada hardcoded, nada commitado.
- **SQL portável** na camada de repositório: nada específico de driver (trocar modernc ↔ mattn deve ser só o import + `sql.Open`).
- **Banco nunca dentro da pasta sincronizada**; paths de vídeo no banco sempre **relativos** à raiz de armazenamento — nunca absolutos ou específicos de máquina.
- **Frontend: Svelte 5 com runes, sempre.** Nunca usar sintaxe legada do Svelte 3/4 (stores com `$:`, `export let`, etc.) — usar `$state`, `$derived`, `$effect`, `$props`. Se houver dúvida entre padrão antigo e novo, parar e perguntar.
- Código e identificadores em inglês; documentação, mensagens de erro voltadas ao usuário e textos de análise em PT-BR.
- STT sempre com diarização + timestamps por palavra + configuração multilíngue/code-switching do provedor (documentar no código a configuração usada e por quê).
- No prompt de análise: palavra em PT/ES na fala do aluno é recurso ao idioma nativo (candidata a vocabulário), **não** erro de inglês.

## Comandos

```bash
wails3 dev                # app em modo dev (hot reload do frontend)
wails3 build              # build do app
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
