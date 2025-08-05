<template>
  <div class="dashboard">
    <h2>系统仪表板</h2>
    
    <el-row :gutter="20" class="stats-row">
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-content">
            <div class="stat-icon">
              <el-icon size="40" color="#409eff"><User /></el-icon>
            </div>
            <div class="stat-info">
              <div class="stat-value">{{ status.user ? '已登录' : '未登录' }}</div>
              <div class="stat-label">用户状态</div>
            </div>
          </div>
        </el-card>
      </el-col>
      
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-content">
            <div class="stat-icon">
              <el-icon size="40" color="#67c23a"><Connection /></el-icon>
            </div>
            <div class="stat-info">
              <div class="stat-value">{{ nodeCount }}</div>
              <div class="stat-label">连接节点</div>
            </div>
          </div>
        </el-card>
      </el-col>
      
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-content">
            <div class="stat-icon">
              <el-icon size="40" color="#e6a23c"><Clock /></el-icon>
            </div>
            <div class="stat-info">
              <div class="stat-value">{{ formatTime(status.timestamp) }}</div>
              <div class="stat-label">系统时间</div>
            </div>
          </div>
        </el-card>
      </el-col>
      
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-content">
            <div class="stat-icon">
              <el-icon size="40" color="#f56c6c"><CircleCheck /></el-icon>
            </div>
            <div class="stat-info">
              <div class="stat-value">{{ status.status || '运行中' }}</div>
              <div class="stat-label">系统状态</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="20" class="content-row">
      <el-col :span="12">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>系统信息</span>
              <el-button @click="refreshStatus" :loading="loading" type="text">
                <el-icon><Refresh /></el-icon>
                刷新
              </el-button>
            </div>
          </template>
          <div class="system-info">
            <div class="info-item">
              <span class="info-label">节点ID:</span>
              <span class="info-value">{{ status.host_id || '未知' }}</span>
            </div>
            <div class="info-item" v-if="status.user">
              <span class="info-label">当前用户:</span>
              <span class="info-value">{{ status.user.username }} ({{ status.user.nickname }})</span>
            </div>
            <div class="info-item" v-if="status.user">
              <span class="info-label">用户版本:</span>
              <span class="info-value">v{{ status.user.version }}</span>
            </div>
            <div class="info-item" v-if="status.user">
              <span class="info-label">存储配额:</span>
              <span class="info-value">{{ status.user.storage_quota }} MB</span>
            </div>
          </div>
        </el-card>
      </el-col>
      
      <el-col :span="12">
        <el-card>
          <template #header>
            <span>快速操作</span>
          </template>
          <div class="quick-actions">
            <el-button @click="$router.push('/profile')" type="primary" v-if="status.user">
              <el-icon><User /></el-icon>
              编辑个人资料
            </el-button>
            <el-button @click="$router.push('/nodes')" type="success">
              <el-icon><Connection /></el-icon>
              查看网络节点
            </el-button>
            <el-button @click="$router.push('/users')" type="info">
              <el-icon><Search /></el-icon>
              查询用户
            </el-button>
            <el-button @click="showConnectDialog = true" type="warning">
              <el-icon><Link /></el-icon>
              连接节点
            </el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 连接节点对话框 -->
    <el-dialog v-model="showConnectDialog" title="连接到节点" width="500px">
      <el-form :model="connectForm" label-width="100px">
        <el-form-item label="节点地址">
          <el-input
            v-model="connectForm.address"
            placeholder="例如: /ip4/127.0.0.1/tcp/4001/p2p/12D3KooW..."
            type="textarea"
            :rows="3"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="showConnectDialog = false">取消</el-button>
          <el-button type="primary" @click="handleConnect" :loading="connectLoading">
            连接
          </el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { User, Connection, Clock, CircleCheck, Refresh, Search, Link } from '@element-plus/icons-vue'
import api from '../api'

