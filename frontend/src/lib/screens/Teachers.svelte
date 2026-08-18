<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as TeacherService from "../../../bindings/assistente-idiomas/services/teacherservice";
  import type { Teacher } from "../../../bindings/assistente-idiomas/services/models";

  let { onBack }: { onBack: () => void } = $props();

  let teachers: Teacher[] = $state([]);
  let loading: boolean = $state(true);
  let teachersError: string = $state("");
  let renameDrafts: Record<number, string> = $state({});
  let renamingId: number | null = $state(null);
  let renameErrors: Record<number, string> = $state({});

  async function loadTeachers(justRenamedId?: number) {
    const previousTeachers = teachers;
    const previousDrafts = renameDrafts;
    const fresh = (await TeacherService.ListTeachers()) ?? [];

    // Merge instead of blindly rebuilding: a row the user is actively editing
    // (draft differs from the name it had before this reload) must survive a
    // reload triggered by renaming a *different* teacher. The just-renamed
    // row is always reset to its fresh (now-saved) name. Teachers no longer
    // present in `fresh` are dropped; teachers new to `fresh` get a draft
    // seeded from their current name.
    const nextDrafts: Record<number, string> = {};
    for (const t of fresh) {
      const existingDraft = previousDrafts[t.id];
      const oldTeacher = previousTeachers.find((p) => p.id === t.id);
      const isDirty =
        existingDraft !== undefined && oldTeacher !== undefined && existingDraft !== oldTeacher.name;
      nextDrafts[t.id] = isDirty && t.id !== justRenamedId ? existingDraft : t.name;
    }

    teachers = fresh;
    renameDrafts = nextDrafts;
  }

  async function renameTeacher(id: number) {
    renameErrors = { ...renameErrors, [id]: "" };
    renamingId = id;
    try {
      await TeacherService.RenameTeacher(id, renameDrafts[id]);
      await loadTeachers(id);
    } catch (e) {
      renameErrors = { ...renameErrors, [id]: String(e) };
    } finally {
      renamingId = null;
    }
  }

  onMount(async () => {
    try {
      await loadTeachers();
    } catch (e) {
      teachersError = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <button class="back" onclick={onBack} style="color: {colors.mut};">← Biblioteca</button>

  <h1 style="font-family: {fonts.display};">Professores</h1>

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else}
    {#if teachersError}
      <p class="error" style="color: {colors.red};">{teachersError}</p>
    {/if}
    {#if teachers.length === 0}
      <p class="hint" style="color: {colors.mut};">Nenhum professor cadastrado ainda.</p>
    {:else}
      <ul class="teacher-list">
        {#each teachers as teacher (teacher.id)}
          <li>
            <input
              type="text"
              bind:value={renameDrafts[teacher.id]}
              style="border: 1px solid {colors.line}; background: transparent; color: {colors.text};"
            />
            <button
              onclick={() => renameTeacher(teacher.id)}
              disabled={renamingId === teacher.id || !renameDrafts[teacher.id] || renameDrafts[teacher.id] === teacher.name}
            >
              {renamingId === teacher.id ? "Renomeando…" : "Renomear"}
            </button>
            {#if renameErrors[teacher.id]}
              <p class="error" style="color: {colors.red};">{renameErrors[teacher.id]}</p>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 48rem;
    margin: 0 auto;
    width: 100%;
  }
  .back {
    background: none;
    border: none;
    cursor: pointer;
    font-size: 0.85rem;
    margin-bottom: 1rem;
    padding: 0;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0 0 1.25rem;
  }
  .hint {
    font-size: 0.8rem;
    line-height: 1.4;
    margin: 0;
  }
  .error {
    font-size: 0.85rem;
    margin: 0;
  }
  button {
    align-self: flex-start;
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
  .teacher-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .teacher-list li {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .teacher-list input {
    flex: 1;
    min-width: 10rem;
    padding: 0.4rem 0.6rem;
    border-radius: 0.5rem;
  }
</style>
