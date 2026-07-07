<template>
  <!-- 登录/注册共用的卡片外壳：标题 + 表单容器。
   * 通过 slot 把表单字段交还给父组件，仅保留共有的视觉与动画。 -->
  <el-card class="auth-card">
    <template #header>
      <div class="auth-card-header">
        <h2>{{ title }}</h2>
      </div>
    </template>
    <slot />
  </el-card>
</template>

<script>
export default {
  name: "AuthCard",
  props: {
    title: {type: String, required: true},
  },
};
</script>

<style scoped>
.auth-card {
  width: 420px;
  background: var(--surface-glass);
  backdrop-filter: blur(10px);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
  border: 1px solid rgba(255, 255, 255, 0.2);
  animation: auth-slide-in 0.8s var(--ease-out);
  position: relative;
  z-index: 1;
}

@keyframes auth-slide-in {
  from {
    opacity: 0;
    transform: translateY(30px) scale(0.9);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}

.auth-card-header {
  text-align: center;
  padding: 30px 0 20px 0;
}

.auth-card-header h2 {
  margin: 0;
  background: var(--gradient-brand);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-size: 28px;
  font-weight: 600;
}

/* 卡片内表单元素统一间距与圆角 */
.auth-card :deep(.el-form-item) {
  margin-bottom: 20px;
}

.auth-card :deep(.el-button) {
  height: 48px;
  border-radius: var(--radius-md);
  font-weight: 600;
  transition: all 0.3s var(--ease-out);
}

/* 认证页主操作按钮需要略微外扩，以抵消表单 label 预留宽度带来的视觉偏移。 */
.auth-card :deep(.auth-primary-action.el-button--primary) {
  margin-left: -35px;
  margin-right: -35px;
}

.auth-card :deep(.auth-link-action.el-button--text) {
  gap: 0;
}

.auth-card :deep(.el-button:hover) {
  transform: translateY(-2px);
  box-shadow: var(--shadow-brand);
}

.auth-card :deep(.el-input) {
  transition: all 0.3s var(--ease-out);
}
.auth-card :deep(.el-input:focus-within) {
  transform: scale(1.02);
}
</style>
