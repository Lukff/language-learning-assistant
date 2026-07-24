# História 8 — Configurações básicas (path + credencial): design

> Cobre a História 8 completa (`docs/fase-1-mvp.md`). Escopo mínimo deliberado: só o que cobre o
> gap conhecido registrado no progresso da História 2 (credencial de keyring perdida/limpa sem UI
> de recuperação) e a necessidade de reapontar a raiz de armazenamento quando o usuário move a
> pasta por conta própria (novo disco, reorganização). Seleção de provedor STT/LLM, estimativa de
> custo e o restante das Configurações completas continuam fora de escopo — Fase 5.

## Contexto e motivação

Duas lacunas conhecidas desde histórias anteriores:

1. **Credencial:** se a credencial da ElevenLabs for perdida/limpa do keyring depois do wizard de
   first-run (História 2), o app não detecta isso sozinho e não havia UI pra recadastrar — só
   apagar `config.json` e refazer o wizard inteiro.
2. **Pasta de armazenamento:** o `config.json` guarda um path absoluto e específico da máquina
   (`storage_root`). Se o usuário mover essa pasta manualmente (fora do app), hoje não há como
   reapontar sem editar o JSON à mão.

Esta história introduz uma tela de Configurações que resolve as duas, reaproveitando ao máximo o
que já existe (dialog de pasta do wizard, varredura por hash da História 3, `config.SaveSTTAPIKey`)
— nenhuma lógica de negócio nova além da checagem de "vídeo ausente".

## Decisões de escopo

- **Troca de pasta não move nem copia arquivos.** O usuário já moveu a pasta manualmente fora do
  app; o app só grava o novo `storage_root` e reconcilia o que puder.
- **Troca de pasta nunca é bloqueada.** Mesmo que vídeos não sejam encontrados na pasta nova, a
  troca é aceita — os ausentes só ficam sinalizados, nunca impedem o usuário de seguir em frente.
- **Reconciliação por hash reaproveita a varredura da História 3** (`importer.Scan`): arquivo
  encontrado em path diferente (mesmo hash) tem o `video_path` atualizado automaticamente — cobre
  o caso de vídeos renomeados na mudança de pasta. Efeito colateral aceito e desejado: vídeos novos
  encontrados na pasta nova (sem lesson correspondente) também viram candidatos pendentes, mesmo
  comportamento de "Sincronizar pasta".
- **"Vídeo ausente" é sempre recalculado, nunca persistido.** Mesmo padrão do status
  processando/pronta/erro (`internal/db/lesson_status.go`, `deriveStatus`) — derivado a cada
  leitura, nunca uma coluna gravada à parte. Autocura: se o arquivo reaparecer no path esperado
  (por exemplo, outra rodada de "Sincronizar pasta" resolveu por hash, ou o usuário devolveu o
  arquivo manualmente), o aviso some sozinho na próxima vez que a Biblioteca carregar.
- **Credencial nunca é exibida** — só um status booleano ("configurada" / "não configurada"); o
  campo de (re)cadastro está sempre disponível, sem pré-preencher nada.
- **Configurações é uma tela própria** (rota, não modal), acessada por um ícone de engrenagem no
  `Header` — não entra na lista de navegação da Sidebar (não é usada no dia a dia como
  Biblioteca/Fila).
- **Resumo da varredura pós-troca de pasta aparece como texto fixo na própria tela de
  Configurações** (mesmo formato `new/updated/skipped/errors` que a Biblioteca já usa pra
  "Sincronizar pasta") — sem toast, sem evento novo do Wails (é uma única tela, sem necessidade de
  notificar outras abas).

## Arquitetura

```
services/
  settings.go            # NOVO: SettingsService
  storage_folder.go       # NOVO: helper compartilhado de dialog+validação de escrita
  setup.go                 # ChooseStorageFolder passa a delegar pro helper compartilhado
  library.go               # ListLessons/GetLesson ganham VideoMissing (checagem de existência)
  import.go                # dbRepo (não-exportado) é reaproveitado por SettingsService, mesmo pacote
main.go                    # registra SettingsService; passa storageRoot também pra LibraryService
frontend/src/lib/
  Header.svelte            # ícone de engrenagem
  screens/Settings.svelte  # NOVO
  screens/Library.svelte   # badge "vídeo ausente" por lesson
App.svelte                 # nova rota { screen: "settings" }
```

