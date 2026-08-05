<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as LibraryService from "../../../bindings/assistente-idiomas/services/libraryservice";
  import * as AnalysisService from "../../../bindings/assistente-idiomas/services/analysisservice";
  import type { Lesson, Transcript, CorrectionsResult } from "../../../bindings/assistente-idiomas/services/models";
  import EditLessonModal from "../EditLessonModal.svelte";

  let { lessonId, onBack }: { lessonId: number; onBack: () => void } = $props();

  let lesson: Lesson | null = $state(null);
  let loading: boolean = $state(true);
  let lessonError: string = $state("");
  // Erro de uma ação pontual (reprocessar transcrição) — não deve
  // derrubar o vídeo/painel, por isso fica separado de lessonError.
  let actionError: string = $state("");

  let transcript: Transcript | null = $state(null);
  let loadingTranscript: boolean = $state(false);

  let retrying: boolean = $state(false);

  let corrections: CorrectionsResult | null = $state(null);
  let analyzingCorrections: boolean = $state(false);
  let correctionsError: string = $state("");

  let editing: boolean = $state(false);

  async function onLessonSaved() {
    editing = false;
    await refreshAfterSpeakerChange();
  }

  // Re-busca lesson e corrections sem fechar o modal — usada depois de
  // trocar o falante do aluno (chooseSpeaker no EditLessonModal), pra não
  // descartar edições de data/professor ainda não salvas nessa mesma sessão
  // do modal.
  async function refreshAfterSpeakerChange() {
    try {
      lesson = await LibraryService.GetLesson(lessonId);
      await fetchCorrectionsIfReady();
    } catch (e) {
      actionError = String(e);
    }
  }

  let videoEl: HTMLVideoElement | undefined = $state();
  let currentTime: number = $state(0);
  let rowRefs: (HTMLElement | null)[] = [];

  function formatLessonDateTime(value: string): string {
    const [datePart, timePart] = value.split("T");
    const [year, month, day] = datePart.split("-");
    const formattedDate = `${day}/${month}/${year}`;
    return timePart ? `${formattedDate} ${timePart}` : formattedDate;
  }

  function formatDuration(seconds: number | null): string {
    if (seconds == null) return "";
    const totalMinutes = Math.round(seconds / 60);
    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;
    return hours > 0 ? `${hours}h ${minutes}min` : `${minutes}min`;
  }

  // Ordem de primeira fala — determinística, não depende de ordem de mapa.
  // "Speaker A"/"Speaker B" (ou C, D... em diarização com ruído) são os
  // rótulos neutros exibidos antes do usuário escolher quem é o aluno.
  const speakerOrder = $derived.by(() => {
    const seen: string[] = [];
    for (const u of transcript?.utterances ?? []) {
      if (!seen.includes(u.speaker)) seen.push(u.speaker);
    }
    return seen;
  });

  function neutralLabel(speaker: string): string {
    const idx = speakerOrder.indexOf(speaker);
    return `Speaker ${String.fromCharCode(65 + (idx < 0 ? 0 : idx))}`;
  }

  type Role = "aluno" | "tutor" | "neutro";

  function roleFor(speaker: string): Role {
    if (!lesson?.studentSpeakerLabel) return "neutro";
    return speaker === lesson.studentSpeakerLabel ? "aluno" : "tutor";
  }

  function labelFor(speaker: string, role: Role): string {
    if (role === "aluno") return "Você";
    if (role === "tutor") return "Tutor";
    return neutralLabel(speaker);
  }

  // Última utterance cujo start já passou — busca linear, poucas centenas
  // de falas por aula, custo irrelevante a cada tick de timeupdate.
  const currentIndex = $derived.by(() => {
    const utterances = transcript?.utterances ?? [];
    let idx = -1;
    for (let i = 0; i < utterances.length; i++) {
      if (utterances[i].startSeconds <= currentTime) idx = i;
      else break;
    }
    return idx;
  });

  $effect(() => {
    const el = rowRefs[currentIndex];
    el?.scrollIntoView({ block: "nearest" });
  });

  function onTimeUpdate() {
    if (videoEl) currentTime = videoEl.currentTime;
  }

  // Só ajusta a posição (seek) — não força play nem pause, pra não
  // surpreender quem só quer conferir o timestamp.
  function seekTo(startSeconds: number) {
    if (videoEl) videoEl.currentTime = startSeconds;
  }

  async function fetchTranscriptIfReady() {
    if (!lesson || lesson.status !== "pronta") {
      transcript = null;
      return;
    }
    loadingTranscript = true;
    try {
      transcript = await LibraryService.GetTranscript(lessonId);
    } catch {
      transcript = null;
    } finally {
      loadingTranscript = false;
    }
  }

  async function fetchCorrectionsIfReady() {
    if (!lesson || lesson.status !== "pronta" || !lesson.studentSpeakerLabel) {
      corrections = null;
      return;
    }
    try {
      corrections = await AnalysisService.GetCorrections(lessonId);
    } catch {
      corrections = null;
    }
  }

  async function analyzeCorrections() {
    correctionsError = "";
    analyzingCorrections = true;
    try {
      corrections = await AnalysisService.AnalyzeCorrections(lessonId);
    } catch (e) {
      correctionsError = String(e);
    } finally {
      analyzingCorrections = false;
    }
  }

  async function reprocessCorrections() {
    const confirmed = confirm("Isso sobrescreve a análise atual e gera uma nova chamada à API. Continuar?");
    if (!confirmed) return;
    correctionsError = "";
    analyzingCorrections = true;
    try {
      corrections = await AnalysisService.ReprocessCorrections(lessonId);
    } catch (e) {
      correctionsError = String(e);
    } finally {
      analyzingCorrections = false;
    }
  }

  async function retry() {
    actionError = "";
    retrying = true;
    try {
      await LibraryService.RetryLesson(lessonId);
      lesson = await LibraryService.GetLesson(lessonId);
      await fetchTranscriptIfReady();
    } catch (e) {
      actionError = String(e);
    } finally {
      retrying = false;
    }
  }

  onMount(async () => {
    try {
      lesson = await LibraryService.GetLesson(lessonId);
      await fetchTranscriptIfReady();
      await fetchCorrectionsIfReady();
    } catch (e) {
      lessonError = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <button class="back" onclick={onBack} style="color: {colors.mut};">← Biblioteca</button>

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else if lessonError}
    <p class="error" style="color: {colors.red};">{lessonError}</p>
  {:else if lesson}
    {#if actionError}
      <p class="action-error" style="color: {colors.red}; border: 1px solid {colors.red};">{actionError}</p>
    {/if}

    <div class="header-row">
      <h1 style="font-family: {fonts.display};">{formatLessonDateTime(lesson.lessonDate)}</h1>
      <span class="meta" style="color: {colors.mut}; font-family: {fonts.mono};"
        >{lesson.tutor}{formatDuration(lesson.durationSeconds) ? ` · ${formatDuration(lesson.durationSeconds)}` : ""}</span
      >
      <button onclick={() => (editing = true)}>Editar</button>
      {#if lesson.videoMissing}
        <span
          class="video-missing-badge"
          style="color: {colors.amber}; background: rgba(227,164,76,.1);"
        >
          vídeo não encontrado na pasta atual
        </span>
      {/if}
    </div>

    <div class="grid">
      <div>
        <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -->
        <video
          bind:this={videoEl}
          ontimeupdate={onTimeUpdate}
          controls
          src={`/media/lesson/${lesson.id}`}
          style="border: 1px solid {colors.line};"
        >
          <track kind="captions" />
        </video>
        <p class="hint" style="color: {colors.mut};">
          Clique em qualquer fala ao lado para pular o vídeo até aquele momento.
        </p>
      </div>

      <div class="panel" style="background: {colors.surface}; border: 1px solid {colors.line};">
        {#if lesson.status === "processando"}
          <p class="panel-message" style="color: {colors.mut};">Transcrição em processamento…</p>
        {:else if lesson.status === "erro"}
          <p class="panel-message" style="color: {colors.red};">{lesson.errorMessage}</p>
          <button onclick={retry} disabled={retrying}>
            {retrying ? "Reprocessando…" : "Reprocessar"}
          </button>
        {:else if loadingTranscript}
          <p class="panel-message" style="color: {colors.mut};">Carregando transcrição…</p>
        {:else if !transcript || !transcript.utterances || transcript.utterances.length === 0}
          <p class="panel-message" style="color: {colors.mut};">Transcrição em processamento…</p>
        {:else}
          {#if !lesson.studentSpeakerLabel}
            <p class="hint" style="color: {colors.mut};">
              Escolha quem é você em "Editar" para habilitar a análise de correções.
            </p>
          {:else}
            <div class="corrections-actions">
              {#if corrections?.analyzed}
                <button onclick={reprocessCorrections} disabled={analyzingCorrections}>
                  {analyzingCorrections ? "Reprocessando…" : "Reprocessar correções"}
                </button>
              {:else}
                <button onclick={analyzeCorrections} disabled={analyzingCorrections}>
                  {analyzingCorrections ? "Analisando…" : "Analisar correções"}
                </button>
              {/if}
              {#if correctionsError}
                <p class="error" style="color: {colors.red};">{correctionsError}</p>
              {/if}
            </div>
          {/if}

          <div class="transcript">
            {#each transcript.utterances as utterance, i (i)}
              {@const role = roleFor(utterance.speaker)}
              {@const correctionsForRow = corrections?.items?.filter((c) => c.utteranceIndex === i) ?? []}
              {@const inlineCorrection = role === "aluno" ? correctionsForRow.find((c) => c.wrong) : undefined}
              {@const fallbackCorrections = correctionsForRow.filter((c) => c !== inlineCorrection)}
              <button
                bind:this={rowRefs[i]}
                class="row"
                onclick={() => seekTo(utterance.startSeconds)}
                style="background: {i === currentIndex ? colors.surface2 : 'transparent'}; border-left: 3px solid {role === 'aluno' ? colors.blue : 'transparent'};"
              >
                <span
                  class="speaker-label"
                  style="color: {role === 'aluno' ? colors.blue : role === 'tutor' ? colors.green : colors.mut}; font-family: {fonts.body};"
                >
                  {labelFor(utterance.speaker, role)}
                </span>
                {#if inlineCorrection}
                  <p class="text" style="color: {colors.text};">{inlineCorrection.before}<span
                      class="corrected-original"
                      style="color: {colors.mut};">{inlineCorrection.wrong}</span
                    > <span class="corrected-fix" style="color: {colors.amber};" title={inlineCorrection.explanation}
                      >{inlineCorrection.correction}</span
                    >{inlineCorrection.after}</p>
                {:else}
                  <p class="text" style="color: {colors.text};">{utterance.text}</p>
                {/if}
                {#each fallbackCorrections as fallback}
                  <p class="correction-fallback" style="color: {colors.mut};">
                    ⚠ correção não localizada: "{fallback.original}" → "{fallback.correction}" — {fallback.explanation}
                  </p>
                {/each}
              </button>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

{#if editing && lesson}
  <EditLessonModal
    lessonId={lesson.id}
    initialLessonDate={lesson.lessonDate}
    initialTeacherName={lesson.tutor}
    speakerOptions={speakerOrder}
    currentStudentSpeaker={lesson.studentSpeakerLabel}
    hasAnalysisResults={corrections?.analyzed ?? false}
    onSaved={onLessonSaved}
    onSpeakerChanged={refreshAfterSpeakerChange}
    onClose={() => (editing = false)}
  />
{/if}

<style>
  .screen {
    padding: 2rem;
    max-width: 100rem;
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
  .header-row {
    display: flex;
    align-items: baseline;
    gap: 0.75rem;
    flex-wrap: wrap;
    margin-bottom: 1.25rem;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0;
  }
  .meta {
    font-size: 0.85rem;
  }
  .video-missing-badge {
    font-size: 0.75rem;
    padding: 0.25rem 0.6rem;
    border-radius: 999px;
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 1.25rem;
  }
  @media (min-width: 960px) {
    .grid {
      grid-template-columns: 2fr 1fr;
      align-items: start;
    }
  }
  video {
    width: 100%;
    border-radius: 0.75rem;
    background: black;
    aspect-ratio: 16 / 9;
    max-height: calc(100vh - 14rem);
  }
  .hint {
    font-size: 0.75rem;
    margin-top: 0.75rem;
    line-height: 1.5;
  }
  .panel {
    border-radius: 0.75rem;
    padding: 1rem;
    max-height: calc(100vh - 14rem);
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .panel-message {
    font-size: 0.9rem;
  }
  .corrections-actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin-bottom: 0.5rem;
  }
  .corrections-actions button {
    font-size: 0.8rem;
    padding: 0.4rem 0.8rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
  .transcript {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }
  .row {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.15rem;
    text-align: left;
    background: none;
    border: none;
    border-radius: 0.5rem;
    padding: 0.5rem 0.75rem;
    cursor: pointer;
    width: 100%;
  }
  .speaker-label {
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-weight: 600;
  }
  .text {
    font-size: 0.9rem;
    line-height: 1.5;
    margin: 0;
  }
  .corrected-original {
    text-decoration: line-through;
  }
  .corrected-fix {
    font-weight: 600;
  }
  .correction-fallback {
    font-size: 0.75rem;
    font-style: italic;
    margin: 0 0.75rem 0.25rem;
  }
  .error {
    font-size: 0.85rem;
  }
  .action-error {
    font-size: 0.85rem;
    border-radius: 0.5rem;
    padding: 0.5rem 0.75rem;
    margin-bottom: 1rem;
  }
</style>
