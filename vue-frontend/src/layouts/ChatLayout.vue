<template>
  <!-- 聊天页统一外壳：左侧导航 + 右侧主区。
   * AIChat 与 ImageRecognition 共用，消除两份重复的容器样式。
   * sidebar 与 main 通过 slot 注入，layout 只负责布局骨架。 -->
  <div class="chat-layout">
    <div class="chat-layout-sidebar">
      <slot name="sidebar" />
    </div>
    <div class="chat-layout-main">
      <slot name="main" />
    </div>
  </div>
</template>

<script>
export default {
  name: "ChatLayout",
};
</script>

<style scoped>
.chat-layout {
  height: 100vh;
  display: flex;
  background: var(--gradient-brand);
  position: relative;
  overflow: hidden;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto,
    "Helvetica Neue", Arial;
  color: var(--color-text);
}

.chat-layout::before {
  content: "";
  position: absolute;
  inset: 0;
  background: url('data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="20" cy="20" r="2" fill="rgba(255,255,255,0.08)"/><circle cx="80" cy="80" r="2" fill="rgba(255,255,255,0.08)"/><circle cx="40" cy="60" r="1" fill="rgba(255,255,255,0.06)"/><circle cx="60" cy="30" r="1.5" fill="rgba(255,255,255,0.06)"/></svg>');
  animation: chat-layout-float 24s ease-in-out infinite;
  opacity: 0.25;
  pointer-events: none;
}

@keyframes chat-layout-float {
  0%,
  100% {
    transform: translateY(0) rotate(0deg);
  }
  50% {
    transform: translateY(-16px) rotate(180deg);
  }
}

.chat-layout-sidebar {
  position: relative;
  z-index: 2;
}

.chat-layout-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  position: relative;
  z-index: 1;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
</style>
