package main

import "context"

type App struct {
	ctx context.Context
}

type DashboardMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Hint  string `json:"hint"`
}

type DashboardItem struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type DashboardData struct {
	Metrics []DashboardMetric `json:"metrics"`
	Alerts  []DashboardItem   `json:"alerts"`
	Recent  []DashboardItem   `json:"recent"`
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	startCenterServer(ctx)
}

func (a *App) LoadCenterSettings() (centerSettingsFile, error) {
	return readCenterSettings()
}

func (a *App) SaveCenterSettings(settings centerSettingsFile) error {
	return writeCenterSettings(settings)
}

func (a *App) GetCenterStatus() (CenterStatus, error) {
	return centerStatusSnapshot()
}

func (a *App) GetDashboardData() DashboardData {
	return DashboardData{
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
			{Title: "最近新增能力包", Description: "新增“周报汇总”和“异常归档”两个能力包。", Status: "新增"},
			{Title: "最近规则下发", Description: "安全规则已下发到 18 个客户端。", Status: "成功"},
		},
	}
}
