package main

import (
	"encoding/json"
	"net/http"
)

// DashboardMetric represents a single metric on the overview page.
type DashboardMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Hint  string `json:"hint"`
}

// DashboardItem represents an alert or recent activity item.
type DashboardItem struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

// DashboardData is the full dashboard payload.
type DashboardData struct {
	Metrics []DashboardMetric `json:"metrics"`
	Alerts  []DashboardItem   `json:"alerts"`
	Recent  []DashboardItem   `json:"recent"`
}

// handleDashboardAPI serves GET /api/dashboard.
func handleDashboardAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data := DashboardData{
		Metrics: []DashboardMetric{
			{Label: "数字员工总数", Value: "28", Hint: "其中 21 个处于启用状态"},
			{Label: "今日协作次数", Value: "146", Hint: "覆盖办公、生产、质量三类事务"},
			{Label: "当前生效规则数", Value: "32", Hint: "包含安全、下发与模型路由规则"},
		},
		Alerts: []DashboardItem{
			{Title: "风险能力包待审核", Description: "2 个能力包等待安全审核。", Status: "待处理"},
			{Title: "模型不可用告警", Description: "一个备用模型链路检查失败。", Status: "需关注"},
		},
		Recent: []DashboardItem{
			{Title: "最近活跃数字员工", Description: "小迪、阿宁、老陈在最近 1 小时内有任务处理记录。", Status: "活跃"},
			{Title: "最近新增能力包", Description: "新增「周报汇总」和「异常归档」两个能力包。", Status: "新增"},
			{Title: "最近规则下发", Description: "安全规则已下发到 18 个客户端。", Status: "成功"},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

// handleCenterSettingsAPI serves GET/PUT /api/center/settings.
func handleCenterSettingsAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := readCenterSettings()
		if err != nil {
			writeCenterError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(settings)

	case http.MethodPut:
		var settings centerSettingsFile
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			writeCenterError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := writeCenterSettings(settings); err != nil {
			writeCenterError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
