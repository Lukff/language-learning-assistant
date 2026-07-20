# História 1 — Esqueleto do app: design

> Spec da primeira fatia de implementação da Fase 1 (`docs/fase-1-mvp.md`, História 1).
> Objetivo: o app Wails v3 + Svelte 5 abre com a navegação e a identidade visual definidas,
> sem nenhuma lógica de negócio ainda (DB, importação, jobs ficam para as próximas histórias).

## Contexto

O projeto está hoje na Fase 0 encerrada (CLI `cmd/spike`, packages `media`/`stt`/`analysis`
definitivos) e sem nenhum código de UI. Existe um protótipo React validado
(`docs/prototipo-app-aulas.jsx`) que define a identidade visual (tema escuro, paleta de cores,
fontes Sora/Inter/JetBrains Mono, sidebar com nav Biblioteca/Progresso/Fila, header com indicador
de sync) a ser **portado para Svelte 5, não redesenhado**.

O CLI `wails3` não estava instalado na máquina; foi instalado via
`go install github.com/wailsapp/wails/v3/cmd/wails3@latest` (versão resultante:
`v3.0.0-alpha2.117` — esta é a versão que vai pinada no `go.mod`, conforme `CLAUDE.md`).

## Decisões de escopo já fechadas

- **Header sem SyncPill:** sync é fora de escopo até a Fase 4; o header desta história fica
  vazio/minimalista, sem indicador de sincronização. Adicionar quando a Fase 4 chegar.
- **Sem dados fake:** Biblioteca e Fila mostram empty state real (nenhuma aula/job ainda existe,
  não há DB nesta história). Progresso mostra um placeholder fixo, já que só chega na Fase 3.

## Abordagem de scaffolding

`wails3 init` gera um projeto standalone (`go.mod`, `go.sum`, `README.md`, `.gitignore` próprios)
— confirmado com um `init` de teste em diretório temporário fora do repo (removido depois). Rodar
isso direto na raiz do repositório arriscaria sobrescrever o `go.mod` existente (módulo
`assistente-idiomas`, com `cmd/spike` e os packages `internal/media|stt|analysis` já em uso).

Processo:
1. `wails3 init -n assistente-idiomas -d <tmp> -t svelte` em diretório temporário, para obter os
   artefatos gerados (template `svelte` = Svelte + TypeScript + Vite; testado gerando
   **Svelte 5.46.4**, já compatível com a regra de runes do `CLAUDE.md` — sem downgrade
   necessário).
2. Copiar para o repo, sem sobrescrever nada existente: `frontend/` (estrutura Vite/Svelte),
   `build/` (ícones, config de empacotamento por plataforma), `Taskfile.yml`.
3. **Não copiar** o `go.mod`/`go.sum` gerados. Em vez disso, `go get
   github.com/wailsapp/wails/v3@v3.0.0-alpha2.117` no `go.mod` existente — preserva o módulo
   `assistente-idiomas` e as dependências já presentes.
   - Efeito colateral esperado: o directive `go` do nosso `go.mod` sobe de `1.23.6` para o mínimo
     exigido pelo wails v3 alpha2.117 (`1.25.0`, confirmado no `go.mod` gerado no teste). O
     toolchain Go local (`1.23.6`) faz auto-download da versão correta (`GOTOOLCHAIN=auto`, já
     confirmado no ambiente) na primeira `go build`/`wails3 build`.
4. `main.go` **escrito à mão**, não o gerado por padrão — o template inclui um `GreetService` e um
   evento `time` de demonstração que não fazem sentido aqui. Contém só a casca: cria a janela,
   aponta o asset handler para `frontend/dist` embutido. Nenhum import de `internal/` além do que
   for estritamente necessário para abrir a janela (nesta história, nenhum — isso começa na
   História 2 com config/DB).

## Estrutura de diretórios resultante

```
main.go             # casca Wails v3 (nova)
frontend/            # Svelte 5 + TypeScript + Vite (novo, do template)
  src/
    App.svelte        # shell: sidebar + header + área de conteúdo
    lib/
      Sidebar.svelte
      Header.svelte
      screens/
        Library.svelte    # empty state
        Queue.svelte       # empty state
        Progress.svelte    # placeholder fixo
    theme.ts            # paleta de cores + fontes (equivalente ao objeto C/F do protótipo)
build/                # ícones e config de empacotamento (novo, do template)
cmd/spike/            # inalterado
internal/             # inalterado
```

