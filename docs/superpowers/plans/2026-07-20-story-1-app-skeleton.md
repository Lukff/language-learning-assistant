# Story 1 — App Skeleton: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the Wails v3 + Svelte 5 app shell — window opens, sidebar navigates between
Library / Progress / Queue, visual identity (dark theme, Sora/Inter/JetBrains Mono) matches
`docs/prototipo-app-aulas.jsx` — with zero business logic (no DB, no data, no imports).

**Architecture:** Scaffold the Wails v3 `svelte` template into a scratch directory, then merge
selectively into the existing repo (preserving the existing Go module, `cmd/spike/`, `internal/`).
`main.go` at repo root is hand-written (thin shell only). Frontend is Svelte 5 runes components
under `frontend/src/`, styled with a `theme.ts` constants module (colors + font stacks) mirroring
the prototype's `C`/`F` objects, applied via inline `style` attributes — same approach the
prototype uses, translated 1:1 instead of introducing a CSS-variable layer that doesn't exist yet
in either source.

**Tech Stack:** Wails v3 `v3.0.0-alpha2.117` (Go), Svelte 5 (runes) + TypeScript + Vite, `@fontsource/sora` `@fontsource/inter` `@fontsource/jetbrains-mono` (self-hosted webfonts, no runtime network calls).

## Global Constraints

- Wails v3 version is **pinned** in `go.mod`: `v3.0.0-alpha2.117` (the version installed and
  build-tested during design). Do not `go get @latest` later without a deliberate upgrade task.
- `internal/` packages never import anything under `github.com/wailsapp/wails/v3` — the thin-layer
  principle is non-negotiable (`CLAUDE.md`).
- Svelte 5 runes only (`$state`, `$derived`, `$props`) — no Svelte 3/4 legacy syntax (`export let`,
  `$:` reactive statements, stores-as-props).
- No fake/sample data anywhere in this story — Library and Queue show real empty states; Progress
  shows a fixed "chega na Fase 3" placeholder.
- No SyncPill / sync UI in the header this story — header stays empty.
- Fonts are self-hosted (npm packages bundled by Vite) — never a runtime `@import` from
  `fonts.googleapis.com`.
- Every task's Go changes must leave `go vet ./...` clean.
- Implementers **stage** (`git add`) their changes at the end of each task but do **not** commit —
  the user controls commit timing (established project convention; see
  `.superpowers/sdd/progress.md` history). If commit messages are ever needed, they follow the
  one-line semantic format (`feat: ...`, `chore: ...`) from `CLAUDE.md`.

---

### Task 1: Scaffold the Wails v3 project into the repo

**Files:**
- Create: `main.go` (repo root — **template's demo content, unmodified**: registers
  `GreetService` and the `time` event, exactly as `wails3 init` generates it. This is
  intentional, not an oversight: the template's demo `App.svelte` (left in place this task)
  imports a `GreetService` binding, which only exists if the Go service that generates it is
  registered. Task 5 replaces this file with the real thin shell, in the same task where it
  replaces `App.svelte` — the two changes are coupled and must land together.)
