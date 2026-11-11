<script setup lang="ts">
import type { Problem, ReferenceSubject } from '#/api/console/problem';
defineProps<{
  problems?: null | Problem[];
  references?: null | ReferenceSubject[];
}>();
</script>
<template>
  <ul
    v-if="problems?.length"
    role="alert"
    class="my-3 space-y-2 rounded border border-red-300 p-3 text-red-700"
  >
    <li v-for="(problem, index) in problems" :key="index">
      <div class="font-medium">{{ problem.message }}</div>
      <div class="text-sm">
        {{
          [
            problem.host,
            problem.subject?.kind,
            problem.subject?.id,
            problem.subject?.path,
          ]
            .filter(Boolean)
            .join(' · ')
        }}<span v-if="problem.subject?.line">
          :{{ problem.subject.line }}:{{ problem.subject.col }}</span>
      </div>
      <div v-if="problem.next" class="text-sm">{{ problem.next }}</div>
    </li>
  </ul>
  <ul
    v-if="references?.length"
    role="alert"
    class="my-3 space-y-1 rounded border border-red-300 p-3 text-red-700"
  >
    <li v-for="(subject, index) in references" :key="index">
      {{ [subject.kind, subject.id, subject.path].filter(Boolean).join(' · ')
      }}<span v-if="subject.line"> :{{ subject.line }}:{{ subject.col }}</span>
    </li>
  </ul>
</template>
