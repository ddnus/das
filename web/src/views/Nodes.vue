<template>
  <div class="nodes">
    <div class="page-header">
      <h2>网络节点</h2>
      <div class="header-actions">
        <el-button @click="refreshNodes" :loading="loading" type="primary">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
        <el-button @click="showConnectDialog = true" type="success">
          <el-icon><Plus /></el-icon>
          连接节点
        </el-button>
      </div>
    </div>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>当前连接的节点 ({{ nodes.length }})</span>
          <el-tag :type="nodes.length > 0 ? 'success' : 'warning'">
            {{ nodes.length > 0 ? '已连接' : '未连接' }}
          </el-tag>
        </div>
      </template>

      <div v-if="loading" class="loading-container">
        <el-skeleton :rows="5" animated />
      </div>

      <div v-else-if="nodes.length === 0" class="empty-container">
        <el-empty description="暂无连接的节点">
          <el-button @click="showConnectDialog = true" type="primary">连接节点</el-button>
        </el-empty>
      </div>

      <div v-else>
        <el-table :data="nodes" stripe style="width: 100%">
          <el-table-column prop="id" label="节点ID" width="200">
            <template #default="scope">
              <el-tooltip :content="scope.row.id" placement="top">
                <span class="node-id">{{ scope.row.id.substring(0, 20) }}...</span>
              </el-tooltip>
            </template>
          </el-table-column>
          
          <el-table-column prop="type" label="节点类型" width="100">
            <template #default="scope">
              <el-tag :type="getNodeTypeColor(scope.row.type)" size="small">
                {{ scope.row.type || '未知' }}
              </el-tag>
            </template>
          </el-table-column>
          
          <el-table-column prop="connected" label="连接状态" width="100">
            <template #default="scope">
              <el-tag :type="scope.row.connected === 'Connected' ? 'success' : 'danger'" size="small">
                {{ scope.row.connected }}
              </el-tag>
            </template>
          </el-table-column>
          
          <el-table-column prop="reputation" label="信誉值" width="100">
            <template #default="scope">
              <span v-if="scope.row.reputation">{{ scope.row.reputation }}</span>
              <span v-else class="text-muted">-</span>
            </template>
          </el-table-column>
          
          <el-table-column prop="online_time" label="在线时间" width="120">
            <template #default="scope">
              <span v-if="scope.row.online_time">{{ formatOnlineTime(scope.row.online_time) }}</span>
              <span v-else class="text-muted">-</span>
            </template>
          </el-table-column>
          
          <el-table-column prop="storage" label="存储空间" width="100">
            <template #default="scope">
              <span v-if="scope.row.storage">{{ scope.row.storage }} MB</span>
              <span v-else class="text-muted">-</span>
            </template>
          </el-table-column>
          
          <el-table-column prop="connected_at" label="连接时间" width="160">
            <template #default="scope">
              {{ formatTime(scope.row.connected_at) }}
            </template>
          </el-table-column>
          
          <el-table-column label="操作" width="120">
            <template #default="scope">
              <el-button @click="showNodeDetail(scope.row)" type="text" size="small">
                <el-icon><View /></el-icon>
                详情
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </el-card>

    <!-- 连接节点对话框 -->
    <el-dialog v-model="showConnectDialog" title="连接到节点" width="600px">
      <el-form :model="connectForm" label-width="100px">
        <el-form-item label="节点地址">
          <el-input
            v-model="connectForm.address"
            placeholder="例如: /ip4/127.0.0.1/tcp/4001/p2p/12D3KooW..."
            type="textarea"
            :rows="3"
          />
          <div class="form-tip">
            请输入完整的多地址格式，包含IP、端口和节点ID
          </div>
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

    <!-- 节点详情对话框 -->
    <el-dialog v-model="showDetailDialog" title="节点详情" width="700px">
      <div v-if="selectedNode" class="node-detail">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="节点ID">
            <el-input :value="selectedNode.id" readonly type="textarea" :rows="2" />
          </el-descriptions-item>
          <el-descriptions-item label="节点类型">
            <el-tag :type="getNodeTypeColor(selectedNode.type)">
              {{ selectedNode.type || '未知' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="连接状态">
            <el-tag :type="selectedNode.connected === 'Connected' ? 'success' : 'danger'">
              {{ selectedNode.connected }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="信誉值">
            {{ selectedNode.reputation || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="在线时间">
            {{ selectedNode.online_time ? formatOnlineTime(selectedNode.online_time) : '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="存储空间">
            {{ selectedNode.storage ? selectedNode.storage + ' MB' : '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="计算资源">
            {{ selectedNode.compute ? selectedNode.compute + ' 单位' : '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="网络资源">
            {{ selectedNode.network ? selectedNode.network + ' 单位' : '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="连接时间" :span="2">
            {{ formatTime(selectedNode.connected_at) }}
          </el-descriptions-item>
        </el-descriptions>
        
        <div v-if="selectedNode.addresses && selectedNode.addresses.length > 0" class="addresses-section">
          <h4>节点地址</h4>
          <el-tag v-for="addr in selectedNode.addresses" :key="addr" class="address-tag">
            {{ addr }}
          </el-tag>
        </div>
        
        <div v-if="selectedNode.protocols && selectedNode.protocols.length > 0" class="protocols-section">
          <h4>支持协议</h4>
          <el-tag v-for="protocol in selectedNode.protocols" :key="protocol" type="info" class="protocol-tag">
            {{ protocol }}
          </el-tag>
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<script>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Plus, View } from '@element-plus/icons-vue'
import api from '../api'

export default {
  name: 'Nodes',
  components: {
    Refresh,
    Plus,
    View
  },
  setup() {
    const nodes = ref([])
    const loading = ref(false)
    const showConnectDialog = ref(false)
    const showDetailDialog = ref(false)
    const connectLoading = ref(false)
    const selectedNode = ref(null)

    const connectForm = reactive({
      address: ''
    })

    // 获取节点列表
    const getNodes = async () => {
      loading.value = true
      try {
        const response = await api.getNodes()
        if (response.data.success) {
          nodes.value = response.data.data || []
        } else {
          nodes.value = []
          ElMessage.warning(response.data.message || '获取节点列表失败')
        }
      } catch (error) {
        nodes.value = []
        console.error('获取节点列表失败:', error)
      } finally {
        loading.value = false
      }
    }

    // 刷新节点列表
    const refreshNodes = async () => {
      await getNodes()
      ElMessage.success('节点列表已刷新')
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
          await getNodes() // 刷新节点列表
        } else {
          ElMessage.error(response.data.message || '连接失败')
        }
      } catch (error) {
        ElMessage.error('连接失败: ' + (error.response?.data?.message || error.message))
      } finally {
        connectLoading.value = false
      }
    }

    // 显示节点详情
    const showNodeDetail = (node) => {
      selectedNode.value = node
      showDetailDialog.value = true
    }

    // 获取节点类型颜色
    const getNodeTypeColor = (type) => {
      switch (type) {
        case '全节点':
          return 'success'
        case '半节点':
          return 'warning'
        default:
          return 'info'
      }
    }

    // 格式化在线时间
    const formatOnlineTime = (seconds) => {
      if (!seconds) return '-'
      const hours = Math.floor(seconds / 3600)
      const minutes = Math.floor((seconds % 3600) / 60)
      if (hours > 0) {
        return `${hours}小时${minutes}分钟`
      }
      return `${minutes}分钟`
    }

    // 格式化时间
    const formatTime = (timeStr) => {
      if (!timeStr) return '-'
      return new Date(timeStr).toLocaleString()
    }

    onMounted(() => {
      getNodes()
    })

    return {
      nodes,
      loading,
      showConnectDialog,
      showDetailDialog,
      connectLoading,
      selectedNode,
      connectForm,
      getNodes,
      refreshNodes,
      handleConnect,
      showNodeDetail,
      getNodeTypeColor,
      formatOnlineTime,
      formatTime
    }
  }
}
</script>

<style scoped>
.nodes {
  padding: 0;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
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

.header-actions {
  display: flex;
  gap: 12px;
}

.header-actions .el-button {
  border-radius: 10px;
  padding: 10px 20px;
  font-weight: 500;
  transition: all 0.3s ease;
}

.header-actions .el-button:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.1);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
  color: #1e293b;
}

.loading-container {
  padding: 40px;
}

.empty-container {
  padding: 60px 40px;
  text-align: center;
}

.node-id {
  font-family: 'SF Mono', 'Monaco', 'Inconsolata', 'Roboto Mono', monospace;
  font-size: 13px;
  background: linear-gradient(135deg, #f1f5f9 0%, #e2e8f0 100%);
  padding: 4px 8px;
  border-radius: 6px;
  color: #475569;
  font-weight: 500;
}

.text-muted {
  color: #94a3b8;
  font-style: italic;
}

.form-tip {
  font-size: 13px;
  color: #64748b;
  margin-top: 8px;
  padding: 8px 12px;
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  border-radius: 6px;
  border-left: 3px solid #667eea;
}

.node-detail {
  padding: 20px 0;
}

.addresses-section,
.protocols-section {
  margin-top: 24px;
  padding: 20px;
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  border-radius: 12px;
  border-left: 4px solid #667eea;
}

.addresses-section h4,
.protocols-section h4 {
  margin: 0 0 16px 0;
  color: #1e293b;
  font-size: 16px;
  font-weight: 600;
}

.address-tag,
.protocol-tag {
  margin: 6px 8px 6px 0;
  font-family: 'SF Mono', 'Monaco', 'Inconsolata', 'Roboto Mono', monospace;
  font-size: 12px;
  border-radius: 8px;
  padding: 6px 12px;
  transition: all 0.3s ease;
}

.address-tag {
  max-width: 350px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: inline-block;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  cursor: pointer;
}

.address-tag:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(102, 126, 234, 0.3);
}

.protocol-tag {
  background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
  color: white;
}

/* 表格样式优化 */
:deep(.el-table) {
  border-radius: 12px;
  overflow: hidden;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.08);
}

:deep(.el-table__header) {
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
}

:deep(.el-table th) {
  background: transparent;
  color: #374151;
  font-weight: 600;
  border-bottom: 2px solid #e2e8f0;
}

:deep(.el-table td) {
  border-bottom: 1px solid #f1f5f9;
  padding: 16px 12px;
}

:deep(.el-table__row:hover) {
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
}

:deep(.el-tag) {
  border-radius: 6px;
  font-weight: 500;
  border: none;
}

:deep(.el-tag--success) {
  background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
  color: white;
}

:deep(.el-tag--danger) {
  background: linear-gradient(135deg, #f56565 0%, #e53e3e 100%);
  color: white;
}

:deep(.el-tag--warning) {
  background: linear-gradient(135deg, #ed8936 0%, #dd6b20 100%);
  color: white;
}

:deep(.el-tag--info) {
  background: linear-gradient(135deg, #4299e1 0%, #3182ce 100%);
  color: white;
}

/* 空状态样式 */
:deep(.el-empty) {
  padding: 60px 40px;
}

:deep(.el-empty__image) {
  width: 120px;
  height: 120px;
}

:deep(.el-empty__description) {
  color: #64748b;
  font-size: 16px;
  margin-top: 16px;
}

/* 骨架屏样式 */
:deep(.el-skeleton__item) {
  background: linear-gradient(90deg, #f1f5f9 25%, #e2e8f0 50%, #f1f5f9 75%);
  border-radius: 8px;
}

/* 描述列表样式 */
:deep(.el-descriptions) {
  background: white;
  border-radius: 12px;
  overflow: hidden;
}

:deep(.el-descriptions__header) {
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  padding: 16px 20px;
  border-bottom: 1px solid #e2e8f0;
}

:deep(.el-descriptions__body) {
  padding: 0;
}

:deep(.el-descriptions-item__cell) {
  padding: 16px 20px;
  border-bottom: 1px solid #f1f5f9;
}

:deep(.el-descriptions-item__label) {
  font-weight: 600;
  color: #374151;
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
}

:deep(.el-descriptions-item__content) {
  color: #1e293b;
}
</style>
