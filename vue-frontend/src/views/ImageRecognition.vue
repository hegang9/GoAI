<template>
  <ChatLayout>
    <!-- 左侧：图像识别为单会话，复用 SessionSidebar 但隐藏新建按钮 -->
    <template #sidebar>
      <SessionSidebar title="图像识别" :show-new-button="false" :sessions="[]">
        <template #list>
          <li class="chat-sidebar-item active">图像识别助手</li>
        </template>
      </SessionSidebar>
    </template>

    <!-- 右侧主区 -->
    <template #main>
      <ChatTopBar title="AI 图像识别助手" @back="$router.push('/menu')" />

      <MessageList :messages="messages" :show-tts="false" />

      <div class="image-input-area">
        <form @submit.prevent="handleSubmit">
          <input
            ref="fileInputRef"
            type="file"
            accept="image/*"
            required
            @change="handleFileSelect"
          />
          <button
            type="submit"
            class="chat-btn chat-btn-brand"
            :disabled="!selectedFile"
          >
            发送图片
          </button>
        </form>
      </div>
    </template>
  </ChatLayout>
</template>

<script>
import {ref, nextTick} from "vue";
import {ElMessage} from "element-plus";
import ChatLayout from "../layouts/ChatLayout.vue";
import SessionSidebar from "../components/chat/SessionSidebar.vue";
import ChatTopBar from "../components/chat/ChatTopBar.vue";
import MessageList from "../components/chat/MessageList.vue";
import api from "../utils/api";

export default {
  name: "ImageRecognition",
  components: {ChatLayout, SessionSidebar, ChatTopBar, MessageList},
  setup() {
    const messages = ref([]);
    const selectedFile = ref(null);
    const fileInputRef = ref();
    const messageListRef = ref();

    const handleFileSelect = (event) => {
      selectedFile.value = event.target.files[0];
    };

    const handleSubmit = async () => {
      if (!selectedFile.value) return;
      const file = selectedFile.value;
      const imageUrl = URL.createObjectURL(file);

      // 先把用户消息（含图片预览）追加到视图
      messages.value.push({
        role: "user",
        content: `已上传图片: ${file.name}`,
        imageUrl,
      });

      await nextTick();
      messageListRef.value?.scrollToBottom();

      const formData = new FormData();
      formData.append("image", file);

      try {
        const response = await api.post("/image/recognize", formData, {
          headers: {"Content-Type": "multipart/form-data"},
        });

        if (response.data && response.data.class_name) {
          messages.value.push({
            role: "assistant",
            content: `识别结果: ${response.data.class_name}`,
          });
        } else {
          messages.value.push({
            role: "assistant",
            content: `[错误] ${response.data.status_msg || "识别失败"}`,
          });
        }
      } catch (error) {
        console.error("Upload error:", error);
        messages.value.push({
          role: "assistant",
          content: `[错误] 无法连接到服务器或上传失败: ${error.message}`,
        });
        ElMessage.error("图片识别失败");
      } finally {
        URL.revokeObjectURL(imageUrl);
        await nextTick();
        messageListRef.value?.scrollToBottom();
        selectedFile.value = null;
        if (fileInputRef.value) fileInputRef.value.value = "";
      }
    };

    return {
      messages,
      selectedFile,
      fileInputRef,
      messageListRef,
      handleFileSelect,
      handleSubmit,
    };
  },
};
</script>

<style scoped>
.image-input-area {
  padding: var(--space-lg);
  background: var(--surface-glass-strong);
  backdrop-filter: blur(8px);
  border-top: 1px solid var(--surface-border);
  position: relative;
  z-index: 1;
}

.image-input-area form {
  display: flex;
  gap: 20px;
}

.image-input-area input[type="file"] {
  flex: 1;
  border: 2px dashed #d9d9d9;
  border-radius: var(--radius-md);
  padding: 15px 20px;
  background: rgba(255, 255, 255, 0.8);
  color: #666;
  cursor: pointer;
  transition: all 0.3s var(--ease-out);
  font-size: 14px;
}

.image-input-area input[type="file"]:hover {
  border-color: #409eff;
  background: rgba(64, 158, 255, 0.05);
}

.image-input-area input[type="file"]::file-selector-button {
  border: none;
  background: var(--gradient-brand);
  padding: 8px 16px;
  border-radius: var(--radius-sm);
  color: white;
  cursor: pointer;
  font-weight: 600;
  margin-right: 12px;
  transition: all 0.3s var(--ease-out);
  box-shadow: 0 2px 10px rgba(102, 126, 234, 0.3);
}

.image-input-area input[type="file"]::file-selector-button:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 15px rgba(102, 126, 234, 0.4);
}

.image-input-area button {
  padding: 15px 30px;
  border-radius: var(--radius-md);
  font-size: 16px;
  font-weight: 600;
}
</style>
