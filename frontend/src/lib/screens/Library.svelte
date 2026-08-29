<script lang="ts">
  import { onMount } from "svelte";
  import { Events } from "@wailsio/runtime";
  import { colors, fonts } from "../theme";
  import * as ImportService from "../../../bindings/assistente-idiomas/services/importservice";
  import * as LibraryService from "../../../bindings/assistente-idiomas/services/libraryservice";
  import * as TeacherService from "../../../bindings/assistente-idiomas/services/teacherservice";
  import * as TopicsService from "../../../bindings/assistente-idiomas/services/topicsservice";
  import type { PendingImport, Lesson, LessonFilter, Teacher, Topic } from "../../../bindings/assistente-idiomas/services/models";
  import ImportConfirmModal from "../ImportConfirmModal.svelte";

  let {
    onOpenLesson,
    onOpenTeachers,
    onOpenTopics,
  }: { onOpenLesson: (lessonId: number) => void; onOpenTeachers: () => void; onOpenTopics: () => void } = $props();

  let pending: PendingImport[] = $state([]);
  let lessons: Lesson[] = $state([]);
  let teachers: Teacher[] = $state([]);
  let topics: Topic[] = $state([]);
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
  let filterTopicIds: number[] = $state([]);
  let topicFilterInput: string = $state("");

  const STATUS_LABEL: Record<string, string> = {
    ready: "ready",
    processing: "processing…",
    error: "error",
  };

  async function loadPending() {
    pending = (await ImportService.ListPendingImports()) ?? [];
  }

  async function loadLessons() {
    const filter: LessonFilter = {
      teacherId: filterTeacherId,
      dateFrom: filterDateFrom,
      dateTo: filterDateTo,
      topicIds: filterTopicIds,
    };
    lessons = (await LibraryService.ListLessons(filter)) ?? [];
  }

  async function loadTeachers() {
    teachers = (await TeacherService.ListTeachers()) ?? [];
  }

  async function loadTopics() {
    topics = (await TopicsService.ListTopics()) ?? [];
  }

  function addTopicFilter(name: string) {
    const match = topics.find((t) => t.name.toLowerCase() === name.trim().toLowerCase());
    if (!match || filterTopicIds.includes(match.id)) return;
    filterTopicIds = [...filterTopicIds, match.id];
    topicFilterInput = "";
    applyFilter();
  }

  function removeTopicFilter(topicId: number) {
    filterTopicIds = filterTopicIds.filter((id) => id !== topicId);
    applyFilter();
  }

  const MONTH_ABBREVIATIONS = [
    "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
  ];

  // lessonDate is stored as "YYYY-MM-DD" or "YYYY-MM-DDTHH:MM" (the format of
  // <input type="datetime-local">); this just reformats it for display
  // without depending on timezone (it's not a "Z" timestamp, it's local time).
  function formatLessonDateTime(value: string): string {
    const [datePart, timePart] = value.split("T");
    const [year, month, day] = datePart.split("-");
    const formattedDate = `${MONTH_ABBREVIATIONS[Number(month) - 1]} ${Number(day)}, ${year}`;
    return timePart ? `${formattedDate}, ${timePart}` : formattedDate;
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
      await Promise.all([loadPending(), loadLessons(), loadTeachers(), loadTopics()]);
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
      syncMessage = `${summary.new} new, ${summary.updated} updated, ${summary.errors} errors`;
      // The scan can reconcile the video_path of already-confirmed lessons
      // (file renamed/moved, found by hash) — reload lessons
      // too, not just pending, otherwise the missing-video badge (Story 8)
      // stays stuck showing the video_path from before the scan.
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
    <h1 style="font-family: {fonts.display};">Library</h1>
    <div class="header-actions">
      <button onclick={onOpenTeachers}>Teachers</button>
      <button onclick={onOpenTopics}>Topics</button>
      <button onclick={syncFolder} disabled={syncing}>
        {syncing ? "Syncing…" : "Sync folder"}
      </button>
    </div>
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
    <p style="color: {colors.mut};">Loading…</p>
  {:else}
    {#if pending.length > 0}
      <section class="pending" style="background: {colors.surface}; border: 1px solid {colors.line};">
        <h2 style="font-family: {fonts.display};">{pending.length} lessons awaiting review</h2>
        <ul>
          {#each pending as item (item.id)}
            <li>
              <span class="path" style="font-family: {fonts.mono}; color: {colors.mut};">{item.path}</span>
              <button onclick={() => (reviewing = item)}>Review</button>
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    <section class="filters" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <label>
        Tutor
        <select bind:value={filterTeacherId} onchange={applyFilter}>
          <option value={0}>All</option>
          {#each teachers as teacher (teacher.id)}
            <option value={teacher.id}>{teacher.name}</option>
          {/each}
        </select>
      </label>
      <label>
        From
        <input type="date" bind:value={filterDateFrom} onchange={applyFilter} />
      </label>
      <label>
        To
        <input type="date" bind:value={filterDateTo} onchange={applyFilter} />
      </label>
      {#if topics.length > 0}
        <div class="topic-filter">
          <label>
            Topics
            <input
              list="topic-filter-datalist"
              type="text"
              placeholder="Add topic…"
              autocomplete="off"
              bind:value={topicFilterInput}
              onchange={() => addTopicFilter(topicFilterInput)}
            />
            <datalist id="topic-filter-datalist">
              {#each topics.filter((t) => !filterTopicIds.includes(t.id)) as topic (topic.id)}
                <option value={topic.name}></option>
              {/each}
            </datalist>
          </label>
          {#if filterTopicIds.length > 0}
            <div class="topic-filter-chips">
              {#each filterTopicIds as topicId (topicId)}
                {@const topic = topics.find((t) => t.id === topicId)}
                {#if topic}
                  <span class="topic-chip">
                    {topic.name}
                    <button
                      type="button"
                      onclick={() => removeTopicFilter(topicId)}
                      aria-label={`Remove filter ${topic.name}`}>×</button
                    >
                  </span>
                {/if}
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    </section>

    {#if lessons.length > 0}
      <section class="lessons" style="background: {colors.surface}; border: 1px solid {colors.line};">
        <h2 style="font-family: {fonts.display};">{lessons.length} lessons</h2>
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
              {#if lesson.status === "error"}
                <div class="status-block">
                  <span class="badge" style="color: {colors.red}; background: rgba(224,108,108,.1);">error</span>
                  <span class="error-message" style="color: {colors.mut};">{lesson.errorMessage}</span>
                  <button onclick={() => retry(lesson.id)} disabled={retryingId === lesson.id}>
                    {retryingId === lesson.id ? "Reprocessing…" : "Reprocess"}
                  </button>
                </div>
              {:else}
                <span
                  class="badge"
                  style="color: {lesson.status === 'ready'
                    ? colors.green
                    : colors.blue}; background: {lesson.status === 'ready'
                    ? 'rgba(111,191,142,.1)'
                    : 'rgba(110,168,254,.1)'};"
                >
                  {STATUS_LABEL[lesson.status] ?? lesson.status}
                </span>
              {/if}
              {#if lesson.videoMissing}
                <span class="badge" style="color: {colors.amber}; background: rgba(227,164,76,.1);">
                  video missing
                </span>
              {/if}
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    {#if pending.length === 0 && lessons.length === 0}
      <p style="color: {colors.mut};">No lessons imported yet.</p>
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
    gap: 1rem;
    flex-wrap: wrap;
    margin-bottom: 1rem;
  }
  .header-actions {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
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
  .topic-filter {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    min-width: 12rem;
  }
  .topic-filter-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.4rem;
    align-items: center;
  }
  .topic-chip {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.2rem 0.5rem;
    border-radius: 999px;
    font-size: 0.75rem;
    background: rgba(110, 168, 254, 0.12);
  }
  .topic-chip button {
    padding: 0;
    border: none;
    background: none;
    cursor: pointer;
    line-height: 1;
    font-size: 0.9rem;
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
