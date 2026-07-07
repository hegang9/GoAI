import { ref, computed } from 'vue'
import api from '../utils/api'

// 会话管理：加载会话列表、切换会话（懒加载历史）、创建临时会话、同步历史。
// 仅负责会话维度的状态与请求，不处理消息发送。
export function useChatSession() {
  // sessions 以 id -> {id, name, messages} 的字典形式存储，便于 O(1) 查找
  const sessions = ref({})
  const currentSessionId = ref(null)
  // tempSession 标记当前是否处于"新会话草稿"状态（尚未持久化）
  const tempSession = ref(false)
  const currentMessages = ref([])

  const sessionList = computed(() => Object.values(sessions.value))

  // 拉取会话列表（不含消息，消息按需懒加载）
  const loadSessions = async () => {
    try {
      const response = await api.get('/ai/chat/sessions')
      if (
        response.data &&
        response.data.status_code === 1000 &&
        Array.isArray(response.data.sessions)
      ) {
        const sessionMap = {}
        response.data.sessions.forEach((s) => {
          const sid = String(s.sessionId)
          sessionMap[sid] = {
            id: sid,
            name: s.name || `会话 ${sid}`,
            messages: []
          }
        })
        sessions.value = sessionMap
      }
    } catch (error) {
      console.error('Load sessions error:', error)
    }
  }

  // 创建临时会话：首次发送消息时由后端持久化并返回真实 sessionId
  const createNewSession = () => {
    currentSessionId.value = 'temp'
    tempSession.value = true
    currentMessages.value = []
  }

  // 切换会话；首次切换时懒加载历史消息
  const switchSession = async (sessionId) => {
    if (!sessionId) return
    const sid = String(sessionId)
    currentSessionId.value = sid
    tempSession.value = false

    const target = sessions.value[sid]
    if (!target) return

    if (!target.messages || target.messages.length === 0) {
      try {
        const response = await api.post('/ai/chat/history', { sessionId: sid })
        if (
          response.data &&
          response.data.status_code === 1000 &&
          Array.isArray(response.data.history)
        ) {
          target.messages = response.data.history.map((item) => ({
            role: item.is_user ? 'user' : 'assistant',
            content: item.content
          }))
        }
      } catch (err) {
        console.error('Load history error:', err)
      }
    }
    currentMessages.value = [...(target.messages || [])]
  }

  // 手动同步当前会话的历史消息
  const syncHistory = async () => {
    if (!currentSessionId.value || tempSession.value) return false
    try {
      const response = await api.post('/ai/chat/history', {
        sessionId: currentSessionId.value
      })
      if (
        response.data &&
        response.data.status_code === 1000 &&
        Array.isArray(response.data.history)
      ) {
        const messages = response.data.history.map((item) => ({
          role: item.is_user ? 'user' : 'assistant',
          content: item.content
        }))
        sessions.value[currentSessionId.value].messages = messages
        currentMessages.value = [...messages]
        return true
      }
      return false
    } catch (err) {
      console.error('Sync history error:', err)
      return false
    }
  }

  // 新会话首次持久化后，把临时会话升级为正式会话
  const promoteTempSession = (sessionId, name, messages) => {
    const sid = String(sessionId)
    sessions.value[sid] = {
      id: sid,
      name: name || '新会话',
      messages: [...messages]
    }
    currentSessionId.value = sid
    tempSession.value = false
  }

  return {
    sessions,
    currentSessionId,
    tempSession,
    currentMessages,
    sessionList,
    loadSessions,
    createNewSession,
    switchSession,
    syncHistory,
    promoteTempSession
  }
}
