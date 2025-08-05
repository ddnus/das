<template>
  <div class="profile">
    <h2>个人资料</h2>
    
    <el-card v-if="!currentUser" class="no-user-card">
      <div class="no-user">
        <el-icon size="60" color="#ccc"><User /></el-icon>
        <p>请先登录以查看个人资料</p>
        <el-button type="primary" @click="$emit('show-login')">立即登录</el-button>
      </div>
    </el-card>

    <div v-else>
      <el-row :gutter="20">
        <el-col :span="8">
          <el-card>
            <template #header>
              <span>头像信息</span>
            </template>
            <div class="avatar-section">
              <el-avatar :size="120" :src="currentUser.avatar">
                {{ currentUser.nickname?.charAt(0) || currentUser.username?.charAt(0) }}
              </el-avatar>
              <div class="avatar-info">
                <h3>{{ currentUser.nickname || currentUser.username }}</h3>
                <p class="username">@{{ currentUser.username }}</p>
                <p class="version">版本 v{{ currentUser.version }}</p>
              </div>
            </div>
          </el-card>
        </el-col>
        
        <el-col :span="16">
          <el-card>
            <template #header>
              <div class="card-header">
                <span>基本信息</span>
                <el-button @click="editMode = !editMode" type="primary" :icon="editMode ? 'Close' : 'Edit'">
                  {{ editMode ? '取消编辑' : '编辑资料' }}
                </el-button>
              </div>
            </template>
            
            <el-form :model="userForm" label-width="100px" v-if="editMode">
              <el-form-item label="昵称">
                <el-input v-model="userForm.nickname" placeholder="请输入昵称" />
              </el-form-item>
              <el-form-item label="个人简介">
                <el-input
                  v-model="userForm.bio"
                  type="textarea"
                  placeholder="请输入个人简介"
                  :rows="4"
                />
              </el-form-item>
              <el-form-item label="头像URL">
                <el-input v-model="userForm.avatar" placeholder="请输入头像URL" />
              </el-form-item>
              <el-form-item>
                <el-button type="primary" @click="handleUpdate" :loading="updateLoading">
                  保存修改
                </el-button>
                <el-button @click="resetForm">重置</el-button>
              </el-form-item>
            </el-form>
            
            <div v-else class="user-info">
              <div class="info-item">
                <span class="info-label">用户名:</span>
                <span class="info-value">{{ currentUser.username }}</span>
              </div>
              <div class="info-item">
                <span class="info-label">昵称:</span>
                <span class="info-value">{{ currentUser.nickname || '未设置' }}</span>
              </div>
              <div class="info-item">
                <span class="info-label">个人简介:</span>
                <span class="info-value">{{ currentUser.bio || '未设置' }}</span>
              </div>
              <div class="info-item">
                <span class="info-label">头像:</span>
                <span class="info-value">{{ currentUser.avatar || '未设置' }}</span>
              </div>
              <div class="info-item">
                <span class="info-label">存储配额:</span>
                <span class="info-value">{{ currentUser.storage_quota }} MB</span>
              </div>
              <div class="info-item">
                <span class="info-label">创建时间:</span>
                <span class="info-value">{{ formatTime(currentUser.created_at) }}</span>
              </div>
              <div class="info-item">
                <span class="info-label">更新时间:</span>
                <span class="info-value">{{ formatTime(currentUser.updated_at) }}</span>
              </div>
            </div>
          </el-card>
        </el-col>
      </el-row>

      <el-row :gutter="20" style="margin-top: 20px;">
        <el-col :span="24">
          <el-card>
            <template #header>
              <span>存储空间</span>
            </template>
            <div class="storage-info">
              <el-progress
                :percentage="storageUsagePercent"
                :color="storageUsagePercent > 80 ? '#f56c6c' : '#409eff'"
              />
              <p class="storage-text">
                已使用: {{ storageUsed }} MB / {{ currentUser.storage_quota }} MB
              </p>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </div>
  </div>
</template>

<script>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { User, Edit, Close } from '@element-plus/icons-vue'
import api from '../api'

