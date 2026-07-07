<template>
  <!-- 会话列表侧栏：标题 + 新建按钮 + 会话列表。
   * 通过 props 接收数据，通过 emit 上报交互，纯展示组件。 -->
  <aside class="chat-sidebar">
    <div class="chat-sidebar-header">
      <span>{{ title }}</span>
      <button
        v-if="showNewButton"
        class="chat-btn chat-btn-brand new-chat-btn"
        @click="$emit('new')"
      >
        ＋ 新聊天
      </button>
      <slot name="header-extra" />
    </div>
    <ul class="chat-sidebar-list">
      <slot name="list">
        <li
          v-for="session in sessions"
          :key="session.id"
          :class="['chat-sidebar-item', {active: activeId === session.id}]"
          @click="$emit('select', session.id)"
        >
          {{ session.name || `会话 ${session.id}` }}
        </li>
      </slot>
    </ul>
  </aside>
</template>

<script>
export default {
  name: "SessionSidebar",
  props: {
    title: {type: String, default: "会话列表"},
    sessions: {type: Array, default: () => []},
    activeId: {type: [String, Number], default: null},
    showNewButton: {type: Boolean, default: true},
  },
  emits: ["new", "select"],
};
</script>

<style scoped>
.new-chat-btn {
  width: 100%;
  padding: 12px 0;
  font-size: 14px;
}
</style>
