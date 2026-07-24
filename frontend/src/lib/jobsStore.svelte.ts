import { Events } from "@wailsio/runtime";
import * as QueueService from "../../bindings/assistente-idiomas/services/queueservice";
import type { QueueItem } from "../../bindings/assistente-idiomas/services/models";

let items: QueueItem[] = $state([]);
let initialized = false;

// jobsStore é o único ponto de leitura do estado da fila no frontend —
// Queue.svelte e o badge da Sidebar.svelte leem daqui, sem cada um se
// inscrever separadamente em "job:updated" (ver
// docs/superpowers/specs/2026-07-23-historia-7-fila-visivel-design.md).
// Getters (não uma exportação direta de `items`) porque `export let` não
// propaga reatividade entre módulos no Svelte 5 — funções/objetos com
// getter são o padrão recomendado pra estado compartilhado em .svelte.ts.
export const jobsStore = {
  get items() {
    return items;
  },
  get activeCount() {
    return items.filter((i) => i.status !== "erro").length;
  },
};

// refreshJobsStore busca a fila de novo agora — exportada pra quem acabou
// de disparar uma ação que muda um job (ex.: Queue.svelte's retry) não
// precisar esperar o próximo "job:updated" (que só chega quando o worker
// pega o job no poll de ~5s) pra ver o resultado refletido na tela.
export async function refreshJobsStore() {
  items = (await QueueService.ListQueue()) ?? [];
}

// initJobsStore busca a fila uma vez e assina "job:updated" pra refazer a
// busca a cada transição de status de job. Chamado uma única vez em
// App.svelte — chamadas repetidas são no-op (evita inscrições duplicadas
// no evento).
export function initJobsStore() {
  if (initialized) return;
  initialized = true;
  refreshJobsStore();
  Events.On("job:updated", refreshJobsStore);
}
