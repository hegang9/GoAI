import { nextTick } from 'vue'

// 容器自动滚动到底部：消息列表追加内容后调用。
// 通过 nextTick 确保 DOM 更新完成后再滚动。
export function useAutoScroll() {
  const scrollToBottom = (containerRef) => {
    if (!containerRef || !containerRef.value) return
    try {
      containerRef.value.scrollTop = containerRef.value.scrollHeight
    } catch (e) {
      // 容器尚未挂载或被卸载，忽略
    }
  }

  // 等待 DOM 更新后滚动，常用于追加消息后
  const scrollAfterUpdate = async (containerRef) => {
    await nextTick()
    scrollToBottom(containerRef)
  }

  return { scrollToBottom, scrollAfterUpdate }
}
