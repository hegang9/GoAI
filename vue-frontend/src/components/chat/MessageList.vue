<template>
  <!-- 消息列表容器：负责滚动条与自动滚动到底部。
   * 通过 messages 数组渲染 MessageBubble，把渲染细节委托给子组件。 -->
  <div ref="containerRef" class="chat-messages">
    <MessageBubble
      v-for="(message, index) in messages"
      :key="index"
      :role="message.role"
      :content="message.content"
      :raw-html="rawHtml"
      :streaming="message.meta && message.meta.status === 'streaming'"
      :show-tts="showTts"
      :image-url="message.imageUrl"
      @tts="$emit('tts', $event)"
    />
  </div>
</template>

<script>
import {ref, watch, nextTick} from "vue";
import MessageBubble from "./MessageBubble.vue";

export default {
  name: "MessageList",
  components: {MessageBubble},
  props: {
    messages: {type: Array, default: () => []},
    // 是否启用 markdown 渲染（AIChat 启用，图片识别不启用）
    rawHtml: {type: Boolean, default: false},
    // 是否在 AI 消息上展示 TTS 按钮
    showTts: {type: Boolean, default: false},
  },
  emits: ["tts"],
  setup(props) {
    const containerRef = ref(null);

    // 滚动到底部：等待 DOM 更新完成后再读取 scrollHeight
    const scrollToBottom = () => {
      nextTick(() => {
        if (containerRef.value) {
          containerRef.value.scrollTop = containerRef.value.scrollHeight;
        }
      });
    };

    // 消息数量或最后一条内容变化时自动滚动（流式逐字追加触发）
    watch(
      () => [
        props.messages.length,
        props.messages[props.messages.length - 1]?.content,
      ],
      scrollToBottom,
    );

    // 暴露给父组件按需调用（例如发送后立即滚动）
    return {containerRef, scrollToBottom};
  },
};
</script>
