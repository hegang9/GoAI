<template>
  <!-- 聊天页顶部工具条：返回按钮 + 标题 + 可配置的工具区插槽。
   * AIChat 与 ImageRecognition 通过 slot 注入各自所需的控件。 -->
  <header class="chat-topbar">
    <button class="chat-btn chat-btn-ghost" @click="$emit('back')">
      ← 返回
    </button>
    <h2 v-if="title">{{ title }}</h2>
    <slot />
  </header>
</template>

<script>
export default {
  name: "ChatTopBar",
  props: {
    title: {type: String, default: ""},
  },
  emits: ["back"],
};
</script>
