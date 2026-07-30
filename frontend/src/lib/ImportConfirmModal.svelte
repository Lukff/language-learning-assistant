<script lang="ts">
  import { untrack } from "svelte";
  import { colors, fonts } from "./theme";
  import * as ImportService from "../../bindings/assistente-idiomas/services/importservice";
  import type { PendingImport } from "../../bindings/assistente-idiomas/services/models";
  import TeacherCombobox from "./TeacherCombobox.svelte";

  let {
    pending,
    onConfirmed,
    onClose,
  }: {
    pending: PendingImport;
    onConfirmed: () => void;
    onClose: () => void;
  } = $props();

  // Cópia editável do palpite de data/horário — deliberadamente não reativa a
  // mudanças de `pending` (cada candidato tem sua própria instância deste
  // componente, ver Library.svelte). `untrack` documenta essa intenção pro
  // linter do Svelte 5.
  let lessonDate: string = $state(untrack(() => pending.suggestedDate));
  let tutor: string = $state("");
  let error: string = $state("");
  let saving: boolean = $state(false);

  async function confirm() {
    error = "";
    saving = true;
    try {
      await ImportService.ConfirmImport(pending.id, lessonDate, tutor);
      onConfirmed();
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
    <h2 style="font-family: {fonts.display};">Confirmar aula encontrada</h2>
    <p class="path" style="color: {colors.mut}; font-family: {fonts.mono};">{pending.path}</p>

    <label for="lesson-date">Data e horário da aula</label>
    <input id="lesson-date" type="datetime-local" bind:value={lessonDate} />

    <label for="tutor">Tutor</label>
    <TeacherCombobox id="tutor" bind:value={tutor} />

    {#if error}
      <p class="error" style="color: {colors.red};">{error}</p>
    {/if}

    <div class="actions">
      <button class="secondary" onclick={onClose} disabled={saving}>Cancelar</button>
      <button class="primary" onclick={confirm} disabled={saving || !lessonDate || !tutor}>
        {saving ? "Salvando…" : "Confirmar"}
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
  .path {
    font-size: 0.75rem;
    word-break: break-all;
    margin: 0 0 1.25rem;
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
