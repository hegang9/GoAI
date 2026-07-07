import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../utils/api'

// RAG 知识库文档管理：拉取已上传文档列表 + 上传新文档。
// 只允许 .md / .txt（与原 AIChat 内联逻辑保持一致）。
export function useRagFiles() {
  const ragFiles = ref([])
  const uploading = ref(false)

  const loadRagFiles = async () => {
    try {
      const response = await api.get('/file/list')
      if (response.data && response.data.status_code === 1000) {
        ragFiles.value = response.data.files || []
      }
    } catch (error) {
      console.error('Load rag files error:', error)
    }
  }

  const uploadFile = async (file) => {
    if (!file) return false
    const fileName = file.name.toLowerCase()
    if (!fileName.endsWith('.md') && !fileName.endsWith('.txt')) {
      ElMessage.error('只允许上传 .md 或 .txt 文件')
      return false
    }

    try {
      uploading.value = true
      const formData = new FormData()
      formData.append('file', file)

      const response = await api.post('/file/upload', formData, {
        headers: { 'Content-Type': 'multipart/form-data' }
      })

      if (response.data && response.data.status_code === 1000) {
        ElMessage.success('文件上传成功')
        await loadRagFiles()
        return true
      }
      ElMessage.error(response.data?.status_msg || '上传失败')
      return false
    } catch (error) {
      console.error('File upload error:', error)
      ElMessage.error('文件上传失败')
      return false
    } finally {
      uploading.value = false
    }
  }

  return { ragFiles, uploading, loadRagFiles, uploadFile }
}
