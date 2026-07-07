<template>
  <!-- 聊天输入区：textarea + 发送按钮。
   * Enter 发送（不含 Shift）、Shift+Enter 换行由浏览器默认行为处理。 -->
  <div class="chat-input-area">
    <textarea
      v-model="text"
      placeholder="请输入你的问题..."
      :disabled="disabled"
      ref="inputRef"
      rows="1"
      @keydown.enter.exact.prevent="submit"
    ></textarea>
    <button
      type="button"
      class="chat-btn chat-btn-brand send-btn"
      :disabled="!text.trim() || disabled"
      @click="submit"
    >
      {{ disabled ? "发送中..." : "发送" }}
    </button>
  </div>
</template>

<script>
import {ref} from "vue";

export default {
  name: "ChatInput",
  props: {
    disabled: {type: Boolean, default: false},
  },
  emits: ["send"],
  setup(props, {emit, expose}) {
    const text = ref("");
    const inputRef = ref(null);

    const submit = () => {
      const value = text.value.trim();
      if (!value || props.disabled) return;
      emit("send", value);
      text.value = "";
    };

    const focus = () => {
      inputRef.value?.focus();
    };

    expose({focus});

    return {text, inputRef, submit, focus};
  },
};
</script>

<style scoped>
.chat-input-area {
  padding: var(--space-lg);
  background: var(--surface-glass-strong);
  backdrop-filter: blur(8px);
  border-top: 1px solid var(--surface-border);
  position: relative;
  z-index: 1;
}

.chat-input-area textarea {
  width: 100%;
  resize: none;
  border: 2px solid var(--surface-border);
  border-radius: var(--radius-md);
  padding: 14px 16px;
  font-size: 15px;
  outline: none;
  background: var(--surface-glass-strong);
  color: var(--color-text);
  transition: all 0.18s var(--ease-out);
  min-height: 20px;
  max-height: 160px;
  box-shadow: var(--shadow-sm);
}

.chat-input-area textarea:focus {
  border-color: #409eff;
  box-shadow: 0 8px 30px rgba(64, 158, 255, 0.08);
  transform: translateY(-1px);
}

.send-btn {
  position: absolute;
  right: 36px;
  bottom: 30px;
  padding: 12px 22px;
  border-radius: var(--radius-pill);
  font-size: 15px;
}
</style>
