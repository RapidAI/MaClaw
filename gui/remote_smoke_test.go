package main

import "testing"

func TestPrintRemoteSmokeReportDoesNotPanic(t *testing.T) {
	printRemoteSmokeReport(RemoteSmokeReport{
		Tool:        "claude",
		ProjectPath: "D:\\workprj\\demo",
		UseProxy:    false,
		Connection: RemoteConnectionStatus{
			Enabled:   true,
			HubURL:    "http://127.0.0.1:9399",
			MachineID: "m_123",
			Connected: true,
		},
		Readiness: RemoteToolReadiness{
			Ready:           true,
			ToolInstalled:   true,
			ModelConfigured: true,
			ToolPath:        "D:\\Users\\demo\\.maclaw\\data\\tools\\claude.exe",
			CommandPath:     "D:\\Users\\demo\\.maclaw\\data\\tools\\claude.exe",
			PTYSupported:    true,
			PTYMessage:      "ConPTY is available",
			SelectedModel:   "anthropic",
			SelectedModelID: "claude-sonnet",
		},
	})
}
