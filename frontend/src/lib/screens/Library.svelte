<script lang="ts">
  import { onMount } from "svelte";
  import { Events } from "@wailsio/runtime";
  import { colors, fonts } from "../theme";
  import * as ImportService from "../../../bindings/assistente-idiomas/services/importservice";
  import * as LibraryService from "../../../bindings/assistente-idiomas/services/libraryservice";
  import * as TeacherService from "../../../bindings/assistente-idiomas/services/teacherservice";
  import type { PendingImport, Lesson, LessonFilter, Teacher } from "../../../bindings/assistente-idiomas/services/models";
  import ImportConfirmModal from "../ImportConfirmModal.svelte";

  let { onOpenLesson }: { onOpenLesson: (lessonId: number) => void } = $props();

  let pending: PendingImport[] = $state([]);
  let lessons: Lesson[] = $state([]);
  let teachers: Teacher[] = $state([]);
  let loading: boolean = $state(true);
  let syncing: boolean = $state(false);
  let syncMessage: string = $state("");
  let error: string = $state("");
  let reviewing: PendingImport | null = $state(null);
  let retryingId: number | null = $state(null);
  let pendingQueue: PendingImport[] = $state([]);
  let dropErrors: string[] = $state([]);

  interface DropErrorPayload {
    path: string;
    error: string;
  }

  let filterTeacherId: number = $state(0);
  let filterDateFrom: string = $state("");
  let filterDateTo: string = $state("");

  const STATUS_LABEL: Record<string, string> = {
    pronta: "pronta",
    processando: "processando…",
    erro: "erro",
  };

  async function loadPending() {
    pending = (await ImportService.ListPendingImports()) ?? [];
  }

  async function loadLessons() {
    const filter: LessonFilter = { teacherId: filterTeacherId, dateFrom: filterDateFrom, dateTo: filterDateTo };
    lessons = (await LibraryService.ListLessons(filter)) ?? [];
  }

  async function loadTeachers() {
    teachers = (await TeacherService.ListTeachers()) ?? [];
  }

  // lessonDate é gravado como "AAAA-MM-DD" ou "AAAA-MM-DDTHH:MM" (formato de
  // <input type="datetime-local">); aqui só reformata pra exibição em pt-BR
  // sem depender de fuso horário (não é um timestamp com "Z", é hora local).
  function formatLessonDateTime(value: string): string {
    const [datePart, timePart] = value.split("T");
    const [year, month, day] = datePart.split("-");
    const formattedDate = `${day}/${month}/${year}`;
    return timePart ? `${formattedDate} ${timePart}` : formattedDate;
  }

  function formatDuration(seconds: number | null): string {
    if (seconds == null) return "";
    const totalMinutes = Math.round(seconds / 60);
    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;
    return hours > 0 ? `${hours}h ${minutes}min` : `${minutes}min`;
  }

  async function loadAll() {
    try {
      await Promise.all([loadPending(), loadLessons(), loadTeachers()]);
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  }

  async function applyFilter() {
    error = "";
    try {
      await loadLessons();
    } catch (e) {
      error = String(e);
    }
  }

  async function syncFolder() {
    error = "";
    syncMessage = "";
    syncing = true;
    try {
      const summary = await ImportService.ScanFolder();
      syncMessage = `${summary.new} novas, ${summary.updated} atualizadas, ${summary.errors} erros`;
      // A varredura pode reconciliar o video_path de aulas já confirmadas
      // (arquivo renomeado/movido, achado por hash) — recarrega lessons
      // também, não só pending, senão o badge de vídeo ausente (História 8)
      // fica preso mostrando o video_path de antes da varredura.
      await Promise.all([loadPending(), loadLessons()]);
    } catch (e) {
      error = String(e);
    } finally {
      syncing = false;
    }
  }

  function openNextFromQueue() {
    if (reviewing || pendingQueue.length === 0) return;
    const [next, ...rest] = pendingQueue;
    pendingQueue = rest;
    reviewing = next;
  }

  function closeReview() {
    reviewing = null;
    openNextFromQueue();
  }

  async function onConfirmed() {
    reviewing = null;
    try {
      await Promise.all([loadPending(), loadLessons(), loadTeachers()]);
    } catch (e) {
      error = String(e);
    }
    openNextFromQueue();
  }

  async function retry(lessonId: number) {
    error = "";
    retryingId = lessonId;
    try {
      await LibraryService.RetryLesson(lessonId);
      await loadLessons();
    } catch (e) {
      error = String(e);
    } finally {
      retryingId = null;
    }
  }

  function openLesson(lesson: Lesson) {
    onOpenLesson(lesson.id);
  }

  onMount(() => {
    loadAll();
    const offDropped = Events.On("import:dropped", (ev) => {
      const item = ev.data as PendingImport;
      pendingQueue = [...pendingQueue, item];
      openNextFromQueue();
      loadPending().catch((e) => (error = String(e)));
    });
    const offDropError = Events.On("import:drop-error", (ev) => {
      const { path, error: dropError } = ev.data as DropErrorPayload;
      dropErrors = [...dropErrors, `${path}: ${dropError}`];
    });
    return () => {
      offDropped();
      offDropError();
    };
  });
</script>

<div
  class="screen"
  data-file-drop-target
  style="font-family: {fonts.body}; color: {colors.text}; --drop-highlight: {colors.blue};"
>
  <div class="header-row">
    <h1 style="font-family: {fonts.display};">Biblioteca</h1>
    <button onclick={syncFolder} disabled={syncing}>
      {syncing ? "Sincronizando…" : "Sincronizar pasta"}
    </button>
  </div>

  {#if syncMessage}
    <p class="hint" style="color: {colors.mut};">{syncMessage}</p>
  {/if}
  {#if error}
    <p class="error" style="color: {colors.red};">{error}</p>
  {/if}
  {#if dropErrors.length > 0}
    <ul class="drop-errors">
      {#each dropErrors as msg, i (i)}
        <li style="color: {colors.red};">{msg}</li>
      {/each}
    </ul>
  {/if}

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else}
    {#if pending.length > 0}
      <section class="pending" style="background: {colors.surface}; border: 1px solid {colors.line};">
        <h2 style="font-family: {fonts.display};">{pending.length} aulas aguardando revisão</h2>
        <ul>
          {#each pending as item (item.id)}
            <li>
              <span class="path" style="font-family: {fonts.mono}; color: {colors.mut};">{item.path}</span>
              <button onclick={() => (reviewing = item)}>Revisar</button>
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    <section class="filters" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <label>
        Tutor
        <select bind:value={filterTeacherId} onchange={applyFilter}>
          <option value={0}>Todos</option>
          {#each teachers as teacher (teacher.id)}
            <option value={teacher.id}>{teacher.name}</option>
          {/each}
        </select>
      </label>
      <label>
        De
        <input type="date" bind:value={filterDateFrom} onchange={applyFilter} />
      </label>
      <label>
        Até
        <input type="date" bind:value={filterDateTo} onchange={applyFilter} />
      </label>
    </section>

    {#if lessons.length > 0}
      <section class="lessons" style="background: {colors.surface}; border: 1px solid {colors.line};">
        <h2 style="font-family: {fonts.display};">{lessons.length} aulas</h2>
        <ul>
          {#each lessons as lesson (lesson.id)}
            <li>
              <button class="lesson-main" onclick={() => openLesson(lesson)}>
                <span class="date" style="color: {colors.text};">{formatLessonDateTime(lesson.lessonDate)}</span>
                <span class="tutor" style="color: {colors.mut};"
                  >{lesson.tutor}{formatDuration(lesson.durationSeconds)
                    ? ` · ${formatDuration(lesson.durationSeconds)}`
                    : ""}</span
                >
              </button>
              {#if lesson.status === "erro"}
                <div class="status-block">
                  <span class="badge" style="color: {colors.red}; background: rgba(224,108,108,.1);">erro</span>
                  <span class="error-message" style="color: {colors.mut};">{lesson.errorMessage}</span>
                  <button onclick={() => retry(lesson.id)} disabled={retryingId === lesson.id}>
                    {retryingId === lesson.id ? "Reprocessando…" : "Reprocessar"}
                  </button>
                </div>
              {:else}
                <span
                  class="badge"
                  style="color: {lesson.status === 'pronta'
                    ? colors.green
                    : colors.blue}; background: {lesson.status === 'pronta'
                    ? 'rgba(111,191,142,.1)'
                    : 'rgba(110,168,254,.1)'};"
                >
                  {STATUS_LABEL[lesson.status] ?? lesson.status}
                </span>
              {/if}
              {#if lesson.videoMissing}
                <span class="badge" style="color: {colors.amber}; background: rgba(227,164,76,.1);">
                  vídeo ausente
                </span>
              {/if}
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    {#if pending.length === 0 && lessons.length === 0}
      <p style="color: {colors.mut};">Nenhuma aula importada ainda.</p>
    {/if}
  {/if}
</div>

{#if reviewing}
  <ImportConfirmModal pending={reviewing} {onConfirmed} onClose={closeReview} />
{/if}

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
    margin: 0 auto;
    width: 100%;
  }
  .header-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1rem;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0;
  }
  button {
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
  .hint {
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .error {
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .pending,
  .lessons,
  .filters {
    border-radius: 0.75rem;
    padding: 1rem 1.25rem;
    margin-bottom: 1rem;
  }
  .filters {
    display: flex;
    gap: 1.5rem;
    flex-wrap: wrap;
  }
  .filters label {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    font-size: 0.8rem;
  }
  .pending h2,
  .lessons h2 {
    font-size: 1rem;
    margin: 0 0 0.75rem;
  }
  .pending ul,
  .lessons ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .pending li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }
  .lessons li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }
  .lesson-main {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.25rem;
    background: none;
    border: none;
    padding: 0;
    text-align: left;
    flex: 1;
    min-width: 0;
  }
  .lessons .date {
    font-size: 0.85rem;
  }
  .lessons .tutor {
    font-size: 0.8rem;
  }
  .path {
    font-size: 0.8rem;
    word-break: break-all;
  }
  .status-block {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-shrink: 0;
  }
  .error-message {
    font-size: 0.75rem;
    max-width: 16rem;
  }
  .badge {
    font-size: 0.75rem;
    padding: 0.25rem 0.6rem;
    border-radius: 999px;
    flex-shrink: 0;
  }
  .screen:global(.file-drop-target-active) {
    outline: 2px dashed var(--drop-highlight);
    outline-offset: -8px;
    border-radius: 0.75rem;
  }
  .drop-errors {
    list-style: none;
    margin: 0 0 1rem;
    padding: 0;
    font-size: 0.85rem;
  }
</style>
