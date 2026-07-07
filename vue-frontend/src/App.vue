<template>
  <div id="app">
    <router-view v-slot="{Component, route}">
      <transition
        name="page"
        @before-enter="onPageBeforeEnter"
        @after-enter="onPageAfterEnter"
        @after-leave="onPageAfterLeave"
      >
        <!-- route key 强制每次导航挂载新页面，避免离场动画结束后停留在空节点。 -->
        <component :is="Component" :key="route.fullPath" />
      </transition>
    </router-view>
  </div>
</template>

<script>
import {logFrontendEvent} from "./utils/frontendLogger";

export default {
  name: "App",
  methods: {
    // 顶层页面过渡诊断日志：用于判断白屏是否卡在 router-view/transition 阶段。
    onPageBeforeEnter(element) {
      logFrontendEvent("page:beforeEnter", {
        className: element?.className || "",
        childCount: element?.childElementCount || 0,
      });
    },
    onPageAfterEnter(element) {
      logFrontendEvent("page:afterEnter", {
        className: element?.className || "",
        textPreview: (element?.innerText || "").slice(0, 80),
      });
    },
    onPageAfterLeave(element) {
      logFrontendEvent("page:afterLeave", {
        className: element?.className || "",
      });
    },
  },
};
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

html,
body {
  height: 100%;
  font-family: "Helvetica Neue", Helvetica, "PingFang SC", "Hiragino Sans GB",
    "Microsoft YaHei", "微软雅黑", Arial, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  background: #f5f7fa;
}

#app {
  height: 100%;
}

/* 页面切换动画 */
.page-enter-active,
.page-leave-active {
  transition: all 0.4s cubic-bezier(0.55, 0, 0.1, 1);
}

.page-enter-from {
  opacity: 0;
  transform: translateX(30px);
}

.page-leave-to {
  opacity: 0;
  transform: translateX(-30px);
}

/* 全局滚动条样式 */
::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}

::-webkit-scrollbar-track {
  background: rgba(0, 0, 0, 0.05);
  border-radius: 4px;
}

::-webkit-scrollbar-thumb {
  background: rgba(102, 126, 234, 0.3);
  border-radius: 4px;
  transition: background 0.3s ease;
}

::-webkit-scrollbar-thumb:hover {
  background: rgba(102, 126, 234, 0.5);
}

/* Element Plus 组件样式覆盖 */
.el-button {
  font-weight: 500;
  border-radius: var(--radius-sm, 8px);
}

.el-card {
  border-radius: var(--radius-xl, 20px);
}

.el-message {
  border-radius: var(--radius-sm, 8px);
}

/* 响应式：窄屏去掉横向滑动动画，避免位移感 */
@media (max-width: 768px) {
  .page-enter-from,
  .page-leave-to {
    transform: translateX(0);
    opacity: 0;
  }

  .page-enter-active,
  .page-leave-active {
    transition: opacity 0.3s ease;
  }
}
</style>