Nenhum pacote novo em `internal/` — `internal/importer.Scan` e `internal/config` já cobrem tudo
que a lógica de negócio precisa; `services/` só orquestra.

### `services/storage_folder.go` (novo, extraído de `setup.go`)

```go
// chooseStorageFolder abre o dialog nativo de escolha de pasta e valida que
// ela é gravável. Retorna path vazio (sem erro) se o usuário cancelar.
// Compartilhado por SetupService (wizard) e SettingsService (troca de pasta).
func chooseStorageFolder(title string) (string, error)

func isDirWritable(dir string) error // movido de setup.go, sem mudança de comportamento
```

`SetupService.ChooseStorageFolder` passa a chamar `chooseStorageFolder("Escolha a pasta onde as
aulas ficarão guardadas")`; `SettingsService.ChooseStorageFolder` chama a mesma função com um
título ligeiramente diferente ("Escolha a nova pasta — os arquivos já devem estar lá dentro").

### `services/settings.go` (novo)

```go
// SettingsService cobre a tela de Configurações (História 8): ver/trocar a
// raiz de armazenamento e (re)cadastrar a credencial do provedor STT.
type SettingsService struct {
    conn        *sql.DB
    storageRoot func() (string, error)
}

func NewSettingsService(conn *sql.DB, storageRoot func() (string, error)) *SettingsService

// GetStorageRoot retorna a storage_root configurada atualmente.
func (s *SettingsService) GetStorageRoot() (string, error)

// ChooseStorageFolder abre o dialog nativo (mesma validação de escrita do
// wizard) e retorna o path escolhido, sem gravar nada ainda.
func (s *SettingsService) ChooseStorageFolder() (string, error)

// ChangeStorageFolder grava newRoot em config.json (sempre, mesmo que a
// varredura a seguir encontre problemas) e roda a mesma reconciliação por
// hash da História 3. Nunca bloqueia a troca. ScanSummary é o mesmo tipo já
// definido em import.go (services.ScanSummary) — mesmo pacote, reaproveitado
// sem duplicar o formato de resumo.
func (s *SettingsService) ChangeStorageFolder(newRoot string) (ScanSummary, error)

// HasSTTCredential indica se há uma credencial gravada no keyring, sem
// revelar o valor. false (sem erro) se simplesmente não configurada ainda
// (keyring.ErrNotFound); erro só em falha real de acesso ao keyring.
func (s *SettingsService) HasSTTCredential() (bool, error)

// SaveSTTAPIKey grava/sobrescreve a credencial — delega direto pra
// config.SaveSTTAPIKey.
func (s *SettingsService) SaveSTTAPIKey(apiKey string) error
```

`ChangeStorageFolder`:

```go
func (s *SettingsService) ChangeStorageFolder(newRoot string) (ScanSummary, error) {
    if newRoot == "" {
        return ScanSummary{}, fmt.Errorf("pasta de armazenamento não pode ser vazia")
    }
    if err := isDirWritable(newRoot); err != nil {
        return ScanSummary{}, err
    }
    if err := config.Save(&config.AppConfig{StorageRoot: newRoot}); err != nil {
        return ScanSummary{}, fmt.Errorf("gravar configuração: %w", err)
    }
    sum, err := importer.Scan(newRoot, &dbRepo{conn: s.conn})
    if err != nil {
        // storage_root já foi trocado nesse ponto — decisão consciente (ver
        // "Tratamento de erros"): reconciliação é best-effort, a troca em si
        // não deve ser revertida por uma falha de varredura.
        return ScanSummary{}, err
    }
    return ScanSummary{New: sum.New, Updated: sum.Updated, Skipped: sum.Skipped, Errors: sum.Errors}, nil
}
```

`HasSTTCredential`:

