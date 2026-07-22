<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as LibraryService from "../../../bindings/assistente-idiomas/services/libraryservice";
  import type { Lesson } from "../../../bindings/assistente-idiomas/services/models";

  let { lessonId, onBack }: { lessonId: number; onBack: () => void } = $props();

  let lesson: Lesson | null = $state(null);
  let loading: boolean = $state(true);
  let error: string = $state("");

  function formatLessonDateTime(value: string): string {
    const [datePart, timePart] = value.split("T");
    const [year, month, day] = datePart.split("-");
    const formattedDate = `${day}/${month}/${year}`;
    return timePart ? `${formattedDate} ${timePart}` : formattedDate;
  }

  onMount(async () => {
    try {
      lesson = await LibraryService.GetLesson(lessonId);
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <button class="back" onclick={onBack} style="color: {colors.mut};">← Biblioteca</button>

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else if error}
    <p class="error" style="color: {colors.red};">{error}</p>
  {:else if lesson}
    <h1 style="font-family: {fonts.display};">{formatLessonDateTime(lesson.lessonDate)}</h1>
    <p class="tutor" style="color: {colors.mut};">{lesson.tutor}</p>
    <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -->
    <video controls src={`/media/lesson/${lesson.id}`}>
      <track kind="captions" />
    </video>
  {/if}
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
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
    margin: 0 0 0.25rem;
  }
  .tutor {
    font-size: 0.9rem;
    margin: 0 0 1.5rem;
  }
  video {
    width: 100%;
    border-radius: 0.75rem;
    background: black;
  }
  .error {
    font-size: 0.85rem;
  }
</style>
