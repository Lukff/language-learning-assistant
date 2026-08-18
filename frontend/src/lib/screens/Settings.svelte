<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as SettingsService from "../../../bindings/assistente-idiomas/services/settingsservice";
  import * as TeacherService from "../../../bindings/assistente-idiomas/services/teacherservice";
  import * as TopicsService from "../../../bindings/assistente-idiomas/services/topicsservice";
  import type { Teacher, Topic } from "../../../bindings/assistente-idiomas/services/models";

  let storageRoot: string = $state("");
  let loading: boolean = $state(true);
  let loadError: string = $state("");

  let changingFolder: boolean = $state(false);
  let folderError: string = $state("");
  let scanSummary: string = $state("");

  let hasCredential: boolean = $state(false);
  let credentialError: string = $state("");
  let apiKeyInput: string = $state("");
  let savingCredential: boolean = $state(false);
  let saveCredentialError: string = $state("");
  let saveCredentialSuccess: boolean = $state(false);

  let hasAnalysisCredential: boolean = $state(false);
  let analysisCredentialError: string = $state("");
  let analysisApiKeyInput: string = $state("");
  let savingAnalysisCredential: boolean = $state(false);
  let saveAnalysisCredentialError: string = $state("");
  let saveAnalysisCredentialSuccess: boolean = $state(false);

  let teachers: Teacher[] = $state([]);
  let teachersError: string = $state("");
  let renameDrafts: Record<number, string> = $state({});
  let renamingId: number | null = $state(null);
  let renameErrors: Record<number, string> = $state({});

  let topics: Topic[] = $state([]);
  let topicsError: string = $state("");
  let topicRenameDrafts: Record<number, string> = $state({});
  let renamingTopicId: number | null = $state(null);
  let topicRenameErrors: Record<number, string> = $state({});

  async function loadStorageRoot() {
    storageRoot = await SettingsService.GetStorageRoot();
  }

  async function loadCredentialStatus() {
    try {
      hasCredential = await SettingsService.HasSTTCredential();
      credentialError = "";
    } catch (e) {
      credentialError = String(e);
    }
  }

  async function loadAnalysisCredentialStatus() {
    try {
      hasAnalysisCredential = await SettingsService.HasAnalysisCredential();
      analysisCredentialError = "";
    } catch (e) {
      analysisCredentialError = String(e);
    }
  }

  async function changeFolder() {
    folderError = "";
    scanSummary = "";
    let chosen: string;
    try {
      chosen = await SettingsService.ChooseStorageFolder();
    } catch (e) {
      folderError = String(e);
      return;
    }
    if (!chosen) return;

    changingFolder = true;
    try {
      const summary = await SettingsService.ChangeStorageFolder(chosen);
      scanSummary = `${summary.new} novas, ${summary.updated} atualizadas, ${summary.skipped} puladas, ${summary.errors} erros`;
      await loadStorageRoot();
    } catch (e) {
      folderError = String(e);
    } finally {
      changingFolder = false;
    }
  }

  async function saveCredential() {
    saveCredentialError = "";
    saveCredentialSuccess = false;
    savingCredential = true;
    try {
      await SettingsService.SaveSTTAPIKey(apiKeyInput);
      apiKeyInput = "";
      saveCredentialSuccess = true;
      await loadCredentialStatus();
    } catch (e) {
      saveCredentialError = String(e);
    } finally {
      savingCredential = false;
    }
  }

  async function saveAnalysisCredential() {
    saveAnalysisCredentialError = "";
    saveAnalysisCredentialSuccess = false;
    savingAnalysisCredential = true;
    try {
      await SettingsService.SaveAnalysisAPIKey(analysisApiKeyInput);
      analysisApiKeyInput = "";
      saveAnalysisCredentialSuccess = true;
      await loadAnalysisCredentialStatus();
    } catch (e) {
      saveAnalysisCredentialError = String(e);
    } finally {
      savingAnalysisCredential = false;
    }
  }

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

  async function loadTopics(justRenamedId?: number) {
    const previousTopics = topics;
    const previousDrafts = topicRenameDrafts;
    const fresh = (await TopicsService.ListTopics()) ?? [];
    const nextDrafts: Record<number, string> = {};
    for (const tp of fresh) {
      const existingDraft = previousDrafts[tp.id];
      const oldTopic = previousTopics.find((p) => p.id === tp.id);
      const isDirty = existingDraft !== undefined && oldTopic !== undefined && existingDraft !== oldTopic.name;
      nextDrafts[tp.id] = isDirty && tp.id !== justRenamedId ? existingDraft : tp.name;
    }
    topics = fresh;
    topicRenameDrafts = nextDrafts;
  }

  async function renameTopic(id: number) {
    topicRenameErrors = { ...topicRenameErrors, [id]: "" };
    renamingTopicId = id;
    try {
      await TopicsService.RenameTopic(id, topicRenameDrafts[id]);
      await loadTopics(id);
    } catch (e) {
      topicRenameErrors = { ...topicRenameErrors, [id]: String(e) };
    } finally {
      renamingTopicId = null;
    }
  }

  onMount(async () => {
    try {
      await Promise.all([loadStorageRoot(), loadCredentialStatus(), loadAnalysisCredentialStatus(), loadTeachers(), loadTopics()]);
    } catch (e) {
      loadError = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <h1 style="font-family: {fonts.display};">Configurações</h1>

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else}
    {#if loadError}
      <p class="error" style="color: {colors.red};">{loadError}</p>
    {/if}

    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Armazenamento</h2>
      <p class="path" style="font-family: {fonts.mono}; color: {colors.mut};">{storageRoot}</p>
      <p class="hint" style="color: {colors.mut};">
        Se você já moveu a pasta de aulas manualmente, aponte o app pra ela aqui — os vídeos não
        são copiados nem movidos pelo app.
      </p>
      <button onclick={changeFolder} disabled={changingFolder}>
        {changingFolder ? "Trocando…" : "Trocar pasta"}
      </button>
      {#if scanSummary}
        <p class="hint" style="color: {colors.mut};">{scanSummary}</p>
      {/if}
      {#if folderError}
        <p class="error" style="color: {colors.red};">{folderError}</p>
      {/if}
    </section>

    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Credencial do provedor de transcrição</h2>
      {#if credentialError}
        <p class="error" style="color: {colors.red};">{credentialError}</p>
      {:else}
        <p class="status" style="color: {hasCredential ? colors.green : colors.mut};">
          {hasCredential ? "Credencial configurada" : "Nenhuma credencial configurada"}
        </p>
      {/if}
      <div class="credential-form">
        <input
          type="password"
          bind:value={apiKeyInput}
          placeholder="Nova API key da ElevenLabs"
          autocomplete="off"
          style="border: 1px solid {colors.line}; background: transparent; color: {colors.text};"
        />
        <button onclick={saveCredential} disabled={savingCredential || !apiKeyInput}>
          {savingCredential ? "Salvando…" : "Salvar"}
        </button>
      </div>
      {#if saveCredentialSuccess}
        <p class="hint" style="color: {colors.green};">Credencial salva.</p>
      {/if}
      {#if saveCredentialError}
        <p class="error" style="color: {colors.red};">{saveCredentialError}</p>
      {/if}
    </section>

    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Credencial do provedor de análise</h2>
      {#if analysisCredentialError}
        <p class="error" style="color: {colors.red};">{analysisCredentialError}</p>
      {:else}
        <p class="status" style="color: {hasAnalysisCredential ? colors.green : colors.mut};">
          {hasAnalysisCredential ? "Credencial configurada" : "Nenhuma credencial configurada"}
        </p>
      {/if}
      <div class="credential-form">
        <input
          type="password"
          bind:value={analysisApiKeyInput}
          placeholder="Nova API key da DeepSeek"
          autocomplete="off"
          style="border: 1px solid {colors.line}; background: transparent; color: {colors.text};"
        />
        <button onclick={saveAnalysisCredential} disabled={savingAnalysisCredential || !analysisApiKeyInput}>
          {savingAnalysisCredential ? "Salvando…" : "Salvar"}
        </button>
      </div>
      {#if saveAnalysisCredentialSuccess}
        <p class="hint" style="color: {colors.green};">Credencial salva.</p>
      {/if}
      {#if saveAnalysisCredentialError}
        <p class="error" style="color: {colors.red};">{saveAnalysisCredentialError}</p>
      {/if}
    </section>

    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Professores</h2>
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
    </section>

    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Tópicos</h2>
      {#if topicsError}
        <p class="error" style="color: {colors.red};">{topicsError}</p>
      {/if}
      {#if topics.length === 0}
        <p class="hint" style="color: {colors.mut};">Nenhum tópico cadastrado ainda.</p>
      {:else}
        <ul class="teacher-list">
          {#each topics as topic (topic.id)}
            <li>
              <input
                type="text"
                bind:value={topicRenameDrafts[topic.id]}
                style="border: 1px solid {colors.line}; background: transparent; color: {colors.text};"
              />
              <button
                onclick={() => renameTopic(topic.id)}
                disabled={renamingTopicId === topic.id || !topicRenameDrafts[topic.id] || topicRenameDrafts[topic.id] === topic.name}
              >
                {renamingTopicId === topic.id ? "Renomeando…" : "Renomear"}
              </button>
              {#if topicRenameErrors[topic.id]}
                <p class="error" style="color: {colors.red};">{topicRenameErrors[topic.id]}</p>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {/if}
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 48rem;
    margin: 0 auto;
    width: 100%;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0 0 1.25rem;
  }
  .card {
    border-radius: 0.75rem;
    padding: 1.25rem;
    margin-bottom: 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .card h2 {
    font-size: 1rem;
    margin: 0 0 0.25rem;
  }
  .path {
    font-size: 0.85rem;
    word-break: break-all;
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
  .status {
    font-size: 0.85rem;
    margin: 0;
  }
  button {
    align-self: flex-start;
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
  .credential-form {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    flex-wrap: wrap;
  }
  .credential-form input {
    flex: 1;
    min-width: 12rem;
    padding: 0.5rem 0.75rem;
    border-radius: 0.5rem;
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