```go
func (s *SettingsService) HasSTTCredential() (bool, error) {
    _, err := config.GetSTTAPIKey()
    if err == nil {
        return true, nil
    }
    if errors.Is(err, keyring.ErrNotFound) {
        return false, nil
    }
    return false, fmt.Errorf("não foi possível acessar o gerenciador de credenciais do sistema (verifique se o gnome-keyring/kwallet está rodando): %w", err)
}
```

`config.GetSTTAPIKey` hoje envolve o erro do `keyring` com `fmt.Errorf("...: %w", err)` — como já
usa `%w`, `errors.Is(err, keyring.ErrNotFound)` continua funcionando através do wrap.

### `services/library.go` (estendido)

```go
type LibraryService struct {
    conn        *sql.DB
    storageRoot func() (string, error) // NOVO — mesmo resolver que o worker/middleware já usam
}

func NewLibraryService(conn *sql.DB, storageRoot func() (string, error)) *LibraryService

type Lesson struct {
    // ... campos existentes ...
    VideoMissing bool `json:"videoMissing"` // NOVO
}
```

`ListLessons`/`GetLesson` passam a chamar um helper interno:

```go
// videoMissing indica se o arquivo de vídeo de uma lesson não é encontrado
// na storage_root atual. Qualquer erro de os.Stat (não só "não existe") é
// tratado como ausente — resiliência: nunca deixa a Biblioteca quebrar por
// causa disso, e não vale a pena diferenciar "ausente" de "sem permissão"
// nesta fatia.
func (s *LibraryService) videoMissing(videoPath string) bool {
    root, err := s.storageRoot()
    if err != nil {
        return true
    }
    _, err = os.Stat(filepath.Join(root, filepath.FromSlash(videoPath)))
    return err != nil
}
```

### `main.go`

```go
importService := services.NewImportService(conn)
libraryService := services.NewLibraryService(conn, storageRoot) // storageRoot já existe (closure sobre config.Load)

Services: []application.Service{
    application.NewService(services.NewSetupService()),
    application.NewService(importService),
    application.NewService(libraryService),
    application.NewService(services.NewQueueService(conn)),
    application.NewService(services.NewSettingsService(conn, storageRoot)), // NOVO
},
```

## Frontend

### `Header.svelte`

Ícone de engrenagem (⚙) alinhado à direita do cabeçalho existente; `onclick` chama uma prop
`onOpenSettings: () => void` (mesmo padrão de callback que `Sidebar` já usa com `onNavigate`).

### `App.svelte`

```ts
type Route =
  | { screen: "library" }
  | { screen: "lesson-detail"; lessonId: number }
  | { screen: "progress" }
  | { screen: "queue" }
  | { screen: "settings" }; // NOVO
```

`<Header onOpenSettings={() => (route = { screen: "settings" })} />`; `Sidebar` continua recebendo
só `route.screen` mapeado pra `"library"` quando a rota é `"lesson-detail"` ou `"settings"` (mesmo
tratamento que já existe pra `"lesson-detail"`), já que nenhuma das duas é um item de nav.

### `Settings.svelte` (novo)

Duas seções, no mesmo estilo visual das demais telas (`theme.ts`, cards com `colors.surface2`):

1. **Armazenamento**
   - Texto com a `storage_root` atual (`SettingsService.GetStorageRoot`, carregado no `onMount`).
   - Botão "Trocar pasta" → `ChooseStorageFolder()` → se path não vazio, chama
     `ChangeStorageFolder(path)`; botão mostra estado de carregamento enquanto a promise não
     resolve (varredura pode levar alguns segundos, mesma ordem de grandeza documentada na
     História 3: ~1-3s por vídeo).
   - Resultado (sucesso ou erro) fica em texto fixo abaixo do botão: sucesso mostra
     `N novas, M atualizadas, S puladas, E erros`; erro mostra a mensagem.

2. **Credencial do provedor STT**
   - Status carregado no `onMount` via `HasSTTCredential()`: "Credencial configurada" ou "Nenhuma
     credencial configurada" (ou mensagem de erro se a chamada falhar).
   - Campo `<input type="password">` + botão "Salvar", sempre visível e vazio (nunca pré-
     preenchido); ao salvar com sucesso, atualiza o status pra "Credencial configurada" e limpa o
     campo; erro aparece inline, mesmo padrão do passo 2 do `SetupWizard`.

