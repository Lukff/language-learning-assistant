# História 3b — Importar aula manualmente (drag-and-drop): design

> Cobre a História 3b completa (`docs/fase-1-mvp.md`) e resolve o **risco técnico 2** do projeto
> ("Drag-and-drop de arquivo no Wails v3: confirmar a API de DnD nativa da versão pinada — área
> instável do alpha"). Reaproveita sem alteração o fluxo de confirmação da História 3
> (`ImportConfirmModal.svelte`, `ImportService.ConfirmImport`, `pending_imports`) — ver
> `docs/superpowers/specs/2026-07-22-historia-3-importar-aula-design.md`.

## Contexto e motivação

A varredura da pasta (História 3) cobre o caso "a pasta de armazenamento já tem aulas soltas
nela". Falta o caso "uma aula nova chegou agora (download do Cambly) e o usuário quer registrá-la
sem esperar a próxima varredura" — arrastar o arquivo pro app.

## Risco técnico 2: resolvido

O Wails v3 `v3.0.0-alpha2.117` (versão pinada) tem suporte nativo a drag-and-drop de arquivos,
implementado nas três plataformas (`webview_window_darwin.go`, `webview_window_linux.go`,
`webview_window_windows.go`) — **não é HTML5 File API**, é drop a nível de SO entregue à janela:

- `application.WebviewWindowOptions{EnableFileDrop: true}` habilita o recurso na janela.
- `win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {...})`
  recebe `e.Context().DroppedFiles() []string` — caminhos absolutos no disco, prontos pra
  `os.Open`/`os.Stat`, sem limite de tamanho de payload (ao contrário de ler bytes via `<input
  type="file">` num webview).
- O drop só é reconhecido se o cursor soltar sobre um elemento HTML marcado com o atributo
  `data-file-drop-target` — o runtime injetado pelo Wails já cuida do feedback visual (adiciona a
  classe `file-drop-target-active` nesse elemento durante o hover do drag, sem código nosso).

Fallback de file dialog (cotado como aceitável no documento de riscos) **não é necessário** — a
API nativa funciona nas três plataformas na versão pinada.

## Decisões de escopo

- **Abertura do modal:** soltar o vídeo abre `ImportConfirmModal` na hora (não fica só na lista de
  pendentes esperando clique em "Revisar").
- **Área de drop:** a tela Biblioteca inteira (`Library.svelte`), não uma zona dedicada nem a
  janela inteira do app.
- **Múltiplos arquivos:** todos são processados; cada um vira um candidato pendente, e o modal de
  confirmação abre um de cada vez, em sequência.
- **Pasta de destino da cópia:** direto na raiz de armazenamento, sem subpasta — mesmo princípio
  da História 3 de não impor estrutura de diretórios.
- **Origem já dentro da raiz de armazenamento:** não copia; registra no lugar (mesmo tratamento
  que a varredura já dá a um arquivo existente).
- **Colisão de nome no destino:** sufixo automático (`nome-2.mp4`, `nome-3.mp4`, ...), sem
  perguntar nada ao usuário — mesmo padrão já usado na renomeação pós-confirmação
  (`docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md`).
- **Extensão não reconhecida ou arquivo já importado (mesmo hash):** rejeitado com mensagem de
  erro clara, sem copiar nem hashear à toa.

## Arquitetura

```
main.go                    # EnableFileDrop: true; handler fino repassa DroppedFiles() pro service
services/import.go         # ImportService.DropImport(paths []string) []DropResult — novo método
internal/importer/
  importer.go              # dedupe/hash reaproveitados (LessonByHash, PendingExists)
  copy.go                  # novo: CopyIntoStorageRoot — cópia + resolução de colisão, sem DnD/Wails
frontend/src/lib/
  screens/Library.svelte   # data-file-drop-target na raiz; assina "import:dropped"; fila de
                            # PendingImport pra abrir o modal um de cada vez
  ImportConfirmModal.svelte # inalterado — já recebe qualquer PendingImport
```

`internal/importer/copy.go` não sabe de Wails nem de banco — só faz I/O de arquivo (cópia +
sufixo de colisão), mesmo princípio de camada fina do resto do `internal/`. `ImportService` (em
`services/`, que já conhece `storage_root`, banco e Wails events) orquestra: decide se copia ou
não, chama o hash, insere `pending_imports`, emite o evento.

### `main.go`

```go
win := app.Window.NewWithOptions(application.WebviewWindowOptions{
    // ... opções existentes ...
    EnableFileDrop: true,
})
win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
    results := importService.DropImport(e.Context().DroppedFiles())
    // DropImport já emite um evento por candidato bem-sucedido; results (erros por arquivo)
    // não tem consumidor síncrono aqui — ver "Erros" abaixo.
})
```

### `ImportService.DropImport`

```go
// DropResult é o resultado de processar um único caminho recebido do drop —
// exposto ao frontend via evento pra exibir erro por arquivo sem travar os
// demais.
type DropResult struct {
    Path  string `json:"path"`  // caminho original solto (absoluto, só pra identificar na mensagem)
    Error string `json:"error"` // vazio se deu certo
}

func (s *ImportService) DropImport(paths []string) []DropResult
```

Para cada `path` em `paths`, na ordem recebida:

1. **Extensão:** se não `.mp4` (mesma lista de `internal/importer.videoExtensions`) → `DropResult`
   com erro "tipo de arquivo não suportado (só .mp4)", segue pro próximo.
2. **Hash** (`sha256`, mesmo helper que `importer.Scan` usa — extraído pra ser reaproveitável sem
   duplicar código).
3. **Dedup:**
   - já existe uma `lesson` com esse hash → erro "esta aula já foi importada".
   - já existe um `pending_imports` com esse hash → erro "esta aula já está aguardando revisão".
4. **Posicionamento:**
   - se `path` resolve (via `filepath.Abs` + comparação de prefixo, considerando symlinks via
     `filepath.EvalSymlinks` de ambos os lados) para dentro de `storage_root` → usa o path relativo
     a `storage_root` diretamente, sem copiar.
   - senão → `importer.CopyIntoStorageRoot(path, storageRoot)`: copia o conteúdo (`io.Copy` pra um
     arquivo temporário no destino + rename atômico, não escreve direto no nome final — evita um
     candidato consumir um arquivo parcialmente copiado se a cópia falhar no meio) resolvendo
     colisão de nome com sufixo `-2`, `-3`, ... antes da extensão; retorna o path relativo
     resultante. Falha de cópia (disco cheio, permissão) → `DropResult` com o erro, **não** insere
     `pending_imports` (diferente do rename best-effort pós-confirmação — aqui a cópia é o único
     jeito de o arquivo existir rastreável, então falha tem que ser visível).
5. **`suggestedDate`** via a mesma `suggestDate(filepath.Base(path), mtime)` já usada pela
   varredura.
6. **Insere `pending_imports`** (mesma tabela, mesmo shape que `InsertPending` do scanner grava).
   Se o insert falhar depois de uma cópia bem-sucedida (erro de banco, raro), o arquivo copiado
   fica órfão em `storage_root` sem registro — não é revertido (cópia já é best-effort quanto a
   rollback). Não é um estado permanente: a próxima "Sincronizar pasta" (varredura, História 3) o
   encontra como candidato novo, já que ele está dentro da raiz de armazenamento sem hash
   conhecido. `DropResult` reporta o erro do insert normalmente.
7. **Emite evento Wails** `import:dropped` com o `PendingImport{id, path, suggestedDate}` recém-
   criado (mesmo shape que `ListPendingImports` já expõe — o frontend não precisa de um tipo novo).

### Erros: como chegam ao frontend

`DropImport` roda dentro do handler síncrono de `OnWindowEvent`, sem chamada direta do frontend
(o drop é iniciado pelo SO, não por um clique) — não há uma promise no frontend esperando o
retorno. Por isso os erros por arquivo também vão por evento, não pelo retorno de `DropResult`
usado só internamente/em teste: `DropImport` emite um evento adicional `import:drop-error` por
`DropResult` com `Error != ""`, com `{path, error}`. `Library.svelte` assina os dois eventos.

## Frontend: `Library.svelte`

- Raiz do componente ganha `data-file-drop-target`; CSS local pro estado `.file-drop-target-active`
  usando as cores do tema já importado (`colors.blue`/borda tracejada, consistente com o resto da
  UI — não a marcação verde do exemplo do Wails).
- `onMount` (junto do `loadAll` existente) assina:
  - `Events.On("import:dropped", (pending) => { pendingQueue.push(pending); maybeOpenNext(); loadPending(); })`
  - `Events.On("import:drop-error", ({path, error}) => { dropErrors = [...dropErrors, \`${path}: ${error}\`]; })`
- Fila local (`let pendingQueue: PendingImport[] = $state([])`) drena um item por vez pro mesmo
  `reviewing` que a Biblioteca já usa pra abrir `ImportConfirmModal` — se `reviewing` já está
  ocupado (usuário revisando outro candidato), o novo item espera na fila; ao fechar/confirmar,
  `maybeOpenNext()` pega o próximo. Isso cobre "múltiplos arquivos: processa todos, abre confirmação
  um de cada vez" sem duplicar lógica de exibição do modal.
- Erros de drop (`dropErrors`) aparecem no mesmo estilo de aviso que `error`/`syncMessage` já usam,
  listados (pode ser mais de um arquivo com problema no mesmo drop).

## Fora de escopo desta fatia

- Zona de drop dedicada com instrução visual ("solte aqui") — a tela inteira já reage.
- Funcionar fora da tela Biblioteca (Fila, Progresso, Detalhe) — decisão explícita, não a janela
  inteira.
- Extensões além de `.mp4` — mesma limitação da História 3, sem mudança aqui.
- Cancelar uma cópia em andamento — arquivos de aula são de minutos, não horas; não há barra de
  progresso nem cancelamento nesta fatia.

## Testes

- `internal/importer/copy_test.go`: cópia pra destino vazio; colisão de nome (sufixo `-2`,
  `-3`); origem inexistente (erro claro); cópia interrompida no meio (simulada) não deixa arquivo
  parcial com o nome final (fica só o temporário, ou nada).
- `services/import_test.go` (`DropImport`):
  - extensão não `.mp4` → `DropResult` com erro, nada no banco, nada no disco.
  - hash já existe como `lesson` → erro "já foi importada", sem cópia.
  - hash já existe como `pending_imports` → erro "já aguardando revisão", sem cópia.
  - path já dentro de `storage_root` → sem cópia (mesmo conteúdo, mesmo `os.SameFile`), só grava
    `pending_imports` com o path relativo correto.
  - path fora de `storage_root` → copiado, `pending_imports` aponta pro path relativo novo,
    arquivo original permanece intacto na origem.
  - dois paths no mesmo `DropImport` com nomes-base iguais mas hashes diferentes → o segundo grava
    com sufixo `-2`.
  - falha de cópia simulada (ex.: destino sem permissão de escrita) → `DropResult` com erro, nada
    em `pending_imports`.

## Critério de aceite (História 3b, `docs/fase-1-mvp.md`)

- [ ] Drag-and-drop nativo (Wails v3, `EnableFileDrop` + `WindowFilesDropped`) na tela Biblioteca
      abre o `ImportConfirmModal` (data pré-preenchida do nome/mtime do arquivo, tutor texto
      livre) — um modal por arquivo solto, em sequência se mais de um for solto junto.
- [ ] O vídeo é copiado pra raiz de armazenamento (sem subpasta, sufixo de colisão se necessário)
      quando ainda não está lá; se já estiver dentro da raiz de armazenamento, é registrado no
      lugar sem cópia. O banco guarda apenas o path relativo.
- [ ] Registro em `pending_imports` reaproveita a confirmação/dedup por hash já existente
      (História 3) — confirmar grava `lessons` + jobs `extract_audio`/`transcribe` como
      `pending`, sem mudança no `ConfirmImport` existente.
- [ ] Extensão não reconhecida ou arquivo já importado (mesmo hash, como `lesson` ou como
      `pending_imports`) é rejeitado com mensagem de erro clara, sem copiar nem duplicar.
