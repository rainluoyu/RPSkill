<script setup lang="ts">
import { t } from '../lib/i18n'
defineProps<{ identity: 'director' | 'chat' }>()
</script>
<template>
  <svg viewBox="0 0 64 64" role="img" :aria-label="t('{0}的头像', [identity === 'director' ? t('导演') : t('交流')])" class="builtin-portrait">
    <template v-if="identity === 'director'">
      <rect width="64" height="64" rx="10" fill="#322b25" />
      <g fill="none" stroke="#e6bc72" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round">
        <path d="M14 29h36v20a3 3 0 0 1-3 3H17a3 3 0 0 1-3-3z" fill="#4d3e2b" />
        <path d="m13 21 34-9 3 10-35 9z" fill="#4d3e2b" />
        <path d="m22 19 6 8m5-11 6 8M24 39h16M24 45h10" />
      </g>
      <path d="m51 4 1.8 4.2L57 10l-4.2 1.8L51 16l-1.8-4.2L45 10l4.2-1.8z" fill="#f6d793" />
    </template>
    <template v-else>
      <rect width="64" height="64" rx="10" fill="#233230" />
      <g stroke="#83c9b8" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round">
        <path d="M32 25h17a5 5 0 0 1 5 5v15a5 5 0 0 1-5 5h-3v7l-9-7h-5a5 5 0 0 1-5-5V30a5 5 0 0 1 5-5Z" fill="#34544c" />
        <path d="M14 12h23a5 5 0 0 1 5 5v16a5 5 0 0 1-5 5H26l-9 7v-7h-3a5 5 0 0 1-5-5V17a5 5 0 0 1 5-5Z" fill="#293e38" />
      </g>
      <g fill="#c0e4d8"><circle cx="18" cy="25" r="2" /><circle cx="26" cy="25" r="2" /><circle cx="34" cy="25" r="2" /></g>
    </template>
  </svg>
</template>
<style scoped>
.builtin-portrait { display: block; width: 100%; height: 100%; }
</style>
