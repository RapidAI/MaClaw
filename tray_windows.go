//go:build windows

package main

import (
	"context"
	stdruntime "runtime"
	"time"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func setupTray(app *App, appOptions *options.App) {
	appOptions.OnStartup = func(ctx context.Context) {
		app.startup(ctx)

		go func() {
			// Lock the OS thread for the systray message loop on Windows
			stdruntime.LockOSThread()
			
			systray.Run(func() {
				systray.SetIcon(icon)
	systray.SetTitle("MaClaw")
	systray.SetTooltip("MaClaw Dashboard")
				systray.SetOnDClick(func(menu systray.IMenu) {
					go func() {
						runtime.WindowShow(app.ctx)
						runtime.WindowSetAlwaysOnTop(app.ctx, true)
						runtime.WindowSetAlwaysOnTop(app.ctx, false)
					}()
				})

			mShow := systray.AddMenuItem("Show", "Show Main Window")
			mLaunch := systray.AddMenuItem("开始编程", "Start Coding")
			systray.AddSeparator()

			// Tool menu items map
			toolItems := make(map[string]*systray.MenuItem)

			// Load config to populate tray
			config, _ := app.LoadConfig()

			// 1. Claude Code Submenu
			mClaude := systray.AddMenuItem("Claude Code", "Claude Code Models")
			for _, model := range config.Claude.Models {
				m := mClaude.AddSubMenuItemCheckbox(model.ModelName, "Switch to "+model.ModelName, model.ModelName == config.Claude.CurrentModel && config.ActiveTool == "claude")
				toolItems["claude-"+model.ModelName] = m

				modelName := model.ModelName
				m.Click(func() {
					go func() {
						currentConfig, _ := app.LoadConfig()
						currentConfig.Claude.CurrentModel = modelName
						currentConfig.ActiveTool = "claude"
						app.SaveConfig(currentConfig)

						for _, m := range currentConfig.Claude.Models {
							if m.ModelName == modelName && m.ApiKey == "" {
								runtime.WindowShow(app.ctx)
								break
							}
						}
					}()
				})
			}

			// 2. Gemini CLI Submenu
			mGemini := systray.AddMenuItem("Gemini CLI", "Gemini CLI Models")
			for _, model := range config.Gemini.Models {
				m := mGemini.AddSubMenuItemCheckbox(model.ModelName, "Switch to "+model.ModelName, model.ModelName == config.Gemini.CurrentModel && config.ActiveTool == "gemini")
				toolItems["gemini-"+model.ModelName] = m

				modelName := model.ModelName
				m.Click(func() {
					go func() {
						currentConfig, _ := app.LoadConfig()
						currentConfig.Gemini.CurrentModel = modelName
						currentConfig.ActiveTool = "gemini"
						app.SaveConfig(currentConfig)

						for _, m := range currentConfig.Gemini.Models {
							if m.ModelName == modelName && m.ApiKey == "" {
								runtime.WindowShow(app.ctx)
								break
							}
						}
					}()
				})
			}

			// 3. Codex Submenu
			mCodex := systray.AddMenuItem("OpenAI Codex", "Codex Models")
			for _, model := range config.Codex.Models {
				m := mCodex.AddSubMenuItemCheckbox(model.ModelName, "Switch to "+model.ModelName, model.ModelName == config.Codex.CurrentModel && config.ActiveTool == "codex")
				toolItems["codex-"+model.ModelName] = m

				modelName := model.ModelName
				m.Click(func() {
					go func() {
						currentConfig, _ := app.LoadConfig()
						currentConfig.Codex.CurrentModel = modelName
						currentConfig.ActiveTool = "codex"
						app.SaveConfig(currentConfig)

						for _, m := range currentConfig.Codex.Models {
							if m.ModelName == modelName && m.ApiKey == "" {
								runtime.WindowShow(app.ctx)
								break
							}
						}
					}()
				})
			}

			// 4. OpenCode Submenu
			mOpenCode := systray.AddMenuItem("OpenCode AI", "OpenCode Models")
			for _, model := range config.Opencode.Models {
				m := mOpenCode.AddSubMenuItemCheckbox(model.ModelName, "Switch to "+model.ModelName, model.ModelName == config.Opencode.CurrentModel && config.ActiveTool == "opencode")
				toolItems["opencode-"+model.ModelName] = m

				modelName := model.ModelName
				m.Click(func() {
					go func() {
						currentConfig, _ := app.LoadConfig()
						currentConfig.Opencode.CurrentModel = modelName
						currentConfig.ActiveTool = "opencode"
						app.SaveConfig(currentConfig)

						for _, m := range currentConfig.Opencode.Models {
							if m.ModelName == modelName && m.ApiKey == "" {
								runtime.WindowShow(app.ctx)
								break
							}
						}
					}()
				})
			}

			// 5. CodeBuddy Submenu
			mCodeBuddy := systray.AddMenuItem("CodeBuddy AI", "CodeBuddy Models")
			for _, model := range config.CodeBuddy.Models {
				m := mCodeBuddy.AddSubMenuItemCheckbox(model.ModelName, "Switch to "+model.ModelName, model.ModelName == config.CodeBuddy.CurrentModel && config.ActiveTool == "codebuddy")
				toolItems["codebuddy-"+model.ModelName] = m

				modelName := model.ModelName
				m.Click(func() {
					go func() {
						currentConfig, _ := app.LoadConfig()
						currentConfig.CodeBuddy.CurrentModel = modelName
						currentConfig.ActiveTool = "codebuddy"
						app.SaveConfig(currentConfig)

						for _, m := range currentConfig.CodeBuddy.Models {
							if m.ModelName == modelName && m.ApiKey == "" {
								runtime.WindowShow(app.ctx)
								break
							}
						}
					}()
				})
			}

			// 6. Qoder CLI Submenu
			mQoder := systray.AddMenuItem("Qoder CLI", "Qoder Models")
			for _, model := range config.Qoder.Models {
				m := mQoder.AddSubMenuItemCheckbox(model.ModelName, "Switch to "+model.ModelName, model.ModelName == config.Qoder.CurrentModel && config.ActiveTool == "qoder")
				toolItems["qoder-"+model.ModelName] = m

				modelName := model.ModelName
				m.Click(func() {
					go func() {
						currentConfig, _ := app.LoadConfig()
						currentConfig.Qoder.CurrentModel = modelName
						currentConfig.ActiveTool = "qoder"
						app.SaveConfig(currentConfig)

						for _, m := range currentConfig.Qoder.Models {
							if m.ModelName == modelName && m.ApiKey == "" {
								runtime.WindowShow(app.ctx)
								break
							}
						}
					}()
				})
			}

			// 7. iFlow CLI Submenu
			mIFlow := systray.AddMenuItem("iFlow CLI", "iFlow Models")
			for _, model := range config.IFlow.Models {
				m := mIFlow.AddSubMenuItemCheckbox(model.ModelName, "Switch to "+model.ModelName, model.ModelName == config.IFlow.CurrentModel && config.ActiveTool == "iflow")
				toolItems["iflow-"+model.ModelName] = m

				modelName := model.ModelName
				m.Click(func() {
					go func() {
						currentConfig, _ := app.LoadConfig()
						currentConfig.IFlow.CurrentModel = modelName
						currentConfig.ActiveTool = "iflow"
						app.SaveConfig(currentConfig)

						for _, m := range currentConfig.IFlow.Models {
							if m.ModelName == modelName && m.ApiKey == "" {
								runtime.WindowShow(app.ctx)
								break
							}
						}
					}()
				})
			}

		// 8. Kilo Code CLI Submenu
		mKilo := systray.AddMenuItem("Kilo Code CLI", "Kilo Code Models")
		for _, model := range config.Kilo.Models {
			m := mKilo.AddSubMenuItemCheckbox(model.ModelName, "Switch to "+model.ModelName, model.ModelName == config.Kilo.CurrentModel && config.ActiveTool == "kilo")
			toolItems["kilo-"+model.ModelName] = m

			modelName := model.ModelName
			m.Click(func() {
				go func() {
					currentConfig, _ := app.LoadConfig()
					currentConfig.Kilo.CurrentModel = modelName
					currentConfig.ActiveTool = "kilo"
					app.SaveConfig(currentConfig)

					for _, m := range currentConfig.Kilo.Models {
						if m.ModelName == modelName && m.ApiKey == "" {
							runtime.WindowShow(app.ctx)
							break
						}
					}
				}()
			})
		}

		// 9. Kode CLI Submenu
		mKode := systray.AddMenuItem("Kode CLI", "Kode CLI Models")
		for _, model := range config.Kode.Models {
			m := mKode.AddSubMenuItemCheckbox(model.ModelName, "Switch to "+model.ModelName, model.ModelName == config.Kode.CurrentModel && config.ActiveTool == "kode")
			toolItems["kode-"+model.ModelName] = m

			modelName := model.ModelName
			m.Click(func() {
				go func() {
					currentConfig, _ := app.LoadConfig()
					currentConfig.Kode.CurrentModel = modelName
					currentConfig.ActiveTool = "kode"
					app.SaveConfig(currentConfig)

					for _, m := range currentConfig.Kode.Models {
						if m.ModelName == modelName && m.ApiKey == "" {
							runtime.WindowShow(app.ctx)
							break
						}
					}
				}()
			})
		}

							systray.AddSeparator()

							mQuit := systray.AddMenuItem("Quit", "Quit Application")

				

							var isVisible bool = true

				

							// Register update function

							UpdateTrayMenu = func(lang string) {

								t, ok := trayTranslations[lang]

								if !ok {

									t = trayTranslations["en"]

								}

								systray.SetTitle(t["title"])

								systray.SetTooltip(t["title"])

								if isVisible {

									mShow.SetTitle(t["hide"])

								} else {

									mShow.SetTitle(t["show"])

								}

								mLaunch.SetTitle(t["launch"])

												mQuit.SetTitle(t["quit"])

											}

								

											UpdateTrayVisibility = func(visible bool) {

												isVisible = visible

												UpdateTrayMenu(app.CurrentLanguage)

											}


											// Register config change listener
											OnConfigChanged = func(cfg AppConfig) {
												if toolItems == nil {
													return
												}
												for name, item := range toolItems {
													// Only check the currently active tool's current model
													if (cfg.ActiveTool == "claude" && name == "claude-"+cfg.Claude.CurrentModel) ||
														(cfg.ActiveTool == "gemini" && name == "gemini-"+cfg.Gemini.CurrentModel) ||
														(cfg.ActiveTool == "codex" && name == "codex-"+cfg.Codex.CurrentModel) ||
														(cfg.ActiveTool == "opencode" && name == "opencode-"+cfg.Opencode.CurrentModel) ||
														(cfg.ActiveTool == "codebuddy" && name == "codebuddy-"+cfg.CodeBuddy.CurrentModel) ||
														(cfg.ActiveTool == "qoder" && name == "qoder-"+cfg.Qoder.CurrentModel) ||
														(cfg.ActiveTool == "iflow" && name == "iflow-"+cfg.IFlow.CurrentModel) ||
														(cfg.ActiveTool == "kilo" && name == "kilo-"+cfg.Kilo.CurrentModel) ||
														(cfg.ActiveTool == "kode" && name == "kode-"+cfg.Kode.CurrentModel) {
														item.Check()
													} else {
														item.Uncheck()
													}
												}
												runtime.EventsEmit(app.ctx, "config-changed", cfg)
											}

											// Handle menu clicks

								

							mShow.Click(func() {

								go func() {

									if isVisible {

										runtime.WindowHide(app.ctx)

										isVisible = false

									} else {

										runtime.WindowShow(app.ctx)

										runtime.WindowSetAlwaysOnTop(app.ctx, true)

										runtime.WindowSetAlwaysOnTop(app.ctx, false)

										isVisible = true

									}

									UpdateTrayMenu(app.CurrentLanguage)

								}()

							})

				
			mLaunch.Click(func() {
				go func() {
					currentConfig, _ := app.LoadConfig()
					path := app.GetCurrentProjectPath()
					app.LaunchTool(currentConfig.ActiveTool, false, false, false, "", path, false)
				}()
			})

			mQuit.Click(func() {
				go func() {
					systray.Quit()
					runtime.Quit(app.ctx)
				}()
			})

			if app.CurrentLanguage != "" {
				go func() {
					time.Sleep(500 * time.Millisecond)
					UpdateTrayMenu(app.CurrentLanguage)
				}()
			}
		}, func() {
			// Cleanup logic on exit
		})
		}()
	}
}