### `Library.svelte` / `LessonDetail.svelte`

Lesson com `videoMissing: true` ganha um badge curto ("vídeo não encontrado na pasta atual"),
visualmente na mesma família do aviso de "erro" (cor de alerta), mas como um indicador
independente do `status` do pipeline — uma aula `pronta` (transcrição ok) pode estar com
`videoMissing: true` ao mesmo tempo, e o badge aparece nos dois casos sem interferir no texto do
`status` existente.

## Fluxo de dados

**Trocar pasta:**

```
Settings.svelte: clique "Trocar pasta"
  → SettingsService.ChooseStorageFolder() (dialog nativo + valida escrita)
  → usuário confirma (path não vazio)
  → SettingsService.ChangeStorageFolder(novaPasta)
        → isDirWritable(novaPasta)
        → config.Save({storage_root: novaPasta})        // sempre grava, mesmo se a varredura a seguir falhar
        → importer.Scan(novaPasta, &dbRepo{conn})
              → hash já conhecido, path novo  → db.UpdateLessonPath (renomeado/movido)
              → hash desconhecido             → vira pending_import (mesmo fluxo de sempre)
              → stat bate (inalterado)        → pulado
        ← ScanSummary { new, updated, skipped, errors }
  ← texto fixo na tela com o resumo
```

**Credencial:**

```
Settings.svelte: onMount → HasSTTCredential() → mostra status
Settings.svelte: clique "Salvar" → SaveSTTAPIKey(apiKey) → config.SaveSTTAPIKey (keyring)
```

**Vídeo ausente (recalculado a cada carregamento da Biblioteca, independente de Configurações):**

```
Library.svelte: onMount/refresh → LibraryService.ListLessons(filter)
  → db.ListLessonsWithStatus(...)                        // inalterado
  → para cada lesson: os.Stat(storageRoot + "/" + video_path)
        existe     → VideoMissing = false
        não existe → VideoMissing = true
  ← []Lesson (com VideoMissing)
```

Nenhum evento novo do Wails — tudo é request/response direto, mesmo padrão dos demais serviços;
não há múltiplas telas/abas abertas simultaneamente que precisem ser notificadas.

## Tratamento de erros

- **Dialog cancelado** (`ChooseStorageFolder` retorna vazio): `Settings.svelte` não chama
  `ChangeStorageFolder`, sem erro.
- **Pasta escolhida sem permissão de escrita**: `isDirWritable` recusa antes de gravar
  `storage_root` — "pasta sem permissão de escrita".
- **Erro ao gravar `config.json`**: `ChangeStorageFolder` retorna erro, `storage_root` antigo
  continua valendo (o `config.Save` que falhou não chega a substituir o arquivo).
- **Erro durante a varredura** (`importer.Scan` retorna `err != nil`, ex.: `root` não é mais
  acessível no meio da varredura): nesse ponto `storage_root` **já foi trocado** — decisão
  consciente, já que a pasta em si foi validada como gravável antes; a reconciliação é
  best-effort e uma falha nela não deveria impedir o usuário de já ter apontado pra pasta certa.
  Erro aparece no texto fixo da tela.
- **Erros por arquivo dentro da varredura** (arquivo ilegível, permissão): não abortam — já é o
  comportamento de `importer.Scan` (conta em `Errors`, segue os demais); aparecem no resumo.
- **Falha ao gravar credencial no keyring** (Secret Service indisponível no Linux, risco 3): mesma
  mensagem já usada no wizard (`services/setup.go`).
- **Falha ao ler status da credencial** (`HasSTTCredential`): distingue "não configurada"
  (`keyring.ErrNotFound`, não é erro) de falha real de acesso (mensagem de erro, mesma do wizard).
- **`os.Stat` do vídeo falha por motivo diferente de "não existe"** (ex. permissão): tratado como
  `VideoMissing = true` também — resiliência; não vale a pena diferenciar o motivo nesta fatia.
