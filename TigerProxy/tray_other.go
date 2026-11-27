//go:build !windows

package main

// 非 Windows 平台的 wails3 托盘暂未实现（原 v2 路径依赖
// appOptions.Menu/EditMenu 与 v2 菜单体系，随 v2 移除退役）。
var UpdateTrayVisibility = func(bool) {}

func setupTray(app *App, appOptions *any) {}
