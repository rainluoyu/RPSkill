<script setup lang="ts">
import { t } from "../lib/i18n"
import type { Perspective, Polishing } from '../lib/types'

const perspective = defineModel<Perspective>('perspective', { required: true })
const polishing = defineModel<Polishing>('polishing', { required: true })
</script>
<template>
  <label>{{ t("叙事人称") }}<select v-model="perspective" :aria-label="t('叙事人称')"><option value="third">{{ t("第三人称") }}</option><option value="first">{{ t("第一人称") }}</option></select></label>
  <label>{{ t("润色等级") }}<select v-model="polishing" :aria-label="t('润色等级')"><option value="off">{{ t("关闭 · 直接续写") }}</option><option value="basic">{{ t("基础 · 保留表达") }}</option><option value="full">{{ t("完全 · 重写表达，保留事实") }}</option></select></label>
</template>
