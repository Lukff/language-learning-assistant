import { Events } from "@wailsio/runtime";
import * as QueueService from "../../bindings/assistente-idiomas/services/queueservice";
import type { QueueItem } from "../../bindings/assistente-idiomas/services/models";

let items: QueueItem[] = $state([]);
let initialized = false;

// jobsStore is the single read point for queue state in the frontend —
// Queue.svelte and the Sidebar.svelte badge read from here, instead of each
// subscribing separately to "job:updated" (see
// docs/superpowers/specs/2026-07-23-story-7-visible-queue-design.md).
// Getters (not a direct export of `items`) because `export let` doesn't
// propagate reactivity across modules in Svelte 5 — functions/objects with a
// getter are the recommended pattern for shared state in .svelte.ts.
export const jobsStore = {
  get items() {
    return items;
  },
  get activeCount() {
    return items.filter((i) => i.status !== "error").length;
  },
};

// refreshJobsStore fetches the queue again right now — exported so that
// whoever just triggered an action that changes a job (e.g. Queue.svelte's
// retry) doesn't need to wait for the next "job:updated" (which only arrives
// when the worker picks up the job on its ~5s poll) to see the result reflected on screen.
export async function refreshJobsStore() {
  items = (await QueueService.ListQueue()) ?? [];
}

// initJobsStore fetches the queue once and subscribes to "job:updated" to
// refetch on every job status transition. Called once in
// App.svelte — repeated calls are a no-op (avoids duplicate
// subscriptions to the event).
export function initJobsStore() {
  if (initialized) return;
  initialized = true;
  refreshJobsStore();
  Events.On("job:updated", refreshJobsStore);
}
