<script lang="ts">
  import { colors } from "./theme";

  let { onOpenSettings }: { onOpenSettings: () => void } = $props();
</script>

<header class="header" style="border-bottom: 1px solid {colors.line};">
  <button
    class="settings-button"
    style="color: {colors.mut};"
    onclick={onOpenSettings}
    aria-label="Configurações"
  >
    ⚙
  </button>
</header>

<style>
  .header {
    height: 3rem;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    padding: 0 1rem;
  }
  .settings-button {
    background: transparent;
    border: none;
    cursor: pointer;
    font-size: 1.1rem;
    line-height: 1;
    padding: 0.25rem;
  }
</style>
