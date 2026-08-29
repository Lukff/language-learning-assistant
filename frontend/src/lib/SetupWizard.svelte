<script lang="ts">
  import { colors, fonts } from "./theme";
  import * as SetupService from "../../bindings/assistente-idiomas/services/setupservice";
  import * as ImportService from "../../bindings/assistente-idiomas/services/importservice";

  let { onComplete }: { onComplete: () => void } = $props();

  type Step = "folder" | "credentials" | "scanning";
  let step: Step = $state("folder");
  let storageRoot: string = $state("");
  let apiKey: string = $state("");
  let error: string = $state("");
  let choosing: boolean = $state(false);
  let saving: boolean = $state(false);

  async function chooseFolder() {
    error = "";
    choosing = true;
    try {
      const dir = await SetupService.ChooseStorageFolder();
      if (dir) {
        storageRoot = dir;
        step = "credentials";
      }
    } catch (e) {
      error = String(e);
    } finally {
      choosing = false;
    }
  }

  async function complete() {
    error = "";
    saving = true;
    try {
      await SetupService.CompleteSetup(storageRoot, apiKey);
      step = "scanning";
      // A scan failure shouldn't block the wizard or hide the app from the
      // user — setup is already saved; the next "Sync folder" in the
      // Library will try again.
      try {
        await ImportService.ScanFolder();
      } catch {
        // deliberately ignored — see comment above
      }
      onComplete();
    } catch (e) {
      error = String(e);
    } finally {
      saving = false;
    }
  }
</script>

<div class="wizard" style="background: {colors.bg}; font-family: {fonts.body}; color: {colors.text};">
  <div class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
    {#if step === "folder"}
      <h1 style="font-family: {fonts.display};">Where do your lessons live?</h1>
      <p style="color: {colors.mut};">
        Choose the synced folder (for example, inside Google Drive) where imported videos
        will be stored.
      </p>
      <button onclick={chooseFolder} disabled={choosing}>
        {choosing ? "Opening…" : "Choose folder"}
      </button>
    {:else if step === "credentials"}
      <h1 style="font-family: {fonts.display};">Folder selected</h1>
      <p class="mono" style="color: {colors.mut};">{storageRoot}</p>
      <h2 style="font-family: {fonts.display};">API key (ElevenLabs)</h2>
      <input type="password" bind:value={apiKey} placeholder="sk-..." />
      <button onclick={complete} disabled={saving || apiKey.length === 0}>
        {saving ? "Saving…" : "Finish"}
      </button>
    {:else}
      <h1 style="font-family: {fonts.display};">Looking for lessons in the folder…</h1>
      <p style="color: {colors.mut};">
        Checking whether lesson videos already exist in {storageRoot}.
      </p>
    {/if}
    {#if error}
      <p class="error" style="color: {colors.red};">{error}</p>
    {/if}
  </div>
</div>

<style>
  .wizard {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100vh;
  }
  .card {
    padding: 2rem;
    border-radius: 0.75rem;
    max-width: 28rem;
    width: 100%;
  }
  h1 {
    font-size: 1.25rem;
    margin: 0 0 0.5rem;
  }
  h2 {
    font-size: 1rem;
    margin: 1.5rem 0 0.5rem;
  }
  .mono {
    font-family: "JetBrains Mono", ui-monospace, monospace;
    font-size: 0.8rem;
    word-break: break-all;
  }
  input {
    width: 100%;
    padding: 0.5rem;
    margin-top: 0.5rem;
    box-sizing: border-box;
  }
  button {
    margin-top: 1rem;
    padding: 0.5rem 1rem;
    cursor: pointer;
  }
  .error {
    margin-top: 1rem;
    font-size: 0.85rem;
  }
</style>
