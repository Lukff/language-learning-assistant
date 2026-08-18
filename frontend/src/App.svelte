<script lang="ts">
  import { onMount } from "svelte";
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import LessonDetail from "./lib/screens/LessonDetail.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import Settings from "./lib/screens/Settings.svelte";
  import Teachers from "./lib/screens/Teachers.svelte";
  import Topics from "./lib/screens/Topics.svelte";
  import SetupWizard from "./lib/SetupWizard.svelte";
  import { colors, fonts } from "./lib/theme";
  import { initJobsStore } from "./lib/jobsStore.svelte";
  import * as SetupService from "../bindings/assistente-idiomas/services/setupservice";

  type NavScreen = "library" | "progress" | "queue";
  type Route =
    | { screen: "library" }
    | { screen: "lesson-detail"; lessonId: number }
    | { screen: "progress" }
    | { screen: "queue" }
    | { screen: "settings" }
    | { screen: "teachers" }
    | { screen: "topics" };

  let route: Route = $state({ screen: "library" });
  let checkingFirstRun = $state(true);
  let firstRun = $state(false);

  function navigate(screen: NavScreen) {
    route = { screen };
  }

  function openLesson(lessonId: number) {
    route = { screen: "lesson-detail", lessonId };
  }

  function openTeachers() {
    route = { screen: "teachers" };
  }

  function openTopics() {
    route = { screen: "topics" };
  }

  onMount(async () => {
    initJobsStore();
    try {
      firstRun = await SetupService.IsFirstRun();
    } finally {
      checkingFirstRun = false;
    }
  });
</script>

{#if checkingFirstRun}
  <div class="shell" style="background: {colors.bg};"></div>
{:else if firstRun}
  <SetupWizard onComplete={() => (firstRun = false)} />
{:else}
  <div class="shell" style="background: {colors.bg}; font-family: {fonts.body};">
    <Sidebar
      active={route.screen === "lesson-detail" ||
      route.screen === "settings" ||
      route.screen === "teachers" ||
      route.screen === "topics"
        ? "library"
        : route.screen}
      onNavigate={navigate}
    />
    <main class="main">
      <Header onOpenSettings={() => (route = { screen: "settings" })} />
      <div class="content">
        {#if route.screen === "library"}
          <Library onOpenLesson={openLesson} onOpenTeachers={openTeachers} onOpenTopics={openTopics} />
        {:else if route.screen === "lesson-detail"}
          <LessonDetail lessonId={route.lessonId} onBack={() => navigate("library")} />
        {:else if route.screen === "progress"}
          <Progress />
        {:else if route.screen === "settings"}
          <Settings />
        {:else if route.screen === "teachers"}
          <Teachers onBack={() => navigate("library")} />
        {:else if route.screen === "topics"}
          <Topics onBack={() => navigate("library")} />
        {:else}
          <Queue />
        {/if}
      </div>
    </main>
  </div>
{/if}

<style>
  .shell {
    display: flex;
    width: 100%;
    height: 100vh;
  }
  .main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .content {
    flex: 1;
    overflow-y: auto;
  }
</style>
