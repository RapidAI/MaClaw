//go:build !windows

package main

// 非 Windows 平台的 wails3 托盘暂未实现（原 v2 路径依赖
// appOptions.Menu/EditMenu 与 v2 菜单体系，随 v2 移除退役）。
// main3 统一以单参调用 setupTray(app)，故非 Windows 桩与 Windows 版
// (tray_windows.go) 保持同一签名，避免 darwin/linux 编译
// "not enough arguments in call to setupTray"。
var UpdateTrayVisibility = func(bool) {}

func setupTray(app *App) {}
