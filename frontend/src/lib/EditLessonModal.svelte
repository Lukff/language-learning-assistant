<script lang="ts">
  import { untrack } from "svelte";
  import { colors, fonts } from "./theme";
  import * as LibraryService from "../../bindings/assistente-idiomas/services/libraryservice";
  import TeacherCombobox from "./TeacherCombobox.svelte";

  let {
    lessonId,
    initialLessonDate,
    initialTeacherName,
    speakerOptions,
    currentStudentSpeaker,
    hasAnalysisResults,
    onSaved,
    onSpeakerChanged,
    onClose,
  }: {
    lessonId: number;
    initialLessonDate: string;
    initialTeacherName: string;
    speakerOptions: string[];
    currentStudentSpeaker: string | null;
    hasAnalysisResults: boolean;
    onSaved: () => void;
    onSpeakerChanged: () => void;
    onClose: () => void;
  } = $props();

  let lessonDate: string = $state(untrack(() => initialLessonDate));
  let teacherName: string = $state(untrack(() => initialTeacherName));
  let error: string = $state("");
  let saving: boolean = $state(false);

  let studentSpeaker: string | null = $state(untrack(() => currentStudentSpeaker));
  let speakerError: string = $state("");
  let savingSpeaker: boolean = $state(false);

  function speakerLabel(speaker: string): string {
    const idx = speakerOptions.indexOf(speaker);
    return `Speaker ${String.fromCharCode(65 + (idx < 0 ? 0 : idx))}`;
  }

  async function chooseSpeaker(newSpeaker: string) {
    if (newSpeaker === studentSpeaker) return;
    if (currentStudentSpeaker !== null && hasAnalysisResults) {
      const confirmed = confirm(
        "Changing who you are discards the analyses already done for this lesson — you'll need to reprocess it. Continue?",
      );
      if (!confirmed) return;
    }
    speakerError = "";
    savingSpeaker = true;
    try {
      await LibraryService.SetStudentSpeaker(lessonId, newSpeaker);
      studentSpeaker = newSpeaker;
      onSpeakerChanged();
    } catch (e) {
      speakerError = String(e);
    } finally {
      savingSpeaker = false;
    }
  }

  async function save() {
    error = "";
    saving = true;
    try {
      await LibraryService.UpdateLesson(lessonId, lessonDate, teacherName);
      onSaved();
    } catch (e) {
      error = String(e);
    } finally {
      saving = false;
    }
  }
</script>

<div
  class="overlay"
  role="presentation"
  onclick={onClose}
  onkeydown={(e) => e.key === "Escape" && onClose()}
>
  <div
    class="card"
    style="background: {colors.surface}; border: 1px solid {colors.line}; color: {colors.text}; font-family: {fonts.body};"
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}
  >
    <h2 style="font-family: {fonts.display};">Edit lesson</h2>

    <label for="edit-lesson-date">Lesson date and time</label>
    <input id="edit-lesson-date" type="datetime-local" bind:value={lessonDate} />

    <label for="edit-tutor">Tutor</label>
    <TeacherCombobox id="edit-tutor" bind:value={teacherName} />

    {#if speakerOptions.length > 0}
      <span class="speaker-section-title">Who are you</span>
      {#if speakerOptions.length === 2 && studentSpeaker !== null}
        <button
          type="button"
          class="secondary"
          onclick={() => chooseSpeaker(speakerOptions.find((s) => s !== studentSpeaker) ?? speakerOptions[0])}
          disabled={savingSpeaker}
        >
          {savingSpeaker ? "Saving…" : "Swap speakers"}
        </button>
      {:else}
        <div class="speaker-buttons">
          {#each speakerOptions as speaker (speaker)}
            <button
              type="button"
              class="secondary"
              class:active={studentSpeaker === speaker}
              onclick={() => chooseSpeaker(speaker)}
              disabled={savingSpeaker}
            >
              {speakerLabel(speaker)} is you
            </button>
          {/each}
        </div>
      {/if}
      {#if studentSpeaker}
        <p class="hint">Currently: {speakerLabel(studentSpeaker)}</p>
      {/if}
      {#if speakerError}
        <p class="error" style="color: {colors.red};">{speakerError}</p>
      {/if}
    {/if}

    {#if error}
      <p class="error" style="color: {colors.red};">{error}</p>
    {/if}

    <div class="actions">
      <button class="secondary" onclick={onClose} disabled={saving}>Cancel</button>
      <button class="primary" onclick={save} disabled={saving || !lessonDate || !teacherName}>
        {saving ? "Saving…" : "Save"}
      </button>
    </div>
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.6);
    z-index: 50;
    padding: 1.5rem;
  }
  .card {
    border-radius: 1rem;
    padding: 1.5rem;
    width: 100%;
    max-width: 24rem;
  }
  h2 {
    font-size: 1.1rem;
    margin: 0 0 0.5rem;
  }
  label {
    display: block;
    font-size: 0.8rem;
    margin-bottom: 0.25rem;
  }
  .speaker-section-title {
    display: block;
    font-size: 0.8rem;
    margin-bottom: 0.5rem;
  }
  .speaker-buttons {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin-bottom: 0.75rem;
  }
  .speaker-buttons button.active {
    font-weight: 600;
  }
  .hint {
    font-size: 0.75rem;
    margin: 0 0 0.75rem;
  }
  input {
    width: 100%;
    padding: 0.5rem;
    margin-bottom: 1rem;
    box-sizing: border-box;
  }
  .error {
    font-size: 0.85rem;
    margin-bottom: 1rem;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }
  button {
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
</style>
