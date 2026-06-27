import axios from 'axios'

const api = axios.create({
  baseURL: '/api', // 使用代理路径，开发环境会自动代理到后端
  timeout: 0  //不启用超时机制
})

// 请求拦截器
api.interceptors.request.use(
  config => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  error => {
    return Promise.reject(error)
  }
)

// 响应拦截器
// 后端统一信封结构为 { status_code, status_msg, data }，业务数据收敛在 data 字段。
// 为兼容各视图仍按平铺方式读取业务字段（如 response.data.token），这里将信封中的
// data 业务字段展开合并到 response.data 顶层，同时保留 status_code / status_msg。
api.interceptors.response.use(
  response => {
    const body = response.data
    // 仅处理 JSON 信封；SSE/二进制等非信封响应保持原样。
    if (body && typeof body === 'object' && 'status_code' in body) {
      const payload = (body.data && typeof body.data === 'object') ? body.data : {}
      response.data = {
        status_code: body.status_code,
        status_msg: body.status_msg,
        // 保留原始 data 字段，便于需要时按新契约读取 response.data.data。
        data: body.data,
        // 展开业务字段到顶层，兼容既有按平铺结构读取的视图代码。
        ...payload
      }
    }
    return response
  },
  error => {
    if (error.response && error.response.status === 401) {
      localStorage.removeItem('token')
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

export default api