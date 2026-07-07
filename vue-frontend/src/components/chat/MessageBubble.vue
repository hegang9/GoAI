<template>
  <!-- 单条消息气泡：支持文本/markdown、流式指示、TTS 按钮、附加图片。
   * UI 微调气泡样式只需改 assets/styles/chat.css。 -->
  <div
    :class="['chat-message', isUser ? 'chat-message-user' : 'chat-message-ai']"
  >
    <div class="chat-message-header">
      <b>{{ isUser ? "你" : "AI" }}:</b>
      <button
        v-if="!isUser && showTts"
        class="chat-tts-btn"
        @click="$emit('tts', content)"
      >
        🔊
      </button>
      <span v-if="streaming" class="chat-streaming-indicator">··</span>
    </div>
    <div class="chat-message-content">
      <span v-if="!rawHtml" v-text="content"></span>
      <span v-else v-html="rendered"></span>
      <img v-if="imageUrl" :src="imageUrl" alt="上传的图片" />
    </div>
  </div>
</template>

<script>
// 极简 markdown 渲染：保留原有 AIChat 内联实现，避免引入新依赖。
function renderMarkdown(text) {
  if (!text && text !== "") return "";
  return String(text)
    .replace(/\*\*(.*?)\*\*/g, "<strong>$1</strong>")
    .replace(/\*(.*?)\*/g, "<em>$1</em>")
    .replace(/`(.*?)`/g, "<code>$1</code>")
    .replace(/\n/g, "<br>");
}

export default {
  name: "MessageBubble",
  props: {
    role: {type: String, required: true},
    content: {type: String, default: ""},
    // 是否以 v-html 渲染 markdown
    rawHtml: {type: Boolean, default: false},
    // 流式状态标记
    streaming: {type: Boolean, default: false},
    // 是否展示 TTS 按钮
    showTts: {type: Boolean, default: false},
    // 用户上传的图片预览地址（图片识别用）
    imageUrl: {type: String, default: ""},
  },
  emits: ["tts"],
  computed: {
    isUser() {
      return this.role === "user";
    },
    rendered() {
      return renderMarkdown(this.content);
    },
  },
};
</script>
