<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as SettingsService from "../../../bindings/assistente-idiomas/services/settingsservice";

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
      scanSummary = `${summary.new} new, ${summary.updated} updated, ${summary.skipped} skipped, ${summary.errors} errors`;
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

  onMount(async () => {
    try {
      await Promise.all([loadStorageRoot(), loadCredentialStatus(), loadAnalysisCredentialStatus()]);
    } catch (e) {
      loadError = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <h1 style="font-family: {fonts.display};">Settings</h1>

  {#if loading}
    <p style="color: {colors.mut};">Loading…</p>
  {:else}
    {#if loadError}
      <p class="error" style="color: {colors.red};">{loadError}</p>
    {/if}

    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Storage</h2>
      <p class="path" style="font-family: {fonts.mono}; color: {colors.mut};">{storageRoot}</p>
      <p class="hint" style="color: {colors.mut};">
        If you've already moved the lessons folder manually, point the app to it here — videos
        are not copied or moved by the app.
      </p>
      <button onclick={changeFolder} disabled={changingFolder}>
        {changingFolder ? "Changing…" : "Change folder"}
      </button>
      {#if scanSummary}
        <p class="hint" style="color: {colors.mut};">{scanSummary}</p>
      {/if}
      {#if folderError}
        <p class="error" style="color: {colors.red};">{folderError}</p>
      {/if}
    </section>

    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Transcription provider credential</h2>
      {#if credentialError}
        <p class="error" style="color: {colors.red};">{credentialError}</p>
      {:else}
        <p class="status" style="color: {hasCredential ? colors.green : colors.mut};">
          {hasCredential ? "Credential configured" : "No credential configured"}
        </p>
      {/if}
      <div class="credential-form">
        <input
          type="password"
          bind:value={apiKeyInput}
          placeholder="New ElevenLabs API key"
          autocomplete="off"
          style="border: 1px solid {colors.line}; background: transparent; color: {colors.text};"
        />
        <button onclick={saveCredential} disabled={savingCredential || !apiKeyInput}>
          {savingCredential ? "Saving…" : "Save"}
        </button>
      </div>
      {#if saveCredentialSuccess}
        <p class="hint" style="color: {colors.green};">Credential saved.</p>
      {/if}
      {#if saveCredentialError}
        <p class="error" style="color: {colors.red};">{saveCredentialError}</p>
      {/if}
    </section>

    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Analysis provider credential</h2>
      {#if analysisCredentialError}
        <p class="error" style="color: {colors.red};">{analysisCredentialError}</p>
      {:else}
        <p class="status" style="color: {hasAnalysisCredential ? colors.green : colors.mut};">
          {hasAnalysisCredential ? "Credential configured" : "No credential configured"}
        </p>
      {/if}
      <div class="credential-form">
        <input
          type="password"
          bind:value={analysisApiKeyInput}
          placeholder="New DeepSeek API key"
          autocomplete="off"
          style="border: 1px solid {colors.line}; background: transparent; color: {colors.text};"
        />
        <button onclick={saveAnalysisCredential} disabled={savingAnalysisCredential || !analysisApiKeyInput}>
          {savingAnalysisCredential ? "Saving…" : "Save"}
        </button>
      </div>
      {#if saveAnalysisCredentialSuccess}
        <p class="hint" style="color: {colors.green};">Credential saved.</p>
      {/if}
      {#if saveAnalysisCredentialError}
        <p class="error" style="color: {colors.red};">{saveAnalysisCredentialError}</p>
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
</style>
