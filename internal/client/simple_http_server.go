package client

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// SimpleHTTPServer 简单的HTTP API服务器
type SimpleHTTPServer struct {
	service *ClientService
	server  *http.Server
}

// NewSimpleHTTPServer 创建新的简单HTTP服务器
func NewSimpleHTTPServer(service *ClientService, port int) *SimpleHTTPServer {
	mux := http.NewServeMux()

	httpServer := &SimpleHTTPServer{
		service: service,
		server: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: corsMiddleware(mux),
		},
	}

	// 注册路由
	httpServer.setupRoutes(mux)

	return httpServer
}

// corsMiddleware CORS中间件
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// setupRoutes 设置路由
func (h *SimpleHTTPServer) setupRoutes(mux *http.ServeMux) {
	// 静态文件服务
	mux.Handle("/", http.FileServer(http.Dir("./web/dist/")))

	// API路由
	mux.HandleFunc("/api/register", h.handleRegister)
	mux.HandleFunc("/api/login", h.handleLogin)
	mux.HandleFunc("/api/user", h.handleUser)
	mux.HandleFunc("/api/user/", h.handleUserQuery)
	mux.HandleFunc("/api/nodes", h.handleNodes)
	mux.HandleFunc("/api/nodes/connect", h.handleConnectNode)
	mux.HandleFunc("/api/status", h.handleGetStatus)
}

// Start 启动HTTP服务器
func (h *SimpleHTTPServer) Start() error {
	log.Printf("HTTP服务器启动在端口: %s", h.server.Addr)
	return h.server.ListenAndServe()
}

// Stop 停止HTTP服务器
func (h *SimpleHTTPServer) Stop() error {
	return h.server.Close()
}

// API响应结构
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// 注册请求结构
type RegisterRequest struct {
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Bio      string `json:"bio"`
}

// 登录请求结构
type LoginRequest struct {
	Username string `json:"username"`
}

// 更新用户请求结构
type UpdateUserRequest struct {
	Nickname string `json:"nickname"`
	Bio      string `json:"bio"`
	Avatar   string `json:"avatar"`
}

// 连接节点请求结构
type ConnectNodeRequest struct {
	Address string `json:"address"`
}

// handleRegister 处理注册请求
func (h *SimpleHTTPServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		h.sendError(w, http.StatusMethodNotAllowed, "方法不允许")
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "无效的请求格式")
		return
	}

	if err := h.service.RegisterAccount(req.Username, req.Nickname, req.Bio); err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.sendSuccess(w, nil, "注册成功")
}

// handleLogin 处理登录请求
func (h *SimpleHTTPServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		h.sendError(w, http.StatusMethodNotAllowed, "方法不允许")
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "无效的请求格式")
		return
	}

	account, err := h.service.Login(req.Username)
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, err.Error())
		return
	}

	h.sendSuccess(w, account, "登录成功")
}

// handleUser 处理用户相关请求
func (h *SimpleHTTPServer) handleUser(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		h.handleGetCurrentUser(w, r)
	case "PUT":
		h.handleUpdateUser(w, r)
	default:
		h.sendError(w, http.StatusMethodNotAllowed, "方法不允许")
	}
}

// handleGetCurrentUser 处理获取当前用户请求
func (h *SimpleHTTPServer) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	user := h.service.GetCurrentUser()
	if user == nil {
		h.sendError(w, http.StatusUnauthorized, "未登录")
		return
	}

	h.sendSuccess(w, user, "")
}

// handleUpdateUser 处理更新用户请求
func (h *SimpleHTTPServer) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "无效的请求格式")
		return
	}

	if err := h.service.UpdateAccount(req.Nickname, req.Bio, req.Avatar); err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.sendSuccess(w, nil, "更新成功")
}

// handleUserQuery 处理查询用户请求
func (h *SimpleHTTPServer) handleUserQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		h.sendError(w, http.StatusMethodNotAllowed, "方法不允许")
		return
	}

	// 从URL路径中提取用户名
	path := strings.TrimPrefix(r.URL.Path, "/api/user/")
	username := strings.Split(path, "/")[0]

	if username == "" {
		h.sendError(w, http.StatusBadRequest, "用户名不能为空")
		return
	}

	account, err := h.service.QueryAccount(username)
	if err != nil {
		h.sendError(w, http.StatusNotFound, err.Error())
		return
	}

	h.sendSuccess(w, account, "")
}

// handleNodes 处理节点相关请求
func (h *SimpleHTTPServer) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		h.sendError(w, http.StatusMethodNotAllowed, "方法不允许")
		return
	}

	nodes, err := h.service.GetAllNodes()
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.sendSuccess(w, nodes, "")
}

// handleConnectNode 处理连接节点请求
func (h *SimpleHTTPServer) handleConnectNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		h.sendError(w, http.StatusMethodNotAllowed, "方法不允许")
		return
	}

	var req ConnectNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "无效的请求格式")
		return
	}

	if err := h.service.ConnectToPeer(req.Address); err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.sendSuccess(w, nil, "连接成功")
}

// handleGetStatus 处理获取系统状态请求
func (h *SimpleHTTPServer) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		h.sendError(w, http.StatusMethodNotAllowed, "方法不允许")
		return
	}

	status := map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"status":    "running",
		"user":      h.service.GetCurrentUser(),
		"host_id":   h.service.GetHostID(),
	}

	h.sendSuccess(w, status, "")
}

// sendSuccess 发送成功响应
func (h *SimpleHTTPServer) sendSuccess(w http.ResponseWriter, data interface{}, message string) {
	w.Header().Set("Content-Type", "application/json")
	response := APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	}
	json.NewEncoder(w).Encode(response)
}

// sendError 发送错误响应
func (h *SimpleHTTPServer) sendError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	response := APIResponse{
		Success: false,
		Message: message,
	}
	json.NewEncoder(w).Encode(response)
}
