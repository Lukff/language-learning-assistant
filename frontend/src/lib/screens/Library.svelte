<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as ImportService from "../../../bindings/assistente-idiomas/services/importservice";
  import type { PendingImport } from "../../../bindings/assistente-idiomas/services/models";
  import ImportConfirmModal from "../ImportConfirmModal.svelte";

  let pending: PendingImport[] = $state([]);
  let loading: boolean = $state(true);
  let syncing: boolean = $state(false);
  let syncMessage: string = $state("");
  let error: string = $state("");
  let reviewing: PendingImport | null = $state(null);

  async function loadPending() {
    try {
      pending = (await ImportService.ListPendingImports()) ?? [];
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  }

  async function syncFolder() {
    error = "";
    syncMessage = "";
    syncing = true;
    try {
      const summary = await ImportService.ScanFolder();
      syncMessage = `${summary.new} novas, ${summary.updated} atualizadas, ${summary.errors} erros`;
      await loadPending();
    } catch (e) {
      error = String(e);
    } finally {
      syncing = false;
    }
  }

  function closeReview() {
    reviewing = null;
  }

  async function onConfirmed() {
    reviewing = null;
    await loadPending();
  }

  onMount(loadPending);
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
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

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else if pending.length > 0}
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
  {:else}
    <p style="color: {colors.mut};">Nenhuma aula importada ainda.</p>
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
  .pending {
    border-radius: 0.75rem;
    padding: 1rem 1.25rem;
  }
  .pending h2 {
    font-size: 1rem;
    margin: 0 0 0.75rem;
  }
  .pending ul {
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
  .path {
    font-size: 0.8rem;
    word-break: break-all;
  }
</style>
