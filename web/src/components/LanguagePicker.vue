<script setup lang="ts">
import { locale, setLocale, t } from '../lib/i18n'
</script>
<template>
  <label class="language-picker"><span class="sr-only">{{ t('语言') }}</span><select :value="locale" :aria-label="t('语言')" @change="setLocale(($event.target as HTMLSelectElement).value)"><option value="zh-CN">简体中文</option><option value="en">English</option></select></label>
</template>
<style scoped>
.language-picker { display: inline-flex; flex-shrink: 0; }
.language-picker select { width: auto; padding: 6px 9px; font-size: 12px; }
</style>
