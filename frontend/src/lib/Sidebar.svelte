<script lang="ts">
  import { colors, fonts } from "./theme";
  import { jobsStore } from "./jobsStore.svelte";

  type Screen = "library" | "progress" | "queue";

  let { active, onNavigate }: { active: Screen; onNavigate: (screen: Screen) => void } = $props();

  const NAV: { key: Screen; label: string; icon: string }[] = [
    { key: "library", label: "Biblioteca", icon: "▤" },
    { key: "progress", label: "Progresso", icon: "◔" },
    { key: "queue", label: "Fila", icon: "≡" },
  ];
</script>

<aside class="sidebar" style="border-right: 1px solid {colors.line}; background: {colors.surface};">
  <div class="brand">
    <div class="brand-name" style="font-family: {fonts.display}; color: {colors.text};">Replay</div>
    <div class="brand-sub" style="font-family: {fonts.body}; color: {colors.mut};">
      diário de aulas de inglês
    </div>
  </div>
  {#each NAV as item (item.key)}
    <button
      class="nav-item"
      style="
        background: {active === item.key ? colors.surface2 : 'transparent'};
        color: {active === item.key ? colors.text : colors.mut};
        font-family: {fonts.body};
        font-weight: {active === item.key ? 600 : 400};
      "
      onclick={() => onNavigate(item.key)}
    >
      <span class="icon">{item.icon}</span>
      {item.label}
      {#if item.key === "queue" && jobsStore.activeCount > 0}
        <span class="badge" style="background: {colors.blue}; color: {colors.bg};">
          {jobsStore.activeCount}
        </span>
      {/if}
    </button>
  {/each}
</aside>

<style>
  .sidebar {
    width: 13rem;
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    padding: 1rem;
  }
  .brand {
    margin-bottom: 2rem;
    padding: 0 0.5rem;
  }
  .brand-name {
    font-weight: 700;
    font-size: 1rem;
  }
  .brand-sub {
    font-size: 0.75rem;
    margin-top: 0.125rem;
  }
  .nav-item {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    width: 100%;
    padding: 0.625rem 0.75rem;
    border-radius: 0.5rem;
    font-size: 0.875rem;
    margin-bottom: 0.25rem;
    text-align: left;
    border: none;
    cursor: pointer;
  }
  .icon {
    display: inline-block;
    width: 1.1rem;
    text-align: center;
  }
  .badge {
    margin-left: auto;
    font-size: 0.7rem;
    font-weight: 600;
    padding: 0.1rem 0.45rem;
    border-radius: 999px;
    flex-shrink: 0;
  }
</style>