## Frontend — componentes

- **`theme.ts`**: constantes de cor (`bg`, `surface`, `surface2`, `line`, `text`, `mut`, `blue`,
  `amber`, `green`, `red`) e famílias de fonte (`display` = Sora, `body` = Inter, `mono` =
  JetBrains Mono), extraídas 1:1 dos objetos `C` e `F` do protótipo. Fontes **auto-hospedadas**
  como arquivos estáticos em `frontend/src/assets/fonts/` (o template já traz a Inter; Sora e
  JetBrains Mono são adicionadas do mesmo jeito) — diferente do `@import` de Google Fonts do
  protótipo, porque o app não deve depender de rede para render básico.
- **`Sidebar.svelte`**: logo/nome do app, 3 itens de nav (Biblioteca/Progresso/Fila) com ícone e
  destaque visual do item ativo. Sem o rodapé "Raiz do Drive" (não existe config de storage root
  ainda — História 2). Sem badge de contagem na Fila (não há jobs reais ainda — Histórias 4/7).
- **`Header.svelte`**: vazio nesta história (decisão de escopo acima). Existe como componente
  próprio para não exigir refatoração de layout quando o conteúdo real (sync, breadcrumbs, etc.)
  chegar em fase futura.
- **`Library.svelte` / `Queue.svelte`**: empty state simples (texto centralizado, tom neutro:
  "Nenhuma aula importada ainda" / "Nada na fila no momento"). Sem busca, sem botão de importar
  ainda (também Histórias futuras) — mas o layout de página (padding, largura máxima) já segue o
  protótipo para não precisar reajustar depois.
- **`Progress.svelte`**: placeholder fixo, ex.: "Progresso chega na Fase 3."
- **Navegação:** um `$state` de tela ativa (`"library" | "progress" | "queue"`) no componente
  `App.svelte` raiz, trocado pelos cliques na sidebar. Sem router externo (SvelteKit ou similar)
  — 3 telas fixas sem parâmetros de URL não justificam a dependência.

## Backend (Go) — este slice

Só o mínimo para abrir a janela:
- `main.go`: `application.New` + uma janela (`Width`/`Height` seguindo o protótipo, tema escuro
  refletido em `BackgroundColour`), `Assets` apontando para `frontend/dist` embutido via
  `embed.FS`.
- Nenhum `Service`/binding Go↔JS nesta história (não há dados para expor ainda).

## Fluxo de dados

Nenhum — todas as telas são estáticas/placeholder. O único estado é client-side (tela ativa na
sidebar).

## Tratamento de erros

Nenhum cenário de erro de negócio nesta história (não há chamadas a API, DB ou arquivo). O único
ponto de atenção é a build em si: `wails3 dev`/`wails3 build` devem completar sem erro nas duas
máquinas (Windows e Linux) — é o critério de aceite "compilando e abrindo janela".

## Testes / verificação

Sem testes automatizados de componente Svelte nesta história (não há framework de teste de UI
configurado no projeto ainda, e seria over-engineering para telas estáticas). Verificação:
- `wails3 dev` abrindo janela e navegação funcionando visualmente, nas máquinas Windows e Linux.
- `go vet ./...` limpo.
- Conferência visual manual do tema (cores, fontes) contra o protótipo.

## Fora de escopo desta história

DB, config de storage root, keyring, importação de aula, fila de jobs real, dados reais em
qualquer tela, drag-and-drop, vídeo. Tudo isso é Histórias 2 em diante (`docs/fase-1-mvp.md`).

## Critérios de aceite (de `docs/fase-1-mvp.md`, História 1)

- [ ] Projeto Wails v3 (versão pinada no `go.mod`) + Svelte 5 (runes) compilando e abrindo janela
      nas máquinas Windows e Linux.
- [ ] Camada fina respeitada: `internal/` sem imports de Wails; o app referencia os packages da
      Fase 0 sem copiá-los (não se aplica ainda diretamente nesta história, já que nenhum package
      de fase 0 é usado no esqueleto — mas nenhum import futuro deve quebrar essa regra).
- [ ] Sidebar com Biblioteca / Progresso (placeholder) / Fila, e cabeçalho — portados do
      protótipo React (tema escuro, Sora/Inter/JetBrains Mono).
- [ ] Convenções Svelte 5 do `CLAUDE.md` aplicadas (nenhuma sintaxe legada).
