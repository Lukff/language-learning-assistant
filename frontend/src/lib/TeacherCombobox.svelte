<script lang="ts">
  import { onMount } from "svelte";
  import * as TeacherService from "../../bindings/assistente-idiomas/services/teacherservice";
  import type { Teacher } from "../../bindings/assistente-idiomas/services/models";

  let { id, value = $bindable("") }: { id: string; value?: string } = $props();

  let teachers: Teacher[] = $state([]);

  onMount(async () => {
    teachers = (await TeacherService.ListTeachers()) ?? [];
  });
</script>

<input {id} list={`${id}-datalist`} type="text" bind:value placeholder="Teacher name" autocomplete="off" />
<datalist id={`${id}-datalist`}>
  {#each teachers as teacher (teacher.id)}
    <option value={teacher.name}></option>
  {/each}
</datalist>

<style>
  input {
    width: 100%;
    padding: 0.5rem;
    margin-bottom: 1rem;
    box-sizing: border-box;
  }
</style>
