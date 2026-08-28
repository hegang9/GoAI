<template>
  <ChatLayout>
    <!-- 左侧会话列表 -->
    <template #sidebar>
      <SessionSidebar
        :sessions="sessionList"
        :active-id="currentSessionId"
        @new="onNewSession"
        @select="onSwitchSession"
      />
    </template>

    <!-- 右侧主区 -->
    <template #main>
      <ChatTopBar @back="$router.push('/menu')">
        <button
          class="chat-btn chat-btn-success"
          :disabled="!currentSessionId || tempSession"
          @click="onSyncHistory"
        >
          同步历史数据
        </button>
        <label for="streamingMode" class="chat-inline-label">
          <input type="checkbox" id="streamingMode" v-model="isStreaming" />
          流式响应
        </label>
        <label for="docFilter">检索范围：</label>
        <select
          id="docFilter"
          v-model="selectedDoc"
          class="chat-select"
          :disabled="ragFiles.length === 0"
        >
          <option value="">全知识库</option>
          <option v-for="name in ragFiles" :key="name" :value="name">
            {{ name }}
          </option>
        </select>
        <button
          class="chat-btn chat-btn-danger"
          :disabled="uploading"
          @click="triggerFileUpload"
        >
          📎 上传文档(.md/.txt/.pdf/.docx)
        </button>
        <input
          ref="fileInput"
          type="file"
          accept=".md,.txt,.pdf,.docx,text/markdown,text/plain,application/pdf,application/vnd.openxmlformats-officedocument.wordprocessingml.document"
          style="display: none"
          @change="handleFileUpload"
        />
      </ChatTopBar>

      <MessageList
        ref="messageListRef"
        :messages="currentMessages"
        :raw-html="true"
        :show-tts="true"
        @tts="playTTS"
      />

      <ChatInput ref="chatInputRef" :disabled="loading" @send="onSend" />
    </template>
  </ChatLayout>
</template>

<script>
import {ref, onMounted, nextTick} from "vue";
import {ElMessage} from "element-plus";
import ChatLayout from "../layouts/ChatLayout.vue";
import SessionSidebar from "../components/chat/SessionSidebar.vue";
import ChatTopBar from "../components/chat/ChatTopBar.vue";
import MessageList from "../components/chat/MessageList.vue";
import ChatInput from "../components/chat/ChatInput.vue";
import {useChatSession} from "../composables/useChatSession";
import {useChatSend} from "../composables/useChatSend";
import {useRagFiles} from "../composables/useRagFiles";
import {useTTS} from "../composables/useTTS";

export default {
  name: "AIChat",
  components: {ChatLayout, SessionSidebar, ChatTopBar, MessageList, ChatInput},
  setup() {
    // 流式 / 检索范围属于本页面专属状态，留在 view 内
    const isStreaming = ref(false);
    const selectedDoc = ref("");
    const fileInput = ref(null);
    const messageListRef = ref(null);
    const chatInputRef = ref(null);

    // 装配各 composable
    const session = useChatSession();
    const {ragFiles, uploading, loadRagFiles, uploadFile} = useRagFiles();
    const {playTTS} = useTTS();
    const {loading, sendMessage} = useChatSend({
      sessions: session.sessions,
      currentSessionId: session.currentSessionId,
      tempSession: session.tempSession,
      currentMessages: session.currentMessages,
      selectedDoc,
    });

    // 新建临时会话并聚焦输入框
    const onNewSession = () => {
      session.createNewSession();
      nextTick(() => chatInputRef.value?.focus());
    };

    const onSwitchSession = (sessionId) => {
      session.switchSession(sessionId).then(() => {
        nextTick(() => messageListRef.value?.scrollToBottom());
      });
    };

    const onSyncHistory = async () => {
      if (!session.currentSessionId.value || session.tempSession.value) {
        ElMessage.warning("请选择已有会话进行同步");
        return;
      }
      const ok = await session.syncHistory();
      if (!ok) ElMessage.error("无法获取历史数据");
      nextTick(() => messageListRef.value?.scrollToBottom());
    };

    // 发送消息：先 push 用户消息到当前视图，再交给 composable 处理
    const onSend = async (question) => {
      session.currentMessages.value.push({role: "user", content: question});
      await nextTick();
      messageListRef.value?.scrollToBottom();

      await sendMessage(question, {
        isStreaming: isStreaming.value,
        onChunk: () => messageListRef.value?.scrollToBottom(),
      });

      // 普通模式 loading 在 composable 内已置 false；流式模式在 [DONE] 后手动复位
      if (isStreaming.value) loading.value = false;
      await nextTick();
      messageListRef.value?.scrollToBottom();
    };

    const triggerFileUpload = () => {
      fileInput.value?.click();
    };

    const handleFileUpload = async (event) => {
      const file = event.target.files[0];
      if (!file) return;
      await uploadFile(file);
      if (fileInput.value) fileInput.value.value = "";
    };

    onMounted(() => {
      session.loadSessions();
      loadRagFiles();
    });

    return {
      // session
      sessionList: session.sessionList,
      currentSessionId: session.currentSessionId,
      tempSession: session.tempSession,
      currentMessages: session.currentMessages,
      // send
      loading,
      // rag
      ragFiles,
      uploading,
      // tts
      playTTS,
      // 本页状态
      isStreaming,
      selectedDoc,
      fileInput,
      messageListRef,
      chatInputRef,
      // 事件
      onNewSession,
      onSwitchSession,
      onSyncHistory,
      onSend,
      triggerFileUpload,
      handleFileUpload,
    };
  },
};
</script>
