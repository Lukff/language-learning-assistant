<script lang="ts">
  import { colors, fonts } from "../theme";
  import * as QueueService from "../../../bindings/assistente-idiomas/services/queueservice";
  import { jobsStore, refreshJobsStore } from "../jobsStore.svelte";

  let retryingId: number | null = $state(null);
  let error: string = $state("");

  // Mesmo formato de Library.svelte (lessonDate é "AAAA-MM-DD" ou
  // "AAAA-MM-DDTHH:MM", sem fuso — não é um timestamp com "Z").
  function formatLessonDateTime(value: string): string {
    const [datePart, timePart] = value.split("T");
    const [year, month, day] = datePart.split("-");
    const formattedDate = `${day}/${month}/${year}`;
    return timePart ? `${formattedDate} ${timePart}` : formattedDate;
  }

  async function retry(lessonId: number) {
    error = "";
    retryingId = lessonId;
    try {
      await QueueService.RetryLesson(lessonId);
      // Não espera o próximo "job:updated" (só chega quando o worker pega
      // o job no poll de ~5s) — atualiza a fila na hora, mesmo padrão do
      // botão "Reprocessar" da Biblioteca.
      await refreshJobsStore();
    } catch (e) {
      error = String(e);
    } finally {
      retryingId = null;
    }
  }
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <h1 style="font-family: {fonts.display};">Fila</h1>

  {#if error}
    <p class="error" style="color: {colors.red};">{error}</p>
  {/if}

  {#if jobsStore.items.length === 0}
    <p style="color: {colors.mut};">Nada na fila no momento.</p>
  {:else}
    <ul>
      {#each jobsStore.items as item (item.lessonId)}
        <li style="background: {colors.surface}; border: 1px solid {colors.line};">
          <div class="main">
            <span class="date" style="color: {colors.text};">{formatLessonDateTime(item.lessonDate)}</span>
            <span class="tutor" style="color: {colors.mut};">{item.tutor} · {item.stage}</span>
          </div>
          {#if item.status === "erro"}
            <div class="status-block">
              <span class="badge" style="color: {colors.red}; background: rgba(224,108,108,.1);">erro</span>
              <span class="error-message" style="color: {colors.mut};">{item.lastError}</span>
              <button onclick={() => retry(item.lessonId)} disabled={retryingId === item.lessonId}>
                {retryingId === item.lessonId ? "Reprocessando…" : "Reprocessar"}
              </button>
            </div>
          {:else}
            <span class="badge" style="color: {colors.blue}; background: rgba(110,168,254,.1);">
              {item.status}
            </span>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
    margin: 0 auto;
    width: 100%;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0 0 1rem;
  }
  .error {
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    border-radius: 0.75rem;
    padding: 0.75rem 1.25rem;
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    min-width: 0;
  }
  .date {
    font-size: 0.85rem;
  }
  .tutor {
    font-size: 0.8rem;
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
  button {
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
</style>
