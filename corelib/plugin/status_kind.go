package plugin

type PluginStatus string

const (
	PluginStatusRegistered PluginStatus = "registered"
	PluginStatusRunning    PluginStatus = "running"
	PluginStatusStopped    PluginStatus = "stopped"
	PluginStatusError      PluginStatus = "error"
)

func (s PluginStatus) String() string {
	return string(s)
}

func (s PluginStatus) IsRunning() bool {
	return s == PluginStatusRunning
}

func (s PluginStatus) IsStopped() bool {
	return s == PluginStatusStopped
}

type PluginHealthStatus string

const (
	PluginHealthHealthy   PluginHealthStatus = "healthy"
	PluginHealthDegraded  PluginHealthStatus = "degraded"
	PluginHealthUnhealthy PluginHealthStatus = "unhealthy"
)

func (s PluginHealthStatus) String() string {
	return string(s)
}
