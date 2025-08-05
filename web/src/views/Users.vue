<template>
  <div class="users">
    <div class="page-header">
      <h2>用户查询</h2>
    </div>

    <el-card>
      <template #header>
        <span>搜索用户</span>
      </template>
      
      <el-form :model="searchForm" @submit.prevent="handleSearch" inline>
        <el-form-item label="用户名">
          <el-input
            v-model="searchForm.username"
            placeholder="请输入要查询的用户名"
            style="width: 300px;"
            @keyup.enter="handleSearch"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch" :loading="searching">
            <el-icon><Search /></el-icon>
            查询
          </el-button>
          <el-button @click="clearSearch">
            <el-icon><Refresh /></el-icon>
            清空
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 搜索结果 -->
    <el-card v-if="searchResult" style="margin-top: 20px;">
      <template #header>
        <div class="card-header">
          <span>查询结果</span>
          <el-tag type="success">找到用户</el-tag>
        </div>
      </template>
      
      <div class="user-profile">
        <div class="user-avatar">
          <el-avatar :size="80" :src="searchResult.avatar">
            {{ searchResult.nickname?.charAt(0) || searchResult.username?.charAt(0) }}
          </el-avatar>
        </div>
        
        <div class="user-info">
          <h3 class="user-name">{{ searchResult.nickname || searchResult.username }}</h3>
          <p class="user-username">@{{ searchResult.username }}</p>
          <p class="user-bio">{{ searchResult.bio || '这个用户很懒，什么都没有留下...' }}</p>
          
          <div class="user-stats">
            <div class="stat-item">
              <span class="stat-label">版本:</span>
              <span class="stat-value">v{{ searchResult.version }}</span>
            </div>
            <div class="stat-item">
              <span class="stat-label">存储配额:</span>
              <span class="stat-value">{{ searchResult.storage_quota }} MB</span>
            </div>
            <div class="stat-item">
              <span class="stat-label">创建时间:</span>
              <span class="stat-value">{{ formatTime(searchResult.created_at) }}</span>
            </div>
            <div class="stat-item">
              <span class="stat-label">更新时间:</span>
              <span class="stat-value">{{ formatTime(searchResult.updated_at) }}</span>
            </div>
          </div>
        </div>
      </div>
    </el-card>

    <!-- 搜索历史 -->
    <el-card v-if="searchHistory.length > 0" style="margin-top: 20px;">
      <template #header>
        <div class="card-header">
          <span>搜索历史</span>
          <el-button @click="clearHistory" type="text" size="small">
            <el-icon><Delete /></el-icon>
            清空历史
          </el-button>
        </div>
      </template>
      
      <div class="search-history">
        <el-tag
          v-for="(item, index) in searchHistory"
          :key="index"
          class="history-tag"
          @click="searchFromHistory(item)"
          closable
          @close="removeFromHistory(index)"
        >
          {{ item }}
        </el-tag>
      </div>
    </el-card>

    <!-- 使用说明 -->
    <el-card style="margin-top: 20px;">
      <template #header>
        <span>使用说明</span>
      </template>
      
      <div class="help-content">
        <el-alert
          title="如何查询用户"
          type="info"
          :closable="false"
          show-icon
        >
          <p>1. 在搜索框中输入要查询的用户名</p>
          <p>2. 点击"查询"按钮或按回车键进行搜索</p>
          <p>3. 系统将从网络中查询该用户的最新信息</p>
          <p>4. 查询结果将显示用户的基本信息和统计数据</p>
        </el-alert>
        
        <div class="tips" style="margin-top: 15px;">
          <h4>小贴士:</h4>
          <ul>
            <li>用户名区分大小写，请确保输入正确</li>
            <li>查询需要连接到网络节点，请确保已连接</li>
            <li>系统会自动保存搜索历史，方便快速查询</li>
            <li>点击历史记录可以快速重新搜索</li>
          </ul>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, Refresh, Delete } from '@element-plus/icons-vue'
import api from '../api'

export default {
  name: 'Users',
  components: {
    Search,
    Refresh,
    Delete
  },
  setup() {
    const searchResult = ref(null)
    const searching = ref(false)
    const searchHistory = ref([])

    const searchForm = reactive({
      username: ''
    })

    // 从localStorage加载搜索历史
    const loadSearchHistory = () => {
      try {
        const history = localStorage.getItem('das_search_history')
        if (history) {
          searchHistory.value = JSON.parse(history)
        }
      } catch (error) {
        console.error('加载搜索历史失败:', error)
      }
    }

    // 保存搜索历史到localStorage
    const saveSearchHistory = () => {
      try {
        localStorage.setItem('das_search_history', JSON.stringify(searchHistory.value))
      } catch (error) {
        console.error('保存搜索历史失败:', error)
      }
    }

    // 添加到搜索历史
    const addToHistory = (username) => {
      // 移除重复项
      const index = searchHistory.value.indexOf(username)
      if (index > -1) {
        searchHistory.value.splice(index, 1)
      }
      
      // 添加到开头
      searchHistory.value.unshift(username)
      
      // 限制历史记录数量
      if (searchHistory.value.length > 10) {
        searchHistory.value = searchHistory.value.slice(0, 10)
      }
      
      saveSearchHistory()
    }

    // 从历史记录中移除
    const removeFromHistory = (index) => {
      searchHistory.value.splice(index, 1)
      saveSearchHistory()
    }

    // 清空搜索历史
    const clearHistory = () => {
      searchHistory.value = []
      saveSearchHistory()
      ElMessage.success('搜索历史已清空')
    }

    // 从历史记录搜索
    const searchFromHistory = (username) => {
      searchForm.username = username
      handleSearch()
    }

    // 处理搜索
    const handleSearch = async () => {
      const username = searchForm.username.trim()
      if (!username) {
        ElMessage.error('请输入用户名')
        return
      }

      searching.value = true
      searchResult.value = null

      try {
        const response = await api.queryUser(username)
        if (response.data.success) {
          searchResult.value = response.data.data
          addToHistory(username)
          ElMessage.success('查询成功')
        } else {
          ElMessage.error(response.data.message || '查询失败')
        }
      } catch (error) {
        if (error.response?.status === 404) {
          ElMessage.error('用户不存在')
        } else {
          ElMessage.error('查询失败: ' + (error.response?.data?.message || error.message))
        }
      } finally {
        searching.value = false
      }
    }

    // 清空搜索
    const clearSearch = () => {
      searchForm.username = ''
      searchResult.value = null
    }

    // 格式化时间
    const formatTime = (timeStr) => {
      if (!timeStr) return '未知'
      return new Date(timeStr).toLocaleString()
    }

    onMounted(() => {
      loadSearchHistory()
    })

    return {
      searchResult,
      searching,
      searchHistory,
      searchForm,
      handleSearch,
      clearSearch,
      searchFromHistory,
      removeFromHistory,
      clearHistory,
      formatTime
    }
  }
}
</script>

