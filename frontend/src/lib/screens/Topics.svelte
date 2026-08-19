<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as TopicsService from "../../../bindings/assistente-idiomas/services/topicsservice";
  import type { Topic } from "../../../bindings/assistente-idiomas/services/models";

  let { onBack }: { onBack: () => void } = $props();

  let topics: Topic[] = $state([]);
  let loading: boolean = $state(true);
  let topicsError: string = $state("");
  let topicRenameDrafts: Record<number, string> = $state({});
  let renamingTopicId: number | null = $state(null);
  let topicRenameErrors: Record<number, string> = $state({});
  let deletingTopicId: number | null = $state(null);
  let topicDeleteErrors: Record<number, string> = $state({});

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

  async function deleteTopic(topic: Topic) {
    const confirmed = confirm(`Excluir o tópico "${topic.name}"? Ele será removido de todas as aulas que o usam.`);
    if (!confirmed) return;
    topicDeleteErrors = { ...topicDeleteErrors, [topic.id]: "" };
    deletingTopicId = topic.id;
    try {
      await TopicsService.DeleteTopic(topic.id);
      await loadTopics();
    } catch (e) {
      topicDeleteErrors = { ...topicDeleteErrors, [topic.id]: String(e) };
    } finally {
      deletingTopicId = null;
    }
  }

  onMount(async () => {
    try {
      await loadTopics();
    } catch (e) {
      topicsError = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <button class="back" onclick={onBack} style="color: {colors.mut};">← Biblioteca</button>

  <h1 style="font-family: {fonts.display};">Tópicos</h1>

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else}
    {#if topicsError}
      <p class="error" style="color: {colors.red};">{topicsError}</p>
    {/if}
    {#if topics.length === 0}
      <p class="hint" style="color: {colors.mut};">Nenhum tópico cadastrado ainda.</p>
    {:else}
      <ul class="topic-list">
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
            <button
              onclick={() => deleteTopic(topic)}
              disabled={deletingTopicId === topic.id}
              style="color: {colors.red};"
            >
              {deletingTopicId === topic.id ? "Excluindo…" : "Excluir"}
            </button>
            {#if topicRenameErrors[topic.id]}
              <p class="error" style="color: {colors.red};">{topicRenameErrors[topic.id]}</p>
            {/if}
            {#if topicDeleteErrors[topic.id]}
              <p class="error" style="color: {colors.red};">{topicDeleteErrors[topic.id]}</p>
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
  .topic-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .topic-list li {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .topic-list input {
    flex: 1;
    min-width: 10rem;
    padding: 0.4rem 0.6rem;
    border-radius: 0.5rem;
  }
</style>
