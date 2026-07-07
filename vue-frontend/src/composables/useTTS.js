import { ElMessage } from 'element-plus'
import api from '../utils/api'

// 百度 TTS 语音合成：创建任务 + 轮询结果 + 播放音频。
// 轮询策略与原 AIChat 保持一致：先等 5s 再开始，最长 30 次、每次 2s。
export function useTTS() {
  const playTTS = async (text) => {
    try {
      const createResponse = await api.post('/ai/tts', { text })
      if (
        createResponse.data &&
        createResponse.data.status_code === 1000 &&
        createResponse.data.task_id
      ) {
        const taskId = createResponse.data.task_id
        await new Promise((resolve) => setTimeout(resolve, 5000))

        const maxAttempts = 30
        const pollInterval = 2000
        let attempts = 0

        // 递归轮询，命中终态后返回
        const pollResult = async () => {
          const queryResponse = await api.get('/ai/tts/query', {
            params: { task_id: taskId }
          })

          if (queryResponse.data && queryResponse.data.status_code === 1000) {
            const taskStatus = queryResponse.data.task_status
            if (taskStatus === 'Success' && queryResponse.data.task_result) {
              const audio = new Audio(queryResponse.data.task_result)
              audio.play()
              return true
            }
            if (taskStatus === 'Running' || taskStatus === 'Created') {
              attempts++
              if (attempts < maxAttempts) {
                await new Promise((resolve) => setTimeout(resolve, pollInterval))
                return pollResult()
              }
              ElMessage.error('语音合成超时')
              return true
            }
            ElMessage.error('语音合成失败')
            return true
          }

          attempts++
          if (attempts < maxAttempts) {
            await new Promise((resolve) => setTimeout(resolve, pollInterval))
            return pollResult()
          }
          ElMessage.error('语音合成超时')
          return true
        }

        await pollResult()
      } else {
        ElMessage.error('无法创建语音合成任务')
      }
    } catch (error) {
      console.error('TTS error:', error)
      ElMessage.error('请求语音接口失败')
    }
  }

  return { playTTS }
}
