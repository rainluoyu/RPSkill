<script setup lang="ts">
import { locale, setLocale, t } from '../lib/i18n'
</script>
<template>
  <div class="appearance-options">
    <label>{{ t('界面主题') }}<select :aria-label="t('界面主题')" disabled><option>{{ t('默认主题') }}</option></select></label>
    <label>{{ t('语言') }}<select :value="locale" :aria-label="t('语言')" @change="setLocale(($event.target as HTMLSelectElement).value)"><option value="zh-CN">简体中文</option><option value="en">English</option></select></label>
  </div>
</template>
<style scoped>
.appearance-options { display: grid; gap: 24px; }
.appearance-options label { display: grid; gap: 12px; }
</style>