export default {
  name: 'Profile',
  components: {
    User,
    Edit,
    Close
  },
  setup() {
    const currentUser = ref(null)
    const editMode = ref(false)
    const updateLoading = ref(false)

    const userForm = reactive({
      nickname: '',
      bio: '',
      avatar: ''
    })

    // 计算存储使用情况
    const storageUsed = computed(() => {
      if (!currentUser.value?.storage_space) return 0
      const totalBytes = Object.values(currentUser.value.storage_space)
        .reduce((total, data) => total + (data?.length || 0), 0)
      return Math.round(totalBytes / 1024 / 1024 * 100) / 100 // 转换为MB并保留2位小数
    })

    const storageUsagePercent = computed(() => {
      if (!currentUser.value?.storage_quota) return 0
      return Math.round((storageUsed.value / currentUser.value.storage_quota) * 100)
    })

    // 获取当前用户信息
    const getCurrentUser = async () => {
      try {
        const response = await api.getCurrentUser()
        if (response.data.success) {
          currentUser.value = response.data.data
          resetForm()
        }
      } catch (error) {
        console.log('未登录或获取用户信息失败')
      }
    }

    // 重置表单
    const resetForm = () => {
      if (currentUser.value) {
        userForm.nickname = currentUser.value.nickname || ''
        userForm.bio = currentUser.value.bio || ''
        userForm.avatar = currentUser.value.avatar || ''
      }
    }

    // 处理更新
    const handleUpdate = async () => {
      updateLoading.value = true
      try {
        const response = await api.updateUser(userForm)
        if (response.data.success) {
          ElMessage.success('更新成功')
          editMode.value = false
          await getCurrentUser() // 重新获取用户信息
        } else {
          ElMessage.error(response.data.message || '更新失败')
        }
      } catch (error) {
        ElMessage.error('更新失败: ' + (error.response?.data?.message || error.message))
      } finally {
        updateLoading.value = false
      }
    }

    // 格式化时间
    const formatTime = (timeStr) => {
      if (!timeStr) return '未知'
      return new Date(timeStr).toLocaleString()
    }

    // 监听编辑模式变化，重置表单
    watch(editMode, (newVal) => {
      if (!newVal) {
        resetForm()
      }
    })

    onMounted(() => {
      getCurrentUser()
    })

    return {
      currentUser,
      editMode,
      updateLoading,
      userForm,
      storageUsed,
      storageUsagePercent,
      handleUpdate,
      resetForm,
      formatTime
    }
  }
}
</script>

<style scoped>
.profile {
  padding: 0;
}

.no-user-card {
  text-align: center;
  padding: 60px 40px;
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  border-radius: 16px;
}

.no-user {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 24px;
}

.avatar-section {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 20px;
  padding: 30px;
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  border-radius: 16px;
  position: relative;
  overflow: hidden;
}

.avatar-section::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 60px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  z-index: 0;
}

.avatar-section > * {
  position: relative;
  z-index: 1;
}

.avatar-info {
  text-align: center;
}

.avatar-info h3 {
  margin: 0 0 8px 0;
  font-size: 24px;
  font-weight: 700;
  color: #1e293b;
}

.username {
  color: #64748b;
  margin: 0 0 8px 0;
  font-size: 16px;
  font-weight: 500;
}

.version {
  color: #94a3b8;
  font-size: 14px;
  margin: 0;
  padding: 4px 12px;
  background: rgba(148, 163, 184, 0.1);
  border-radius: 20px;
  display: inline-block;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
  color: #1e293b;
}

.user-info {
  padding: 20px 0;
}

.info-item {
  display: flex;
  margin-bottom: 20px;
  align-items: flex-start;
  padding: 16px 20px;
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  border-radius: 12px;
  border-left: 4px solid #667eea;
  transition: all 0.3s ease;
}

.info-item:hover {
  transform: translateX(4px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05);
}

.info-label {
  width: 120px;
  color: #475569;
  flex-shrink: 0;
  font-weight: 600;
}

.info-value {
  flex: 1;
  font-weight: 500;
  word-break: break-all;
  color: #1e293b;
}

.storage-info {
  padding: 30px;
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  border-radius: 12px;
  text-align: center;
}

.storage-text {
  margin-top: 16px;
  color: #64748b;
  font-size: 16px;
  font-weight: 500;
}

/* 表单样式优化 */
:deep(.el-form-item__label) {
  font-weight: 600;
  color: #374151;
}

:deep(.el-input__wrapper) {
  border-radius: 10px;
  border: 2px solid #e5e7eb;
  transition: all 0.3s ease;
}

:deep(.el-input__wrapper:hover) {
  border-color: #d1d5db;
}

:deep(.el-input__wrapper.is-focus) {
  border-color: #667eea;
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
}

:deep(.el-textarea__inner) {
  border-radius: 10px;
  border: 2px solid #e5e7eb;
  transition: all 0.3s ease;
}

:deep(.el-textarea__inner:focus) {
  border-color: #667eea;
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
}

:deep(.el-progress-bar__outer) {
  border-radius: 10px;
  background-color: #e2e8f0;
}

:deep(.el-progress-bar__inner) {
  border-radius: 10px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

:deep(.el-avatar) {
  border: 4px solid rgba(255, 255, 255, 0.8);
  box-shadow: 0 8px 25px rgba(0, 0, 0, 0.15);
}
</style>
