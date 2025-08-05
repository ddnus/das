import axios from 'axios'

// 创建axios实例
const api = axios.create({
  baseURL: '/api',
  timeout: 10000
})

// 请求拦截器
api.interceptors.request.use(
  config => {
    return config
  },
  error => {
    return Promise.reject(error)
  }
)

// 响应拦截器
api.interceptors.response.use(
  response => {
    return response
  },
  error => {
    return Promise.reject(error)
  }
)

export default {
  // 用户相关
  login(username) {
    return api.post('/login', { username })
  },
  
  register(data) {
    return api.post('/register', data)
  },
  
  getCurrentUser() {
    return api.get('/user')
  },
  
  updateUser(data) {
    return api.put('/user', data)
  },
  
  queryUser(username) {
    return api.get(`/user/${username}`)
  },
  
  // 节点相关
  getNodes() {
    return api.get('/nodes')
  },
  
  connectNode(address) {
    return api.post('/nodes/connect', { address })
  },
  
  // 系统状态
  getStatus() {
    return api.get('/status')
  }
}