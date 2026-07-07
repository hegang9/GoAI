<template>
  <!-- 统一的渐变 + 颗粒背景，所有页面通过此组件复用，调风格只需改 gradients.css -->
  <div class="gb-root" :class="{'gb-root-fill': fill}">
    <div class="gb-content" :class="contentClass">
      <slot />
    </div>
  </div>
</template>

<script>
// 全局渐变背景容器。
// fill=true 时高度撑满父容器（用于内嵌场景如 ChatLayout 内部），
// 否则默认 min-height: 100vh（用于独立页面如 Login/Menu）。
export default {
  name: "GradientBackground",
  props: {
    fill: {type: Boolean, default: false},
    contentClass: {type: String, default: ""},
  },
};
</script>

<style scoped>
.gb-root-fill {
  min-height: 0;
  height: 100%;
}
</style>