- **Princípio de resiliência mantido:** nada nesta história impede assistir a uma aula cujo vídeo
  está de fato presente; `VideoMissing` é só informativo, nunca bloqueia o Detalhe.

## Fora de escopo desta história

- Mover ou copiar arquivos automaticamente ao trocar de pasta.
- Seleção de provedor STT/LLM, estimativa de custo, qualquer outro campo de configuração além de
  `storage_root` e da credencial (Fase 5, `docs/fase-1-mvp.md`).
- Validar `raw_json_path` (transcrições brutas) contra a pasta nova — só `video_path` é checado;
  perder o JSON bruto não impede assistir à aula nem afeta a transcrição já persistida em
  `transcripts.utterances`.
- Editar `storage_root` digitando o path manualmente (só via dialog nativo, mesmo padrão do
  wizard) — evita paths inválidos digitados à mão.
- Cancelar uma varredura em andamento.

## Testes

**`services/settings_test.go`** (novo, mesmo padrão dos demais em `services/`):
- `ChangeStorageFolder`: banco em `t.TempDir()` + duas pastas temporárias simulando origem/destino
  já movidos; lesson com hash existente aparecendo em path diferente na pasta nova tem
  `video_path` atualizado; `config.Load()` reflete a pasta nova depois da chamada.
- `ChangeStorageFolder` com pasta sem permissão de escrita: retorna erro, `storage_root` antigo
  preservado (config não é tocado).
- `HasSTTCredential`: fake `secretStore` (mesmo padrão de `credentials_test.go`) cobrindo os três
  casos — configurada, não configurada (`ErrNotFound`), erro real de acesso.
- `SaveSTTAPIKey`: teste fino confirmando que delega pra `config.SaveSTTAPIKey`.

**`services/library_test.go`** (estendido):
- `ListLessons`/`GetLesson` com `storageRoot` de teste (`t.TempDir()`): lesson cujo `video_path`
  existe no disco (arquivo vazio já basta pro `os.Stat`) → `VideoMissing: false`; lesson cujo
  arquivo não existe → `VideoMissing: true`.

**`internal/importer`**: nenhum teste novo — `Scan` já é coberto pela suíte da História 3 e não
muda de comportamento, só passa a ser chamado de mais um lugar (`SettingsService`, além de
`ImportService.ScanFolder`).

**Verificação manual** (mesmo padrão registrado nas histórias anteriores, pendente de janela real
em Windows/Linux): abrir Configurações pelo ícone do Header; trocar a pasta de armazenamento de
verdade movendo um vídeo com nome diferente e confirmar que a Biblioteca reflete o `video_path`
novo; apagar um vídeo do disco e confirmar que a Biblioteca mostra "vídeo ausente" sem quebrar a
tela; recadastrar a credencial e confirmar que o status muda pra "configurada".

## Critérios de aceite (de `docs/fase-1-mvp.md`, História 8)

- [ ] Tela de Configurações acessível por um ícone no Header, mostra a raiz de armazenamento
      configurada e permite trocá-la (dialog nativo + validação de escrita), sem mover arquivos —
      o usuário já os moveu manualmente. A troca nunca é bloqueada por vídeos não encontrados.
- [ ] Trocar a pasta roda a mesma reconciliação por hash da História 3: vídeos com nome diferente
      na pasta nova têm o `video_path` atualizado automaticamente; vídeos cujo hash não é
      encontrado na pasta nova ficam sinalizados como "vídeo ausente" na Biblioteca (recalculado a
      cada carregamento, nunca uma coluna persistida) até o arquivo aparecer de novo no path
      esperado.
- [ ] Campo pra (re)cadastrar a credencial do provedor STT (ElevenLabs) via `go-keyring`,
      reaproveitando `config.SaveSTTAPIKey`; a tela mostra se já há credencial configurada (sem
      revelar o valor) — cobre o gap conhecido da História 2.
- [ ] Sem seleção de provedor, estimativa de custo ou qualquer outra opção de configuração — isso
      é Fase 5.
