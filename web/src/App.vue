<template>
  <div id="app">
    <el-container class="layout-container">
      <el-header class="header">
        <div class="header-content">
          <h1 class="title">DAS 去中心化账号系统</h1>
          <div class="user-info" v-if="currentUser">
            <el-avatar :size="32" :src="currentUser.avatar">
              {{ currentUser.nickname?.charAt(0) || currentUser.username?.charAt(0) }}
            </el-avatar>
            <span class="username">{{ currentUser.nickname || currentUser.username }}</span>
            <el-button @click="logout" type="text">退出</el-button>
          </div>
          <div v-else>
            <el-button @click="showLogin = true" type="primary">登录</el-button>
          </div>
        </div>
      </el-header>
      
      <el-container>
        <el-aside width="200px" class="sidebar">
          <el-menu
            :default-active="$route.path"
            router
            class="sidebar-menu"
          >
            <el-menu-item index="/dashboard">
              <el-icon><House /></el-icon>
              <span>仪表板</span>
            </el-menu-item>
            <el-menu-item index="/profile" v-if="currentUser">
              <el-icon><User /></el-icon>
              <span>个人资料</span>
            </el-menu-item>
            <el-menu-item index="/nodes">
              <el-icon><Connection /></el-icon>
              <span>网络节点</span>
            </el-menu-item>
            <el-menu-item index="/users">
              <el-icon><UserFilled /></el-icon>
              <span>用户查询</span>
            </el-menu-item>
          </el-menu>
        </el-aside>
        
        <el-main class="main-content">
          <router-view />
        </el-main>
      </el-container>
    </el-container>

    <!-- 登录对话框 -->
    <el-dialog v-model="showLogin" title="用户登录" width="400px">
      <el-form :model="loginForm" label-width="80px">
        <el-form-item label="用户名">
          <el-input v-model="loginForm.username" placeholder="请输入用户名" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="showLogin = false">取消</el-button>
          <el-button type="primary" @click="handleLogin" :loading="loginLoading">
            登录
          </el-button>
          <el-button @click="showRegister = true; showLogin = false">注册新账号</el-button>
        </span>
      </template>
    </el-dialog>

    <!-- 注册对话框 -->
    <el-dialog v-model="showRegister" title="注册账号" width="400px">
      <el-form :model="registerForm" label-width="80px">
        <el-form-item label="用户名">
          <el-input v-model="registerForm.username" placeholder="请输入用户名" />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="registerForm.nickname" placeholder="请输入昵称" />
        </el-form-item>
        <el-form-item label="个人简介">
          <el-input
            v-model="registerForm.bio"
            type="textarea"
            placeholder="请输入个人简介"
            :rows="3"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="showRegister = false">取消</el-button>
          <el-button type="primary" @click="handleRegister" :loading="registerLoading">
            注册
          </el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script>
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { House, User, Connection, UserFilled } from '@element-plus/icons-vue'
import api from './api'

export default {
  name: 'App',
  components: {
    House,
    User,
    Connection,
    UserFilled
  },
  setup() {
    const router = useRouter()
    const currentUser = ref(null)
    const showLogin = ref(false)
    const showRegister = ref(false)
    const loginLoading = ref(false)
    const registerLoading = ref(false)

    const loginForm = reactive({
      username: ''
    })

    const registerForm = reactive({
      username: '',
      nickname: '',
      bio: ''
    })

    // 获取当前用户信息
    const getCurrentUser = async () => {
      try {
        const response = await api.getCurrentUser()
        if (response.data.success) {
          currentUser.value = response.data.data
        }
      } catch (error) {
        console.log('未登录')
      }
    }

    // 处理登录
    const handleLogin = async () => {
      if (!loginForm.username) {
        ElMessage.error('请输入用户名')
        return
      }

      loginLoading.value = true
      try {
        const response = await api.login(loginForm.username)
        if (response.data.success) {
          currentUser.value = response.data.data
          showLogin.value = false
          loginForm.username = ''
          ElMessage.success('登录成功')
          router.push('/dashboard')
        } else {
          ElMessage.error(response.data.message || '登录失败')
        }
      } catch (error) {
        ElMessage.error('登录失败: ' + (error.response?.data?.message || error.message))
      } finally {
        loginLoading.value = false
      }
    }

    // 处理注册
    const handleRegister = async () => {
      if (!registerForm.username) {
        ElMessage.error('请输入用户名')
        return
      }

      registerLoading.value = true
      try {
        const response = await api.register(registerForm)
        if (response.data.success) {
          showRegister.value = false
          Object.assign(registerForm, { username: '', nickname: '', bio: '' })
          ElMessage.success('注册成功，请登录')
          showLogin.value = true
        } else {
          ElMessage.error(response.data.message || '注册失败')
        }
      } catch (error) {
        ElMessage.error('注册失败: ' + (error.response?.data?.message || error.message))
      } finally {
        registerLoading.value = false
      }
    }

    // 退出登录
    const logout = () => {
      currentUser.value = null
      ElMessage.success('已退出登录')
      router.push('/dashboard')
    }

    onMounted(() => {
      getCurrentUser()
    })

    return {
      currentUser,
      showLogin,
      showRegister,
      loginLoading,
      registerLoading,
      loginForm,
      registerForm,
      handleLogin,
      handleRegister,
      logout
    }
  }
}
</script>

