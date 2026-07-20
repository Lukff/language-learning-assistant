<script lang="ts">
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import { colors, fonts } from "./lib/theme";

  type Screen = "library" | "progress" | "queue";

  let screen: Screen = $state("library");
</script>

<div class="shell" style="background: {colors.bg}; font-family: {fonts.body};">
  <Sidebar active={screen} onNavigate={(s) => (screen = s)} />
  <main class="main">
    <Header />
    <div class="content">
      {#if screen === "library"}
        <Library />
      {:else if screen === "progress"}
        <Progress />
      {:else}
        <Queue />
      {/if}
    </div>
  </main>
</div>

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
