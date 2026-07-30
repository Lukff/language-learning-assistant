<script lang="ts">
  import { untrack } from "svelte";
  import { colors, fonts } from "./theme";
  import * as LibraryService from "../../bindings/assistente-idiomas/services/libraryservice";
  import TeacherCombobox from "./TeacherCombobox.svelte";

  let {
    lessonId,
    initialLessonDate,
    initialTeacherName,
    onSaved,
    onClose,
  }: {
    lessonId: number;
    initialLessonDate: string;
    initialTeacherName: string;
    onSaved: () => void;
    onClose: () => void;
  } = $props();

  let lessonDate: string = $state(untrack(() => initialLessonDate));
  let teacherName: string = $state(untrack(() => initialTeacherName));
  let error: string = $state("");
  let saving: boolean = $state(false);

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
    <h2 style="font-family: {fonts.display};">Editar aula</h2>

    <label for="edit-lesson-date">Data e horário da aula</label>
    <input id="edit-lesson-date" type="datetime-local" bind:value={lessonDate} />

    <label for="edit-tutor">Tutor</label>
    <TeacherCombobox id="edit-tutor" bind:value={teacherName} />

    {#if error}
      <p class="error" style="color: {colors.red};">{error}</p>
    {/if}

    <div class="actions">
      <button class="secondary" onclick={onClose} disabled={saving}>Cancelar</button>
      <button class="primary" onclick={save} disabled={saving || !lessonDate || !teacherName}>
        {saving ? "Salvando…" : "Salvar"}
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