<style scoped>
.layout-container {
  height: 100vh;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.header {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  display: flex;
  align-items: center;
  padding: 0 30px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.1);
  backdrop-filter: blur(10px);
}

.header-content {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}

.title {
  margin: 0;
  font-size: 24px;
  font-weight: 600;
  background: linear-gradient(45deg, #fff, #e3f2fd);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  text-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
}

.user-info {
  display: flex;
  align-items: center;
  gap: 15px;
  background: rgba(255, 255, 255, 0.1);
  padding: 8px 16px;
  border-radius: 25px;
  backdrop-filter: blur(10px);
  border: 1px solid rgba(255, 255, 255, 0.2);
}

.username {
  color: white;
  font-weight: 500;
}

.sidebar {
  background: linear-gradient(180deg, #f8fafc 0%, #e2e8f0 100%);
  border-right: 1px solid #e2e8f0;
  box-shadow: 2px 0 8px rgba(0, 0, 0, 0.05);
}

.sidebar-menu {
  border-right: none;
  background: transparent;
}

.sidebar-menu .el-menu-item {
  margin: 4px 8px;
  border-radius: 8px;
  transition: all 0.3s ease;
}

.sidebar-menu .el-menu-item:hover {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  transform: translateX(4px);
}

.sidebar-menu .el-menu-item.is-active {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  box-shadow: 0 4px 12px rgba(102, 126, 234, 0.3);
}

.main-content {
  padding: 30px;
  background: linear-gradient(135deg, #f5f7fa 0%, #c3cfe2 100%);
  min-height: calc(100vh - 60px);
  overflow-y: auto;
}

.dialog-footer {
  display: flex;
  gap: 10px;
  justify-content: flex-end;
}

/* 全局样式优化 */
:deep(.el-card) {
  border-radius: 12px;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.08);
  border: none;
  backdrop-filter: blur(10px);
  background: rgba(255, 255, 255, 0.9);
}

:deep(.el-card__header) {
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  border-bottom: 1px solid #e2e8f0;
  border-radius: 12px 12px 0 0;
  padding: 20px;
}

:deep(.el-button--primary) {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  border: none;
  border-radius: 8px;
  padding: 10px 20px;
  font-weight: 500;
  transition: all 0.3s ease;
}

:deep(.el-button--primary:hover) {
  transform: translateY(-2px);
  box-shadow: 0 8px 25px rgba(102, 126, 234, 0.3);
}

:deep(.el-dialog) {
  border-radius: 16px;
  overflow: hidden;
  backdrop-filter: blur(10px);
  background: rgba(255, 255, 255, 0.95);
}

:deep(.el-dialog__header) {
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  padding: 20px 24px;
  border-bottom: 1px solid #e2e8f0;
}

:deep(.el-input__wrapper) {
  border-radius: 8px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.05);
  transition: all 0.3s ease;
}

:deep(.el-input__wrapper:hover) {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
}

:deep(.el-avatar) {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  border: 2px solid rgba(255, 255, 255, 0.8);
}
</style>