- Create: `greetservice.go` (repo root — template's demo content, unmodified. Deleted in Task 5.)
- Create: `frontend/` (entire tree, from the wails3 `svelte` template, unmodified in this task)
- Create: `build/` (entire tree, from the wails3 `svelte` template, unmodified in this task)
- Create: `Taskfile.yml` (repo root, from the wails3 `svelte` template, unmodified in this task)
- Modify: `go.mod`, `go.sum` (add the wails3 dependency)
- Modify: `.gitignore` (add frontend/build ignores)

**Interfaces:**
- Consumes: nothing (first task).
- Produces: a buildable Wails v3 app skeleton at the repo root (`main.go`, `greetservice.go`,
  `frontend/`, `build/`, `Taskfile.yml`) that Task 2 onward will style and wire up. The demo
  `frontend/src/App.svelte` **and** the demo `main.go`/`greetservice.go` from the template are
  left in place, unmodified, after this task — they build and run as the stock Wails demo.
  Task 5 replaces `App.svelte`, replaces `main.go` with the thin shell, and deletes
  `greetservice.go`, all together (removing the `GreetService` binding and its only consumer in
  the same step keeps the build green at every task boundary).

- [ ] **Step 1: Generate the template into a scratch directory (outside the repo)**

Run (from anywhere; the path must be under `/mnt/c/...` so both the WSL shell and the Windows
`wails3.exe` binary can see it):

```bash
SCAFFOLD_DIR="/mnt/c/Users/lukff/AppData/Local/Temp/wails-scaffold"
rm -rf "$SCAFFOLD_DIR" && mkdir -p "$SCAFFOLD_DIR"
cd "$SCAFFOLD_DIR" && cmd.exe /c "wails3 init -n assistente-idiomas -d . -t svelte -mod assistente-idiomas -q"
```

Do **not** add `-productname`/`-productdescription`/`-productidentifier`/`-productcompany` flags
to this command, even though `wails3 init -help` lists them — confirmed during design (a real
build attempt, not a guess) that passing multi-word values through this bash → `cmd.exe /c "..."`
→ Windows-argv chain corrupts them: every value gets truncated at its first space with a stray
literal `"` left attached, and it silently corrupts the generated `build/windows/wails.exe.manifest`
badly enough to break the build with an XML syntax error (plus wrong-but-not-broken data in
`build/windows/info.json`, both `darwin/Info*.plist`, `linux/desktop`, `linux/nfpm/nfpm.yaml`).
Product metadata is set safely in Step 2a below instead, by editing `build/config.yml` directly
(plain text edit, no shell requoting involved) and regenerating the assets from it.

Expected: `✓ Project 'assistente-idiomas' created successfully.` and
`$SCAFFOLD_DIR/assistente-idiomas/` contains `frontend/`, `build/`, `Taskfile.yml`, `main.go`,
`go.mod`, `go.sum`, `README.md`, `.gitignore`, `greetservice.go`.

- [ ] **Step 2: Copy the frontend/build/main.go/greetservice.go scaffold into the repo (do NOT copy go.mod/go.sum/README/.gitignore — those are merged separately in Step 4, not replaced)**

Run from the repo root (`/mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas`):

```bash
SRC="/mnt/c/Users/lukff/AppData/Local/Temp/wails-scaffold/assistente-idiomas"
cp -r "$SRC/frontend" ./frontend
cp -r "$SRC/build" ./build
cp "$SRC/Taskfile.yml" ./Taskfile.yml
cp "$SRC/main.go" ./main.go
cp "$SRC/greetservice.go" ./greetservice.go
```

Expected: `ls frontend build Taskfile.yml main.go greetservice.go` all exist; `git status` shows
`frontend/`, `build/`, `Taskfile.yml`, `main.go`, `greetservice.go` as untracked; `cmd/`,
`internal/`, `docs/` are unchanged (`git status` shows no modification to them from this step).
`main.go` here is the template's demo content (registers `GreetService`, emits a `time` event
every second) — this is intentional (see Task 1's Files/Interfaces note above), not a mistake to
fix in this task.

- [ ] **Step 2a: Set product metadata in `build/config.yml`, then regenerate the build assets from it**

Edit `build/config.yml` (plain text edit — do not go through `cmd.exe`/shell quoting for this).
The `info:` block currently reads:

```yaml
info:
  companyName: "My Company" # The name of the company
  productName: "My Product" # The name of the application
  productIdentifier: "com.mycompany.myproduct" # The unique product identifier
  description: "A program that does X" # The application description
```

Change those four values (leave `copyright`, `comments`, `version`, and everything else in the
file untouched):

```yaml
info:
  companyName: "Lucas Fernandes" # The name of the company
  productName: "Assistente de Idiomas" # The name of the application
  productIdentifier: "dev.lukff.assistente-idiomas" # The unique product identifier
  description: "Arquivo e análise de aulas de inglês do Cambly" # The application description
```

Then regenerate the platform build assets (Windows manifest/info.json, macOS plists, Linux
desktop file/nfpm.yaml) from the corrected config:

```bash
cd /mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas
cmd.exe /c "wails3 task common:update:build-assets"
```

Expected: `Successfully updated build assets in ...\build`. Verify
`build/windows/wails.exe.manifest` line 3 now reads
`name="dev.lukff.assistente-idiomas"` (plain value, no stray quotes) —
`grep -c '""' build/windows/wails.exe.manifest build/windows/info.json` should print `0` for
both files.

- [ ] **Step 3: Add the ignore rules for generated frontend/build output**

Append to `.gitignore` (repo root):

```
.task
bin/
frontend/dist
frontend/node_modules
frontend/bindings
```

Note: `bin` and `frontend/dist`/`frontend/node_modules` match the wails3 template's own
`.gitignore`; `frontend/bindings` is added because the template's default `.gitignore` omits it
even though `wails3 build` regenerates it from Go services (confirmed by a test build during
design — Wails only ships `//go:embed all:frontend/dist` output for the `assets` embed, bindings
are a separate generated tree).

- [ ] **Step 4: Add the wails3 Go dependency to the existing `go.mod`**

```bash
go.exe get github.com/wailsapp/wails/v3@v3.0.0-alpha2.117
go.exe mod tidy
```

Expected: `go.mod` gains `require github.com/wailsapp/wails/v3 v3.0.0-alpha2.117` (plus its
transitive indirect requires); the `go` directive is bumped from `1.23.6` to at least `1.25.0`
(the minimum wails3 alpha2.117 requires — confirmed during design). `go.sum` is populated. This
may trigger an automatic download of the go1.25 toolchain (`GOTOOLCHAIN=auto`) the first time —
that's expected, not an error.

- [ ] **Step 5: Verify the full build pipeline (npm install, vite build, go build, embed) — stock demo, unmodified**

```bash
cd /mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas
cmd.exe /c "wails3 build"
```

Expected: ends with `task: [windows:build:native] go build ...` and produces
`bin/assistente-idiomas.exe`. `npm install` output and a Vite build summary appear along the way;
warnings about `uname`/`tail` not found and an a11y lint note on the demo `App.svelte` are
expected noise from the Windows toolchain and do not fail the build. This is the stock template
demo (Greet button, GreetService binding, ticking clock) building end-to-end — confirmed to work
this way during design (a full `wails3 build` test with the unmodified template succeeded). If
this step fails, stop and escalate — do not modify `App.svelte`, `main.go`, or `greetservice.go`
to work around it; those changes belong to Task 5, not this task.

- [ ] **Step 6: `go vet` check**

```bash
go.exe vet ./...
```

Expected: no output (clean).

- [ ] **Step 7: Stage (do not commit — user controls commit timing)**

```bash
git add main.go greetservice.go go.mod go.sum .gitignore Taskfile.yml build frontend
git status
```

Expected: `git status` shows `frontend/node_modules`, `frontend/dist`, `frontend/bindings`, `bin/`
as ignored (not staged) — only source/config files staged. Do not run `git commit`.

---

### Task 2: Theme constants and self-hosted fonts

**Files:**
- Create: `frontend/src/lib/theme.ts`
- Create: `frontend/src/app.css`
- Modify: `frontend/src/main.ts` (import the new stylesheet)
- Modify: `frontend/package.json` (adds `@fontsource/*` dependencies — via `npm install`)

**Interfaces:**
- Consumes: nothing from Task 1 directly (parallel-safe with Tasks 3/4, but written sequentially
  here for a linear plan).
- Produces: `colors` and `fonts` exported from `frontend/src/lib/theme.ts`, used by every
  component in Tasks 3–5:
  ```ts
  export const colors: {
    bg: string; surface: string; surface2: string; line: string;
    text: string; mut: string; blue: string; amber: string; green: string; red: string;
  };
  export const fonts: { display: string; body: string; mono: string };
  ```

- [ ] **Step 1: Install the self-hosted font packages**

```bash
cd /mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas/frontend
cmd.exe /c "npm install @fontsource/sora @fontsource/inter @fontsource/jetbrains-mono"
cd /mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas
```

Expected: `frontend/package.json` `dependencies` gains `@fontsource/sora`, `@fontsource/inter`,
`@fontsource/jetbrains-mono`; `frontend/package-lock.json` updates.

- [ ] **Step 2: Create `frontend/src/lib/theme.ts`**

```ts
export const colors = {
  bg: "#14181F",
  surface: "#1B222B",
  surface2: "#222B36",
  line: "#2B3542",
  text: "#E9EDF2",
  mut: "#8B99AB",
  blue: "#6EA8FE",
  amber: "#E3A44C",
  green: "#6FBF8E",
  red: "#E06C6C",
} as const;

export const fonts = {
  display: "'Sora', system-ui, sans-serif",
  body: "'Inter', system-ui, sans-serif",
  mono: "'JetBrains Mono', ui-monospace, monospace",
} as const;
```

- [ ] **Step 3: Create `frontend/src/app.css`**

```css
@import "@fontsource/sora/600.css";
@import "@fontsource/sora/700.css";
@import "@fontsource/inter/400.css";
@import "@fontsource/inter/600.css";
@import "@fontsource/jetbrains-mono/400.css";

:root {
  color-scheme: dark;
}

* {
  box-sizing: border-box;
}

html,
body {
  margin: 0;
  height: 100%;
  background: #14181f;
  color: #e9edf2;
  font-family: "Inter", system-ui, sans-serif;
}

#app {
  height: 100%;
}
```

- [ ] **Step 4: Import the stylesheet in `frontend/src/main.ts`**

`frontend/src/main.ts` currently reads:

```ts
import { mount } from 'svelte'
import App from './App.svelte'

mount(App, { target: document.getElementById('app')! })
```

Change it to:

```ts
import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'

mount(App, { target: document.getElementById('app')! })
```

- [ ] **Step 5: Verify with svelte-check**

```bash
cd /mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas/frontend
cmd.exe /c "npm run check"
cd /mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas
```

Expected: `svelte-check` reports `0 errors` (the pre-existing a11y warning on the demo
`App.svelte`'s `<a>` tag may still show — that file is untouched until Task 5).

- [ ] **Step 6: Stage (do not commit — user controls commit timing)**

```bash
git add frontend/package.json frontend/package-lock.json frontend/src/lib/theme.ts frontend/src/app.css frontend/src/main.ts
git status
```

Do not run `git commit`.

---

### Task 3: Screen placeholder components

**Files:**
- Create: `frontend/src/lib/screens/Library.svelte`
- Create: `frontend/src/lib/screens/Queue.svelte`
- Create: `frontend/src/lib/screens/Progress.svelte`

**Interfaces:**
- Consumes: `colors`, `fonts` from `frontend/src/lib/theme.ts` (Task 2).
- Produces: three zero-prop Svelte components (`Library`, `Queue`, `Progress`), each a default
  export, consumed by `App.svelte` in Task 5.

- [ ] **Step 1: Create `frontend/src/lib/screens/Library.svelte`**

```svelte
<script lang="ts">
  import { colors, fonts } from "../theme";
</script>

<div class="screen">
  <p style="font-family: {fonts.body}; color: {colors.mut};">
    Nenhuma aula importada ainda.
  </p>
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
    margin: 0 auto;
    width: 100%;
  }
</style>
```

- [ ] **Step 2: Create `frontend/src/lib/screens/Queue.svelte`**

```svelte
<script lang="ts">
  import { colors, fonts } from "../theme";
</script>

<div class="screen">
  <p style="font-family: {fonts.body}; color: {colors.mut};">
    Nada na fila no momento.
  </p>
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
    margin: 0 auto;
    width: 100%;
  }
</style>
```

- [ ] **Step 3: Create `frontend/src/lib/screens/Progress.svelte`**

```svelte
<script lang="ts">
  import { colors, fonts } from "../theme";
</script>

<div class="screen">
  <p style="font-family: {fonts.body}; color: {colors.mut};">
    Progresso chega na Fase 3.
  </p>
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
    margin: 0 auto;
    width: 100%;
  }
</style>
```

- [ ] **Step 4: Verify with svelte-check**

```bash
cd /mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas/frontend
cmd.exe /c "npm run check"
cd /mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas
```

Expected: `0 errors` attributable to the new files (pre-existing demo `App.svelte` a11y warning
may still show).

- [ ] **Step 5: Stage (do not commit — user controls commit timing)**

```bash
git add frontend/src/lib/screens
git status
```

Do not run `git commit`.

---

### Task 4: Sidebar and Header components

**Files:**
- Create: `frontend/src/lib/Sidebar.svelte`
- Create: `frontend/src/lib/Header.svelte`

**Interfaces:**
- Consumes: `colors`, `fonts` from `frontend/src/lib/theme.ts` (Task 2).
- Produces:
  ```ts
  // Sidebar.svelte props
  type Screen = "library" | "progress" | "queue";
  let { active, onNavigate }: { active: Screen; onNavigate: (screen: Screen) => void } = $props();
  ```
  `Header.svelte` takes no props. Both consumed by `App.svelte` in Task 5, which also owns the
  `Screen` type (repeated here as a type import target — Task 5 defines the canonical copy in
  `App.svelte` and Sidebar's local `Screen` type must stay structurally identical: the three
  string literals `"library" | "progress" | "queue"`).

- [ ] **Step 1: Create `frontend/src/lib/Sidebar.svelte`**

```svelte
<script lang="ts">
  import { colors, fonts } from "./theme";

  type Screen = "library" | "progress" | "queue";

  let { active, onNavigate }: { active: Screen; onNavigate: (screen: Screen) => void } = $props();

  const NAV: { key: Screen; label: string; icon: string }[] = [
    { key: "library", label: "Biblioteca", icon: "▤" },
    { key: "progress", label: "Progresso", icon: "◔" },
    { key: "queue", label: "Fila", icon: "≡" },
  ];
</script>

<aside class="sidebar" style="border-right: 1px solid {colors.line}; background: {colors.surface};">
  <div class="brand">
    <div class="brand-name" style="font-family: {fonts.display}; color: {colors.text};">Replay</div>
    <div class="brand-sub" style="font-family: {fonts.body}; color: {colors.mut};">
      diário de aulas de inglês
    </div>
  </div>
  {#each NAV as item (item.key)}
    <button
      class="nav-item"
      style="
        background: {active === item.key ? colors.surface2 : 'transparent'};
        color: {active === item.key ? colors.text : colors.mut};
        font-family: {fonts.body};
        font-weight: {active === item.key ? 600 : 400};
      "
      onclick={() => onNavigate(item.key)}
    >
      <span class="icon">{item.icon}</span>
      {item.label}
    </button>
  {/each}
</aside>

<style>
  .sidebar {
    width: 13rem;
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    padding: 1rem;
  }
  .brand {
    margin-bottom: 2rem;
    padding: 0 0.5rem;
  }
  .brand-name {
    font-weight: 700;
    font-size: 1rem;
  }
  .brand-sub {
    font-size: 0.75rem;
    margin-top: 0.125rem;
  }
  .nav-item {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    width: 100%;
    padding: 0.625rem 0.75rem;
    border-radius: 0.5rem;
    font-size: 0.875rem;
    margin-bottom: 0.25rem;
    text-align: left;
    border: none;
    cursor: pointer;
  }
  .icon {
    display: inline-block;
    width: 1.1rem;
    text-align: center;
  }
</style>
```

- [ ] **Step 2: Create `frontend/src/lib/Header.svelte`**

```svelte
<script lang="ts">
  import { colors } from "./theme";
</script>

<header class="header" style="border-bottom: 1px solid {colors.line};"></header>

<style>
  .header {
    height: 3rem;
    flex-shrink: 0;
  }
</style>
```

- [ ] **Step 3: Verify with svelte-check**

```bash
cd /mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas/frontend
cmd.exe /c "npm run check"
cd /mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas
```

Expected: `0 errors` attributable to the new files.

- [ ] **Step 4: Stage (do not commit — user controls commit timing)**

```bash
git add frontend/src/lib/Sidebar.svelte frontend/src/lib/Header.svelte
git status
```

Do not run `git commit`.

---

### Task 5: Wire up App.svelte, remove demo assets, final verification

**Files:**
- Modify: `main.go` (replace the demo content from Task 1 with the real thin shell)
- Delete: `greetservice.go` (its only consumer, the demo `App.svelte`, is replaced in this task too)
- Modify: `frontend/src/App.svelte` (replace demo content with the real shell)
- Modify: `frontend/index.html` (title, drop demo favicon/background references)
- Delete: `frontend/public/wails.png`, `frontend/public/svelte.svg`,
  `frontend/public/bg-desktop.jpg`, `frontend/public/bg-mobile.jpg`,
  `frontend/public/Inter-Medium.ttf`, `frontend/public/style.css`,
  `frontend/Inter Font License.txt`
- Modify: `docs/phase-1-mvp.md` (progress table row)

**Interfaces:**
- Consumes: `Sidebar`, `Header` (Task 4); `Library`, `Progress`, `Queue` (Task 3); `colors`,
  `fonts` (Task 2).
- Produces: the running app shell — nothing downstream in this story consumes `App.svelte`
  directly (it's the composition root).

- [ ] **Step 1: Replace `main.go` with the real thin shell (no demo service, no demo event)**

Task 1 left `main.go` as the template's demo content (registers `GreetService`, emits a `time`
event). Replace it entirely:

```go
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:        "Assistente de Idiomas",
		Description: "Arquivo e análise de aulas de inglês do Cambly",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Assistente de Idiomas",
		Width:            1200,
		Height:           760,
		BackgroundColour: application.NewRGB(20, 24, 31), // #14181F — colors.bg
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 2: Delete `greetservice.go`**

```bash
cd /mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas
rm greetservice.go
```

Expected: no other Go file references `GreetService` (it was only wired into the demo
`main.go`, just replaced in Step 1) — `go.exe vet ./...` in Step 6 below confirms this.

- [ ] **Step 3: Replace `frontend/src/App.svelte`**

```svelte
<script lang="ts">
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import { colors, fonts } from "./lib/theme";

  type Screen = "library" | "progress" | "queue";

  let screen: Screen = $state("library");
</script>

<div class="shell" style="background: {colors.bg}; font-family: {fonts.body};">
  <Sidebar active={screen} onNavigate={(s) => (screen = s)} />
  <main class="main">
    <Header />
    <div class="content">
      {#if screen === "library"}
        <Library />
      {:else if screen === "progress"}
        <Progress />
      {:else}
        <Queue />
      {/if}
    </div>
  </main>
</div>

<style>
  .shell {
    display: flex;
    width: 100%;
    height: 100vh;
  }
  .main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .content {
    flex: 1;
    overflow-y: auto;
  }
</style>
```

- [ ] **Step 4: Update `frontend/index.html`**

Current content:

```html
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <link rel="icon" type="image/svg+xml" href="/wails.png" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover" />
    <link rel="stylesheet" href="/style.css" />
    <title>Wails + Svelte + TS</title>
  </head>
  <body>
    <div class="bg" aria-hidden="true"></div>
    <div id="app"></div>
    <script type="module" src="/src/main.ts"></script>
  </body>
</html>
```

Replace with:

```html
<!DOCTYPE html>
<html lang="pt-BR">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover" />
    <title>Assistente de Idiomas</title>
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="/src/main.ts"></script>
  </body>
</html>
```

(The favicon link and `/style.css` link are dropped along with the demo assets in Step 5; the
`app.css` import inside `main.ts`, added in Task 2, already covers global styling.)

- [ ] **Step 5: Delete the now-unused demo assets**

```bash
cd /mnt/c/Users/lukff/Documents/Projetos/assistente-idiomas
rm frontend/public/wails.png frontend/public/svelte.svg frontend/public/bg-desktop.jpg frontend/public/bg-mobile.jpg frontend/public/Inter-Medium.ttf frontend/public/style.css
rm "frontend/Inter Font License.txt"
```

- [ ] **Step 6: Full build verification**

```bash
cmd.exe /c "wails3 build"
```

Expected: succeeds, produces `bin/assistente-idiomas.exe`. This is the first build since Steps
1–3 removed the `GreetService` binding and its consumer together, and Steps 3–5 replaced/pruned
the frontend demo — a clean build here confirms nothing was left dangling (Vite would fail on a
missing referenced asset or unresolved import; `go build` would fail on a stray reference to the
deleted `greetservice.go`).

- [ ] **Step 7: `go vet` check**

```bash
go.exe vet ./...
```

Expected: no output (clean) — in particular, no complaint about a missing `GreetService` type,
confirming Step 2's deletion left no dangling reference.

- [ ] **Step 8: Manual visual verification (both machines)**

```bash
cmd.exe /c "wails3 dev"
```

Expected: a window opens titled "Assistente de Idiomas", dark background (`#14181F`), sidebar on
the left with "Replay" branding and three nav items (Library / Progress / Queue), Library
active by default showing "Nenhuma aula importada ainda.". Clicking Progress/Queue switches the
content pane and the active nav highlight. Header area is empty. Repeat this check on the Linux
machine (risk 3 area — confirms nothing OS-specific broke, per `docs/phase-1-mvp.md` risk list).
This step needs a real display, so it is manual — not scriptable in this environment.

- [ ] **Step 9: Update `docs/phase-1-mvp.md` progress table**

In `docs/phase-1-mvp.md`, the `## Progress log` table currently has one empty row:

```
| Data | O que foi feito | Observações |
|------|-----------------|-------------|
| | | |
```

Replace the empty row with:

```
| Data | O que foi feito | Observações |
|------|-----------------|-------------|
| 20/07/2026 | Story 1 complete: Wails v3 + Svelte 5 skeleton (sidebar, empty header, 3 placeholder screens) | wails3 v3.0.0-alpha2.117 pinned; self-hosted fonts via @fontsource |
```

Also check the checkboxes under `## Story 1` (all four acceptance criteria) from `- [ ]` to
`- [x]`.

- [ ] **Step 10: Stage (do not commit — user controls commit timing)**

```bash
git add main.go greetservice.go frontend docs/phase-1-mvp.md
git status
```

(`git add frontend` picks up the edited `App.svelte`/`index.html`, the deletions under
`frontend/public/` *and* the deleted `frontend/Inter Font License.txt` at the frontend root, in
one go — nothing new is generated under `frontend/` at this step that isn't already covered by
the `.gitignore` rules from Task 1. `git add main.go` stages the thin-shell rewrite; `git add
greetservice.go` stages its deletion — `git add` stages a file deletion the same way it stages an
edit. Confirm in `git status` output: `deleted: greetservice.go`.)

Expected: `git status` shows the seven deleted demo files (six under `frontend/`, plus
`greetservice.go`) under the `deleted:` section, staged. Do not run `git commit`.

---

## Self-Review Notes

- **Spec coverage:** all four `docs/phase-1-mvp.md` Story 1 acceptance criteria map to tasks —
  compiling/opening window → Task 1 Step 5 + Task 5 Step 6/8; thin-layer/no-Wails-in-internal →
  Global Constraints (nothing in this story touches `internal/`, so it holds trivially, verified
  by `go vet` after every Go change); sidebar/header portado → Tasks 3–5; Svelte 5 runes only →
  every component uses `$state`/`$props`, no legacy syntax.
- **Placeholder scan:** no TBDs; every step has literal file content or an exact command with
  stated expected output.
- **Type consistency:** `Screen = "library" | "progress" | "queue"` is used identically in
  `Sidebar.svelte` (Task 4) and `App.svelte` (Task 5); `colors`/`fonts` field names match between
  `theme.ts` (Task 2) and every consumer (Tasks 3–5).
- **Revision note (post-Task-1 dispatch, round 1):** Task 1's implementer correctly caught that
  the original plan had Task 1 write the thin `main.go` while leaving the demo `App.svelte` in
  place — a hard build break, since the demo `App.svelte` imports a `GreetService` binding that
  only exists when the demo service is registered. Fixed by moving the thin-`main.go` rewrite and
  the `greetservice.go` deletion into Task 5, alongside the `App.svelte` replacement that removes
  the binding's only consumer. Task 1 now ships the *stock* demo end-to-end, and Task 5 swaps
  every piece of it out together.
- **Revision note (post-Task-1 dispatch, round 2):** after the round 1 fix, the implementer hit a
  second, unrelated defect: Step 1's original `wails3 init` command passed
  `-productname "Assistente de Idiomas"` etc. through `cmd.exe /c "..."` from bash, and that
  two-layer quoting corrupted every multi-word value across the whole `build/` tree (confirmed:
  truncated at the first space plus a stray literal `"`), breaking the Windows manifest's XML and
  breaking the build. This was never actually exercised during design — the design-time build
  test used the bare `wails3 init` form without `-product*` flags. Fixed by dropping those flags
  from Step 1 entirely and adding Step 2a: edit `build/config.yml` directly (plain text, no shell
  requoting) and run `wails3 task common:update:build-assets` to regenerate the platform files
  from it — verified end-to-end (clean manifest, full `wails3 build` success) during this fix.