export default {
  name: 'Dashboard',
  components: {
    User,
    Connection,
    Clock,
    CircleCheck,
    Refresh,
    Search,
    Link
  },
  setup() {
    const status = ref({})
    const nodeCount = ref(0)
    const loading = ref(false)
    const showConnectDialog = ref(false)
    const connectLoading = ref(false)

    const connectForm = reactive({
      address: ''
    })

    // 获取系统状态
    const getStatus = async () => {
      try {
        const response = await api.getStatus()
        if (response.data.success) {
          status.value = response.data.data
        }
      } catch (error) {
        console.error('获取系统状态失败:', error)
      }
    }

    // 获取节点数量
    const getNodeCount = async () => {
      try {
        const response = await api.getNodes()
        if (response.data.success) {
          nodeCount.value = response.data.data.length
        }
      } catch (error) {
        nodeCount.value = 0
      }
    }

    // 刷新状态
    const refreshStatus = async () => {
      loading.value = true
      try {
        await Promise.all([getStatus(), getNodeCount()])
        ElMessage.success('状态已刷新')
      } catch (error) {
        ElMessage.error('刷新失败')
      } finally {
        loading.value = false
      }
    }

    // 连接节点
    const handleConnect = async () => {
      if (!connectForm.address.trim()) {
        ElMessage.error('请输入节点地址')
        return
      }

      connectLoading.value = true
      try {
        const response = await api.connectNode(connectForm.address)
        if (response.data.success) {
          ElMessage.success('连接成功')
          showConnectDialog.value = false
          connectForm.address = ''
          await getNodeCount()
        } else {
          ElMessage.error(response.data.message || '连接失败')
        }
      } catch (error) {
        ElMessage.error('连接失败: ' + (error.response?.data?.message || error.message))
      } finally {
        connectLoading.value = false
      }
    }

    // 格式化时间
    const formatTime = (timestamp) => {
      if (!timestamp) return '未知'
      return new Date(timestamp * 1000).toLocaleString()
    }

    onMounted(() => {
      getStatus()
      getNodeCount()
    })

    return {
      status,
      nodeCount,
      loading,
      showConnectDialog,
      connectLoading,
      connectForm,
      refreshStatus,
      handleConnect,
      formatTime
    }
  }
}
</script>

<style scoped>
.dashboard {
  padding: 0;
}

.stats-row {
  margin-bottom: 30px;
}

.stat-card {
  height: 140px;
  background: linear-gradient(135deg, #fff 0%, #f8fafc 100%);
  border: 1px solid rgba(102, 126, 234, 0.1);
  transition: all 0.3s ease;
  cursor: pointer;
}

.stat-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 8px 30px rgba(0, 0, 0, 0.12);
  border-color: rgba(102, 126, 234, 0.3);
}

.stat-content {
  display: flex;
  align-items: center;
  height: 100%;
  padding: 20px;
}

.stat-icon {
  margin-right: 20px;
  padding: 15px;
  border-radius: 12px;
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.1) 0%, rgba(118, 75, 162, 0.1) 100%);
}

.stat-info {
  flex: 1;
}

.stat-value {
  font-size: 28px;
  font-weight: 700;
  margin-bottom: 8px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.stat-label {
  color: #64748b;
  font-size: 14px;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.content-row {
  margin-top: 30px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
  color: #1e293b;
}

.system-info {
  padding: 20px 0;
}

.info-item {
  display: flex;
  margin-bottom: 16px;
  padding: 12px 16px;
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  border-radius: 8px;
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
  font-weight: 600;
  flex-shrink: 0;
}

.info-value {
  flex: 1;
  font-weight: 500;
  color: #1e293b;
  word-break: break-all;
}

.quick-actions {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.quick-actions .el-button {
  justify-content: flex-start;
  height: 48px;
  border-radius: 10px;
  font-weight: 500;
  transition: all 0.3s ease;
  border: 1px solid #e2e8f0;
  background: linear-gradient(135deg, #fff 0%, #f8fafc 100%);
}

.quick-actions .el-button:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.1);
}

.quick-actions .el-button--primary {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  border: none;
  color: white;
}

.quick-actions .el-button--success {
  background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
  border: none;
  color: white;
}

.quick-actions .el-button--info {
  background: linear-gradient(135deg, #4299e1 0%, #3182ce 100%);
  border: none;
  color: white;
}

.quick-actions .el-button--warning {
  background: linear-gradient(135deg, #ed8936 0%, #dd6b20 100%);
  border: none;
  color: white;
}

/* 连接对话框样式 */
:deep(.el-dialog__body) {
  padding: 30px;
}

:deep(.el-form-item__label) {
  font-weight: 600;
  color: #374151;
}

:deep(.el-textarea__inner) {
  border-radius: 8px;
  border: 2px solid #e5e7eb;
  transition: all 0.3s ease;
}

:deep(.el-textarea__inner:focus) {
  border-color: #667eea;
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
}
</style>
