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

  onMount(async () => {
    try {
      await Promise.all([loadStorageRoot(), loadCredentialStatus()]);
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