<style scoped>
.users {
  padding: 0;
}

.page-header {
  margin-bottom: 30px;
  padding: 20px 0;
  border-bottom: 2px solid #e2e8f0;
}

.page-header h2 {
  margin: 0;
  font-size: 28px;
  font-weight: 700;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
  color: #1e293b;
}

.user-profile {
  display: flex;
  gap: 30px;
  padding: 30px;
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  border-radius: 16px;
  position: relative;
  overflow: hidden;
}

.user-profile::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 80px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  z-index: 0;
}

.user-profile > * {
  position: relative;
  z-index: 1;
}

.user-avatar {
  flex-shrink: 0;
  margin-top: 20px;
}

.user-info {
  flex: 1;
  margin-top: 20px;
}

.user-name {
  margin: 0 0 8px 0;
  font-size: 32px;
  font-weight: 700;
  color: #1e293b;
  text-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
}

.user-username {
  margin: 0 0 16px 0;
  color: #64748b;
  font-size: 18px;
  font-weight: 500;
}

.user-bio {
  margin: 0 0 24px 0;
  color: #475569;
  line-height: 1.6;
  font-size: 16px;
  padding: 16px 20px;
  background: rgba(255, 255, 255, 0.8);
  border-radius: 12px;
  border-left: 4px solid #667eea;
}

.user-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 16px;
}

.stat-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  background: rgba(255, 255, 255, 0.9);
  border-radius: 12px;
  border-left: 4px solid #667eea;
  transition: all 0.3s ease;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.05);
}

.stat-item:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.1);
}

.stat-label {
  color: #64748b;
  font-size: 14px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.stat-value {
  font-weight: 600;
  color: #1e293b;
  font-size: 16px;
}

.search-history {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  padding: 20px 0;
}

.history-tag {
  cursor: pointer;
  transition: all 0.3s ease;
  border-radius: 20px;
  padding: 8px 16px;
  font-weight: 500;
  background: linear-gradient(135deg, #f1f5f9 0%, #e2e8f0 100%);
  border: 1px solid #cbd5e1;
  color: #475569;
}

.history-tag:hover {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(102, 126, 234, 0.3);
}

.help-content {
  line-height: 1.7;
  color: #475569;
}

.help-content p {
  margin: 8px 0;
  font-size: 15px;
}

.tips {
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  padding: 24px;
  border-radius: 12px;
  border-left: 4px solid #667eea;
  margin-top: 20px;
}

.tips h4 {
  margin: 0 0 16px 0;
  color: #1e293b;
  font-size: 18px;
  font-weight: 600;
}

.tips ul {
  margin: 0;
  padding-left: 24px;
}

.tips li {
  margin: 8px 0;
  color: #64748b;
  font-size: 15px;
  line-height: 1.6;
}

/* 搜索表单样式 */
:deep(.el-form--inline .el-form-item) {
  margin-right: 20px;
}

:deep(.el-input__wrapper) {
  border-radius: 10px;
  border: 2px solid #e5e7eb;
  transition: all 0.3s ease;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.05);
}

:deep(.el-input__wrapper:hover) {
  border-color: #d1d5db;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
}

:deep(.el-input__wrapper.is-focus) {
  border-color: #667eea;
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
}

:deep(.el-button) {
  border-radius: 10px;
  padding: 10px 20px;
  font-weight: 500;
  transition: all 0.3s ease;
}

:deep(.el-button:hover) {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.1);
}

:deep(.el-button--primary) {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  border: none;
}

:deep(.el-alert) {
  border-radius: 12px;
  border: none;
  background: linear-gradient(135deg, #e0f2fe 0%, #b3e5fc 100%);
  border-left: 4px solid #0288d1;
}

:deep(.el-alert__icon) {
  color: #0288d1;
}

:deep(.el-alert__title) {
  color: #01579b;
  font-weight: 600;
}

:deep(.el-alert__description) {
  color: #0277bd;
}

:deep(.el-avatar) {
  border: 4px solid rgba(255, 255, 255, 0.8);
  box-shadow: 0 8px 25px rgba(0, 0, 0, 0.15);
}
</style>
