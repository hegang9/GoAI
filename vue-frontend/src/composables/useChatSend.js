import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../utils/api'

// 消息发送：普通对话 + 流式 SSE 解析。
// 把 SSE 协议解析、错误回滚等细节收敛在此，view 只调用 sendMessage。
export function useChatSend({ sessions, currentSessionId, tempSession, currentMessages, selectedModel, selectedDoc }) {
  const loading = ref(false)

  // 构造请求体：临时会话走 -new-session 接口，正式会话带 sessionId
  const buildBody = (question) => {
    const filter = selectedDoc.value ? { storedName: selectedDoc.value } : {}
    return tempSession.value
      ? { question, modelType: selectedModel.value, ...filter }
      : { question, modelType: selectedModel.value, sessionId: currentSessionId.value, ...filter }
  }

  // 普通发送
  const sendNormal = async (question) => {
    if (tempSession.value) {
      const response = await api.post('/ai/chat/send-new-session', buildBody(question))
      if (response.data && response.data.status_code === 1000) {
        const sessionId = String(response.data.sessionId)
        const aiMessage = { role: 'assistant', content: response.data.Information || '' }
        // 把临时会话升级为正式会话，并存入完整消息
        sessions.value[sessionId] = {
          id: sessionId,
          name: '新会话',
          messages: [
            { role: 'user', content: question },
            aiMessage
          ]
        }
        currentSessionId.value = sessionId
        tempSession.value = false
        currentMessages.value = [...sessions.value[sessionId].messages]
      } else {
        ElMessage.error(response.data?.status_msg || '发送失败')
        currentMessages.value.pop()
      }
      return
    }

    // 正式会话：本地先 push 用户消息（调用方已 push 到 currentMessages，这里同步到 sessions）
    const sessionMsgs = sessions.value[currentSessionId.value].messages
    sessionMsgs.push({ role: 'user', content: question })

    const response = await api.post('/ai/chat/send', buildBody(question))
    if (response.data && response.data.status_code === 1000) {
      const aiMessage = { role: 'assistant', content: response.data.Information || '' }
      sessionMsgs.push(aiMessage)
      currentMessages.value = [...sessionMsgs]
    } else {
      ElMessage.error(response.data?.status_msg || '发送失败')
      sessionMsgs.pop()
      currentMessages.value.pop()
    }
  }

  // 流式发送：fetch + ReadableStream 解析 SSE
  const sendStream = async (question, { onChunk, onDone, onSessionId } = {}) => {
    const aiMessage = { role: 'assistant', content: '', meta: { status: 'streaming' } }
    const aiMessageIndex = currentMessages.value.length
    currentMessages.value.push(aiMessage)

    // 同步占位到 sessions（正式会话）
    if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value]) {
      if (!sessions.value[currentSessionId.value].messages) {
        sessions.value[currentSessionId.value].messages = []
      }
      sessions.value[currentSessionId.value].messages.push({ role: 'assistant', content: '' })
    }

    const url = tempSession.value
      ? '/api/ai/chat/send-stream-new-session'
      : '/api/ai/chat/send-stream'
    const headers = {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${localStorage.getItem('token') || ''}`
    }

    try {
      const response = await fetch(url, {
        method: 'POST',
        headers,
        body: JSON.stringify(buildBody(question))
      })

      if (!response.ok) throw new Error('Network response was not ok')

      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      // eslint-disable-next-line no-constant-condition
      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() || ''

        for (const line of lines) {
          const trimmed = line.trim()
          if (!trimmed || !trimmed.startsWith('data:')) continue

          const data = trimmed.slice(5).trim()
          if (data === '[DONE]') {
            currentMessages.value[aiMessageIndex].meta = { status: 'done' }
            currentMessages.value = [...currentMessages.value]
            onDone?.()
            continue
          }

          if (data.startsWith('{')) {
            // 可能是 sessionId 首帧
            try {
              const parsed = JSON.parse(data)
              if (parsed.sessionId) {
                const newSid = String(parsed.sessionId)
                if (tempSession.value) {
                  // 升级临时会话
                  sessions.value[newSid] = {
                    id: newSid,
                    name: '新会话',
                    messages: [...currentMessages.value]
                  }
                  currentSessionId.value = newSid
                  tempSession.value = false
                  onSessionId?.(newSid)
                }
                continue
              }
            } catch (e) {
              // 不是 JSON，当作普通文本处理
            }
            currentMessages.value[aiMessageIndex].content += data
          } else {
            currentMessages.value[aiMessageIndex].content += data
          }

          // 强制响应式更新
          currentMessages.value = [...currentMessages.value]
          onChunk?.(currentMessages.value[aiMessageIndex].content)
        }
      }

      // 流结束兜底
      currentMessages.value[aiMessageIndex].meta = { status: 'done' }
      currentMessages.value = [...currentMessages.value]

      // 同步到 sessions 存储
      if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value]) {
        const sessMsgs = sessions.value[currentSessionId.value].messages
        if (Array.isArray(sessMsgs) && sessMsgs.length) {
          const lastIndex = sessMsgs.length - 1
          if (sessMsgs[lastIndex] && sessMsgs[lastIndex].role === 'assistant') {
            sessMsgs[lastIndex].content = currentMessages.value[aiMessageIndex].content
          }
        }
      }
      onDone?.()
    } catch (err) {
      console.error('Stream error:', err)
      currentMessages.value[aiMessageIndex].meta = { status: 'error' }
      currentMessages.value = [...currentMessages.value]
      ElMessage.error('流式传输出错')
      throw err
    }
  }

  // 统一入口：根据 isStreaming 分派
  const sendMessage = async (question, { isStreaming, onChunk, onDone } = {}) => {
    try {
      loading.value = true
      if (isStreaming) {
        await sendStream(question, { onChunk, onDone })
      } else {
        await sendNormal(question)
      }
    } catch (err) {
      console.error('Send message error:', err)
      ElMessage.error('发送失败，请重试')
      // 用户消息回滚
      currentMessages.value.pop()
      if (
        !tempSession.value &&
        currentSessionId.value &&
        sessions.value[currentSessionId.value]?.messages?.length
      ) {
        sessions.value[currentSessionId.value].messages.pop()
      }
    } finally {
      if (!isStreaming) loading.value = false
    }
  }

  return { loading, sendMessage }
}
