<script setup lang="ts">
import { computed } from 'vue'
import { renderMarkdown } from '../lib/markdown'

const props = defineProps<{ text: string }>()
const html = computed(() => renderMarkdown(props.text))
</script>

<template>
  <!-- All untrusted Markdown goes through the shared sanitizer. -->
  <div class="markdown-content" v-html="html"></div>
</template>
