package guiapp

import "sync/atomic"

// agentViewI18n provides localized strings for AgentView builder functions.
// The language is set by App.SetLanguage() and read by builder functions
// without needing an App reference.

var agentViewCurrentLang atomic.Value // stores string

func init() {
	agentViewCurrentLang.Store("en")
}

// setAgentViewLang is called by App.SetLanguage to keep AgentView builders in sync.
func setAgentViewLang(lang string) {
	if lang == "" {
		lang = "en"
	}
	agentViewCurrentLang.Store(lang)
}

// avTr returns the localized string for AgentView UI text.
// Simplified and Traditional Chinese use zh. Cyrillic Mongolian uses the
// English sentence as a key into avMn, then English. Every other language
// uses English, so Mongolian never falls through to Chinese.
func avTr(en, zh string) string {
	lang, _ := agentViewCurrentLang.Load().(string)
	kind := normalizeAppLanguageKind(lang)
	if kind.IsChinese() {
		return zh
	}
	if kind.IsMongolian() {
		if translated := avMn[en]; translated != "" {
			return translated
		}
		return en
	}
	return en
}

var avMn = map[string]string{
	"Show Main Window":       "Үндсэн цонхыг харуулах",
	"Hide Main Window":       "Үндсэн цонхыг нуух",
	"Computer Use":           "Computer Use",
	"Status: idle":           "Төлөв: сул",
	"Status: active":         "Төлөв: идэвхтэй",
	"Status: paused":         "Төлөв: түр зогссон",
	"Status: stopped":        "Төлөв: зогссон",
	"Pause desktop actions":  "Ширээний үйлдлийг түр зогсоох",
	"Resume desktop actions": "Ширээний үйлдлийг үргэлжлүүлэх",
	"Stop desktop control":   "Ширээний удирдлагыг зогсоох",
	"Reset control state":    "Удирдлагын төлөвийг шинэчлэх",
	"\nReply by number, for example:\n1. My project\n2. Go\n3. Windows\n...": "\nДугаараар хариулна уу, жишээ нь:\n1. Миний төсөл\n2. Go\n3. Windows\n...",
	" (choose: ": " (сонгох: ",
	" - ":        " - ",
	" result":    " үр дүн",
	")":          ")",
	"A matching unfinished business transaction already exists. Choose one to reopen its structured form, or start a new task from chat if this is unrelated.": "Тохирох дуусаагүй бизнесийн гүйлгээ аль хэдийн байна. Бүтэцтэй маягтыг дахин нээхийн тулд нэгийг нь сонгох, эсвэл холбоогүй бол чатаас шинэ даалгавар эхлүүлнэ үү.",
	"A note is required when approving.":        "Батлах үед тэмдэглэл шаардлагатай.",
	"A note is required when rejecting.":        "Татгалзах үед тэмдэглэл шаардлагатай.",
	"AI assistant is not initialized.":          "AI туслах эхлээгүй байна.",
	"AI assistant not initialized.":             "AI туслах эхлээгүй.",
	"Action":                                    "Үйлдэл",
	"Add data source":                           "Өгөгдлийн эх нэмэх",
	"App id does not match the open workspace.": "appId нээлттэй ажлын талбартай таарахгүй байна.",
	"App view submit requires appId.":           "Хэрэглээний харагдац илгээхэд appId шаардлагатай.",
	"App view submit requires schema_version.":  "Хэрэглээний харагдац илгээхэд schema_version шаардлагатай.",
	"App view submit requires view_revision.":   "Хэрэглээний харагдац илгээхэд view_revision шаардлагатай.",
	"App view submit requires view_revision. Re-open the app workspace and try again.": "Хэрэглээний харагдац илгээхэд view_revision шаардлагатай. Хэрэглээний ажлын талбарыг дахин нээгээд оролдоно уу.",
	"App workspace refreshed in the task panel.":                                       "Хэрэглээний ажлын талбар даалгаврын самбар дээр шинэчлэгдлээ.",
	"Apply mapping":                                 "Харгалзуулалтыг хэрэгжүүлэх",
	"Approval decision applied.":                    "Батламжийн шийдвэр хэрэгжлээ.",
	"Approval was already final.":                   "Батламж аль хэдийн эцэслэсэн.",
	"Approval workflow instance":                    "Батламжийн ажлын урсгалын процесс",
	"Approval workspace":                            "Батламжийн ажлын талбар",
	"Approval workspace is open in the task panel.": "Батламжийн ажлын талбар даалгаврын самбар дээр нээлттэй.",
	"Approve":                        "Батлах",
	"Approve and run":                "Батлаад ажиллуулах",
	"Approve tool execution":         "Хэрэгслийн гүйцэтгэлийг батлах",
	"Approved from AppView panel":    "AppView самбараас батлагдсан",
	"Artifact":                       "Бүтээгдэхүүн",
	"Business action committed":      "Бизнесийн үйлдэл баталгаажлаа",
	"Business operation completed.":  "Бизнесийн үйлдэл дууссан.",
	"Business result":                "Бизнесийн үр дүн",
	"Business transaction workspace": "Бизнесийн гүйлгээний ажлын талбар",
	"Call propose_profile with host/username/database. Do not send the password as a tool argument.": "host/username/database-тай propose_profile дуудна уу. Нууц үгийг хэрэгслийн аргумент болгон бүү илгээгээрэй.",
	"Choose a workspace .xlsx path. Existing files are not overwritten without approval.":            "Ажлын талбарын .xlsx замыг сонгоно уу. Байгаа файлыг батламжгүйгээр дарж бичихгүй.",
	"Choose an unfinished business transaction to reopen its structured form.":                       "Бүтэцтэй маягтыг дахин нээхийн тулд дуусаагүй бизнесийн гүйлгээ сонгоно уу.",
	"Choose business task": "Бизнесийн даалгавар сонгох",
	"Completed files: %s":  "Дууссан файл: %s",
	"Connect":              "Холбох",
	"Connected. Continue with inspect/query; do not use ssh.": "Холбогдлоо. inspect/query-г үргэлжлүүлнэ үү; ssh бүү ашигла.",
	"Continue":                      "Үргэлжлүүлэх",
	"Continue business transaction": "Бизнесийн гүйлгээг үргэлжлүүлэх",
	"Continue with inspect/query; do not use ssh.": "inspect/query-г үргэлжлүүлнэ үү; ssh бүү ашигла.",
	"Dashboard":                            "Хянах самбар",
	"Data source saved to the OS keyring.": "Өгөгдлийн эх үйлдлийн системийн түлхүүрийн санд хадгалагдлаа.",
	"Data sources":                         "Өгөгдлийн эх",
	"DataSrv query result. Edit is local-only unless you re-run.": "DataSrv асуулгын үр дүн. Засвар зөвхөн локал бөгөөд дахин ажиллуулахаас нааш хадгалагдахгүй.",
	"Database":      "Өгөгдлийн сан",
	"Decision note": "Шийдвэрийн тэмдэглэл",
	"Delete":        "Устгах",
	"Describe the structured business data to submit.": "Илгээх бүтэцтэй бизнесийн өгөгдлийг тайлбарлана уу.",
	"Details": "Дэлгэрэнгүй",
	"Enter the password in the task panel. It is stored in the OS keyring, not in chat.": "Нууц үгээ даалгаврын самбарт оруулна уу. Үйлдлийн системийн түлхүүрийн санд хадгалагдах бөгөөд чатад орохгүй.",
	"Export":       "Экспортлох",
	"Export Excel": "Excel экспортлох",
	"External coding session exited after repeated resume attempts. Summarize current progress and continue remaining coding work through internal CodingSubAgent.": "Гадаад кодчиллын сесс олон дахин үргэлжлүүлсний дараа дууссан. Одоогийн явцыг нэгтгээд үлдсэн кодчиллыг дотоод CodingSubAgent-аар үргэлжлүүл.",
	"External coding session exited. Continue any remaining coding work through internal CodingSubAgent; do not create another external coding session.":            "Гадаад кодчиллын сесс дууссан. Үлдсэн кодчиллыг дотоод CodingSubAgent-аар үргэлжлүүл; өөр гадаад кодчиллын сесс бүү үүсгэ.",
	"Failed to apply approval decision.":                           "Батламжийн шийдвэрийг хэрэгжүүлж чадсангүй.",
	"Failed to close the task panel.":                              "Даалгаврын самбарыг хааж чадсангүй.",
	"Failed to refresh app workspace.":                             "Хэрэглээний ажлын талбарыг шинэчилж чадсангүй.",
	"Feedback":                                                     "Санал хүсэлт",
	"Fill MCP tool parameters before running.":                     "Ажиллуулахын өмнө MCP хэрэгслийн параметрийг бөглөнө үү.",
	"Fill required parameters before running this standard skill.": "Энэ стандарт Skill-ийг ажиллуулахын өмнө шаардлагатай параметрийг бөглөнө үү.",
	"Fill required parameters before running this tool.":           "Энэ хэрэгслийг ажиллуулахын өмнө шаардлагатай параметрийг бөглөнө үү.",
	"Fix the highlighted business data and submit again.":          "Тэмдэглэсэн бизнесийн өгөгдлийг засаад дахин илгээнэ үү.",
	"Host":                           "Хост",
	"ID":                             "ID",
	"Instance":                       "Процесс",
	"Invalid task panel dismissal.":  "Даалгаврын самбарыг хаах үйлдэл буруу.",
	"Invalid task panel submission.": "Даалгаврын самбарын илгээлт буруу.",
	"Last progress: %s":              "Сүүлийн явц: %s",
	"Legacy resume context available (attempt %d). Do not use external session tools for new agent work; route unfinished coding tasks through internal CodingSubAgent.": "Хуучин үргэлжлүүлэх контекст байна (%d дахь оролдлого). Шинэ Agent-ийн ажилд гадаад сессийн хэрэгсэл бүү ашигла; дуусаагүй кодчиллын даалгаврыг дотоод CodingSubAgent-аар явуул.",
	"Legacy resume_session_id for audit only: %s":           "Хуучин resume_session_id зөвхөн аудитад: %s",
	"Local business task":                                   "Локал бизнесийн даалгавар",
	"MCP tool":                                              "MCP хэрэгсэл",
	"MCP tool execution completed.":                         "MCP хэрэгслийн гүйцэтгэл дууссан.",
	"MCP tool parameters need correction before execution.": "Гүйцэтгэхээс өмнө MCP хэрэгслийн параметрийг засах шаардлагатай.",
	"MIS data service is unavailable. Form generated locally from your description.": "MIS өгөгдлийн үйлчилгээ боломжгүй. Маягтыг таны тайлбараас локалд үүсгэсэн.",
	"Missing app id for workspace refresh.":                                          "Ажлын талбарыг шинэчлэх appId дутуу.",
	"Multi-region DataSrv workspace":                                                 "Олон бүсийн DataSrv ажлын талбар",
	"Name":                                                                           "Нэр",
	"Next page":                                                                      "Дараагийн хуудас",
	"No active transactions":                                                         "Идэвхтэй гүйлгээ алга",
	"No data sources":                                                                "Өгөгдлийн эх алга",
	"No saved queries":                                                               "Хадгалсан асуулга алга",
	"No unfinished business transactions are available.":                             "Дуусаагүй бизнесийн гүйлгээ алга.",
	"Open form":                                                                      "Маягт нээх",
	"Original task: %s":                                                              "Анхны даалгавар: %s",
	"Output":                                                                         "Гаралт",
	"Output file":                                                                    "Гаралтын файл",
	"Owner-bound parameterized SELECT snippets.":                                     "Эзэмшигчид холбогдсон параметржүүлсэн SELECT хэсэг.",
	"Password":                                                                       "Нууц үг",
	"Password is stored in the OS keyring and never sent back to the model.":                    "Нууц үг үйлдлийн системийн түлхүүрийн санд хадгалагдах бөгөөд загвар руу буцаж очихгүй.",
	"Password was saved but did not authenticate. Update it in the task panel.":                 "Нууц үг хадгалагдсан боловч баталгаажсангүй. Даалгаврын самбар дээр шинэчилнэ үү.",
	"Pick a profile, or add one from chat with propose_profile.":                                "Хөрөг сонгох, эсвэл чатаас propose_profile-аар нэмнэ үү.",
	"Please provide the following information in order. Fields marked with * are required.\n\n": "Дараах мэдээллийг дарааллаар нь өгнө үү. * тэмдэгтэй талбар шаардлагатай.\n\n",
	"Port":                            "Порт",
	"Records":                         "Бичлэгүүд",
	"Refresh":                         "Шинэчлэх",
	"Reject":                          "Татгалзах",
	"Report":                          "Тайлан",
	"Required for approve and reject": "Батлах болон татгалзахад шаардлагатай",
	"Required parameters are missing. Fill them here to run the skill safely.": "Шаардлагатай параметр дутуу. Skill-ийг аюулгүй ажиллуулахын тулд энд бөглөнө үү.",
	"Required tool parameters are missing. Fill them here to continue.":        "Хэрэгслийн шаардлагатай параметр дутуу. Үргэлжлүүлэхийн тулд энд бөглөнө үү.",
	"Required when rejecting; optional when approving":                         "Татгалзахад шаардлагатай; батлахад заавал биш",
	"Result":                                "Үр дүн",
	"Result package":                        "Үр дүнгийн багц",
	"Retry business commit":                 "Бизнесийн баталгаажуулалтыг дахин оролдох",
	"Retry validation":                      "Баталгаажуулалтыг дахин оролдох",
	"Review and decide in the task panel.":  "Даалгаврын самбар дээр шалгаад шийднэ үү.",
	"Review this operation before it runs.": "Энэ үйлдэл ажиллахаас өмнө шалгана уу.",
	"Run":                                   "Ажиллуулах",
	"Run ":                                  "Ажиллуулах ",
	"Run MCP tool":                          "MCP хэрэгсэл ажиллуулах",
	"Run MCP tool: ":                        "MCP хэрэгсэл ажиллуулах: ",
	"Run skill":                             "Skill ажиллуулах",
	"Run tool":                              "Хэрэгсэл ажиллуулах",
	"Running ":                              "Ажиллаж байна ",
	"Save":                                  "Хадгалах",
	"Saved queries":                         "Хадгалсан асуулга",
	"Saved. Keep using the database tool; do not fall back to ssh.": "Хадгалагдлаа. Өгөгдлийн сангийн хэрэгслийг үргэлжлүүлэн ашиглана уу; ssh руу бүү буц.",
	"Server: ": "Сервер: ",
	"Session id does not match the open workspace.": "sessionId нээлттэй ажлын талбартай таарахгүй байна.",
	"Skill execution finished.":                     "Skill-ийн гүйцэтгэл дууссан.",
	"Skill result":                                  "Skill-ийн үр дүн",
	"Skill running":                                 "Skill ажиллаж байна",
	"Step %d":                                       "Алхам %d",
	"Submit":                                        "Илгээх",
	"Submit structured data":                        "Бүтэцтэй өгөгдөл илгээх",
	"Summary":                                       "Хураангуй",
	"Sync and workflow feedback":                    "Синк ба ажлын урсгалын санал хүсэлт",
	"Sync decision to DataSrv when configured":      "Тохируулсан бол шийдвэрийг DataSrv руу синк хийх",
	"Task panel closed.":                            "Даалгаврын самбар хаагдлаа.",
	"Task panel submission failed.":                 "Даалгаврын самбарын илгээлт амжилтгүй.",
	"Task panel submission received.":               "Даалгаврын самбарын илгээлт хүлээн авлаа.",
	"The business transaction is saved locally and can be edited or retried from this panel.":                    "Бизнесийн гүйлгээ локалд хадгалагдсан бөгөөд энэ самбараас засах эсвэл дахин оролдож болно.",
	"The intent is plausible but not certain. Choose the target business operation to open its structured form.": "Зорилго боломжтой боловч баттай биш. Бүтэцтэй маягтыг нээхийн тулд зорилтот бизнесийн үйлдлийг сонгоно уу.",
	"The saved password could not connect. Enter a new password. It stays in the OS keyring.":                    "Хадгалсан нууц үгээр холбогдож чадсангүй. Шинэ нууц үг оруулна уу. Үйлдлийн системийн түлхүүрийн санд үлдэнэ.",
	"The structured business data was written through MaClawDataSrv.":                                            "Бүтэцтэй бизнесийн өгөгдөл MaClawDataSrv-ээр бичигдлээ.",
	"The user submitted the workflow form. Generate the phase output from the persisted form data.":              "Хэрэглэгч ажлын урсгалын маягтыг илгээсэн. Хадгалсан маягтын өгөгдлөөс үе шатын гаралтыг үүсгэ.",
	"The user submitted workflow form data: ":                                                                    "Хэрэглэгч ажлын урсгалын маягтын өгөгдөл илгээсэн: ",
	"This app workspace is out of date. Close it and use the latest view.":                                       "Энэ хэрэглээний ажлын талбар хуучирсан. Хаагаад хамгийн шинэ харагдацыг ашиглана уу.",
	"This app workspace schema changed. Refresh and submit again.":                                               "Энэ хэрэглээний ажлын талбарын schema өөрчлөгдсөн. Шинэчлээд дахин илгээнэ үү.",
	"This region failed to load. Switch tabs or refresh.":                                                        "Энэ бүс ачааллагдсангүй. Таб солих эсвэл шинэчилнэ үү.",
	"This task panel is out of date. Close it and use the latest form that just opened.":                         "Энэ даалгаврын самбар хуучирсан. Хаагаад дөнгөж нээгдсэн хамгийн шинэ маягтыг ашиглана уу.",
	"This task panel schema changed. Refresh the form and submit again.":                                         "Энэ даалгаврын самбарын schema өөрчлөгдсөн. Маягтыг шинэчлээд дахин илгээнэ үү.",
	"This workflow is no longer active.":                                                                         "Энэ ажлын урсгал идэвхгүй болсон.",
	"Tool":                                                                                                       "Хэрэгсэл",
	"Tool execution completed.":                                                                                  "Хэрэгслийн гүйцэтгэл дууссан.",
	"Type":                                                                                                       "Төрөл",
	"Update approval instance status":                                                                            "Батламжийн процессийн төлөвийг шинэчлэх",
	"Update password":                                                                                            "Нууц үг шинэчлэх",
	"Username":                                                                                                   "Хэрэглэгчийн нэр",
	"Validate again":                                                                                             "Дахин баталгаажуулах",
	"Validation failed":                                                                                          "Баталгаажуулалт амжилтгүй",
	"Waiting for MIS validation":                                                                                 "MIS баталгаажуулалтыг хүлээж байна",
	"Workflow cancelled. Describe your task again to start a new workflow.":                                      "Ажлын урсгал цуцлагдлаа. Шинэ ажлын урсгал эхлүүлэхийн тулд даалгавраа дахин тайлбарлана уу.",
	"done":      "дууссан",
	"rows":      "мөр",
	"saved":     "хадгалсан",
	"source ":   "эх ",
	"status: ":  "төлөв: ",
	"truncated": "таслагдсан",
}
