/**
 * File-companion chrome. English is the Mongolian key.
 * Missing Mongolian stays English. Chinese is only returned for zh-Hans / zh-Hant.
 * Short labels live here, not in the global tail map.
 */
import { normalizeLang } from "./langSelect";

type Row = readonly [string, string, string];

// zh, en, mn. Templates use ${name} in the same order in all three.
const ROWS: readonly Row[] = [
    ["保存失败", "Save failed", "Хадгалж чадсангүй"],
    ["保存修改", "Save changes", "Өөрчлөлтийг хадгалах"],
    ["保留扫描版式，生成可以选中和搜索的 PDF", "Keep the scanned layout and make a PDF you can select and search", "Сканнердсан бүтцийг хадгалж, сонгож хайж болох PDF үүсгэнэ"],
    ["本机", "This computer", "Энэ компьютер"],
    ["本机项目", "On this computer", "Энэ компьютер дээр"],
    ["笔", "Pen", "Үзэг"],
    ["笔记", "Notes", "Тэмдэглэл"],
    ["笔记进度", "Note progress", "Тэмдэглэлийн явц"],
    ["别人的分享", "Shared by someone else", "Бусдын хуваалцсан"],
    ["不支持打开文件夹", "Folders cannot be opened", "Хавтсыг нээх боломжгүй"],
    ["不支持上传文件夹", "Folders cannot be uploaded", "Хавтсыг байршуулах боломжгүй"],
    ["参与者的邮箱或手机号", "Participant email or phone", "Оролцогчийн имэйл эсвэл утас"],
    ["操作失败", "The request failed", "Үйлдэл амжилтгүй"],
    ["成员", "Member", "Гишүүн"],
    ["处理中", "Working", "Боловсруулж байна"],
    ["创建目录失败", "Could not create the folder", "Хавтас үүсгэж чадсангүй"],
    ["磁盘已满", "The disk is full", "Диск дүүрсэн"],
    ["从文档管理移除；云端分享仍属于分享者", "Remove from the library. The cloud share still belongs to the person who shared it", "Баримтын сангаас хасна. Үүлэн дэх хуваалцалт эзэндээ үлдэнэ"],
    ["打不开这个文件", "This file cannot be opened", "Энэ файлыг нээж чадсангүй"],
    ["打不开这个页面", "This page cannot be opened", "Энэ хуудсыг нээж чадсангүй"],
    ["打开的文档", "Open documents", "Нээлттэй баримтууд"],
    ["打开方式", "Open with", "Нээх арга"],
    ["打开失败", "Could not open", "Нээж чадсангүй"],
    ["打开中…", "Opening…", "Нээж байна…"],
    ["待确认", "Waiting to confirm", "Баталгаажуулалтыг хүлээж байна"],
    ["当前文档", "Current document", "Одоогийн баримт"],
    ["当前主持人：你", "Current host: you", "Одоогийн хөтлөгч: та"],
    ["导出", "Export", "Экспорт"],
    ["导出 PDF", "Export PDF", "PDF экспортлох"],
    ["导出 PDF — 按聊天区原来的格式，把全部对话导出", "Export PDF — export the whole conversation in the chat layout", "PDF экспортлох — бүх яриаг чатын бүтэцээр гаргана"],
    ["导出格式", "Export format", "Экспортын хэлбэр"],
    ["导出失败", "Export failed", "Экспорт амжилтгүй"],
    ["导出为 LaTeX", "Export as LaTeX", "LaTeX болгон экспортлох"],
    ["导出为 PDF", "Export as PDF", "PDF болгон экспортлох"],
    ["导出项目", "Export project", "Төслийг экспортлох"],
    ["导出项目 — 打包文档、高亮和聊天为 zip", "Export project — pack the document, highlights, and chat into a zip", "Төслийг экспортлох — баримт, тодруулга, чатыг zip болгоно"],
    ["导出项目 — 文档、高亮和聊天记录打包成项目", "Export project — pack the document, highlights, and chat into a project", "Төслийг экспортлох — баримт, тодруулга, чатыг төсөл болгон багцална"],
    ["导出中…", "Exporting…", "Экспортлож байна…"],
    ["导入", "Import", "Импорт"],
    ["导入到文档库", "Import into the library", "Баримтын санд оруулах"],
    ["导入分享 — 粘贴链接存进本机", "Import a share — paste a link to keep it on this computer", "Хуваалцалт оруулах — холбоосыг буулгаж энэ компьютерт хадгална"],
    ["导入密码", "Import password", "Импортын нууц үг"],
    ["导入失败", "Import failed", "Импорт амжилтгүй"],
    ["导入失败：没有拿到文件", "Import failed: no file was received", "Импорт амжилтгүй: файл ирсэнгүй"],
    ["导入知识库", "Import into knowledge", "Мэдлэгийн санд оруулах"],
    ["导入知识库 — 存入知识库，以后可以检索", "Import into knowledge — keep it in the knowledge base so it can be searched later", "Мэдлэгийн санд оруулах — дараа нь хайхаар хадгална"],
    ["导入知识库失败", "Could not import into the knowledge base", "Мэдлэгийн санд оруулж чадсангүй"],
    ["等待主持人审核", "Waiting for the host to review", "Хөтлөгчийн шалгалтыг хүлээж байна"],
    ["第 1 页", "Page 1", "1-р хуудас"],
    ["读者", "Reader", "Уншигч"],
    ["对方的邮箱或手机号", "Their email or phone", "Харилцагчийн имэйл эсвэл утас"],
    ["对话为空，没有可导出的内容", "The conversation is empty, so there is nothing to export", "Яриа хоосон тул экспортлох зүйл алга"],
    ["发送", "Send", "Илгээх"],
    ["发送失败", "Could not send", "Илгээж чадсангүй"],
    ["分享", "Share", "Хуваалцах"],
    ["分享 — 上传文档、高亮和聊天，生成云端链接", "Share — upload the document, highlights, and chat, and make a cloud link", "Хуваалцах — баримт, тодруулга, чатыг байршуулж үүлэн холбоос үүсгэнэ"],
    ["分享包含文档、高亮和聊天，之后可同步新标注。", "A share includes the document, highlights, and chat. New marks can be synced later.", "Хуваалцалтад баримт, тодруулга, чат орно. Шинэ тэмдэглэгээг дараа нь синк хийнэ."],
    ["分享成功，链接已生成", "Shared. The link is ready", "Хуваалцлаа. Холбоос бэлэн"],
    ["分享到云端", "Share to the cloud", "Үүлэн дээр хуваалцах"],
    ["分享链接", "Share link", "Хуваалцах холбоос"],
    ["分享密码", "Share password", "Хуваалцах нууц үг"],
    ["分享密码需为 4–64 个字符", "The share password must be 4–64 characters", "Хуваалцах нууц үг 4–64 тэмдэгт байна"],
    ["分享时间不正确", "The share time is not valid", "Хуваалцах хугацаа буруу"],
    ["分享天数", "Days to share", "Хуваалцах өдөр"],
    ["分享已过期，可以重新分享", "This share has expired. You can share it again", "Хуваалцах хугацаа дууссан. Дахин хуваалцаж болно"],
    ["分享月数", "Months to share", "Хуваалцах сар"],
    ["复制分享链接", "Copy share link", "Хуваалцах холбоосыг хуулах"],
    ["复制失败", "Could not copy", "Хуулж чадсангүй"],
    ["复制失败，请手动选择链接", "Could not copy. Select the link yourself", "Хуулж чадсангүй. Холбоосыг гараар сонгоно уу"],
    ["复制为 Markdown", "Copy as Markdown", "Markdown болгон хуулах"],
    ["覆盖保存", "Overwrite", "Дарж хадгалах"],
    ["该文章已经取消分享", "This article is no longer shared", "Энэ материалын хуваалцалтыг цуцалсан"],
    ["改范围或有效期后，链接不变，已导入的人继续可用。", "Changing the audience or expiry keeps the same link. People who already imported it can still use it.", "Хүрээ эсвэл хугацааг өөрчилсөн ч холбоос хэвээр. Аль хэдийн оруулсан хүмүүс үргэлжлүүлэн ашиглана."],
    ["个月", "months", "сар"],
    ["公开", "Public", "Нээлттэй"],
    ["关闭", "Close", "Хаах"],
    ["管理员", "Admin", "Админ"],
    ["还没有项目", "No projects yet", "Төсөл хараахан алга"],
    ["恢复进入全屏前的窗口大小", "Restore the window size from before full screen", "Бүтэн дэлгэцээс өмнөх цонхны хэмжээнд буцаах"],
    ["恢复原始大小", "Restore original size", "Анхны хэмжээнд буцаах"],
    ["记笔记", "Take notes", "Тэмдэглэл хийх"],
    ["记笔记 — 把网页内容整理成笔记", "Take notes — turn this page into notes", "Тэмдэглэл хийх — хуудасны агуулгыг тэмдэглэл болгоно"],
    ["检索", "Search", "Хайх"],
    ["检索完成，没有结果", "Search finished, with no results", "Хайлт дууссан, үр дүн алга"],
    ["检索中…", "Searching…", "Хайж байна…"],
    ["可见范围", "Who can see this", "Хэн харах вэ"],
    ["链接已生成", "The link is ready", "Холбоос бэлэн"],
    ["留空则保持原密码", "Leave blank to keep the current password", "Хоосон орхивол одоогийн нууц үг хэвээр"],
    ["论文解读", "Read the paper", "Өгүүллийн тайлбар"],
    ["论文解读 — 生成本文的结构化解读报告", "Read the paper — write a structured briefing of this document", "Өгүүллийн тайлбар — энэ баримтын бүтэцтэй тайлбар бичнэ"],
    ["没能清空聊天", "Could not clear the chat", "Чатыг цэвэрлэж чадсангүй"],
    ["没有读取权限", "No permission to read this file", "Энэ файлыг унших эрх алга"],
    ["没有可导出的内容", "Nothing to export", "Экспортлох зүйл алга"],
    ["没有可导出的正文", "No body text to export", "Экспортлох үндсэн текст алга"],
    ["没有生成可选中的 PDF", "No selectable PDF was produced", "Сонгож болох PDF үүссэнгүй"],
    ["没有写入权限", "No permission to write this file", "Энэ файлд бичих эрх алга"],
    ["描述研究目标、关键字，或 arXiv 编号", "Describe the research goal, keywords, or an arXiv id", "Судалгааны зорилго, түлхүүр үг, эсвэл arXiv дугаараа бичнэ үү"],
    ["目标已修改，请重新检索", "The goal changed. Search again", "Зорилго өөрчлөгдсөн. Дахин хайна уу"],
    ["你是管理员", "You are an admin", "Та админ"],
    ["你是这份文档的主持人", "You are the host of this document", "Та энэ баримтын хөтлөгч"],
    ["你是主持人", "You are the host", "Та хөтлөгч"],
    ["批注没有同步出去", "The comment was not synced", "Тэмдэглэлийг синк хийсэнгүй"],
    ["其他人", "Someone else", "Өөр хүн"],
    ["请联系分享的主持人", "Contact the host of this share", "Энэ хуваалцлын хөтлөгчтэй холбогдоно уу"],
    ["请描述研究目标或输入关键字", "Describe a research goal or enter keywords", "Судалгааны зорилго эсвэл түлхүүр үг оруулна уу"],
    ["请设置分享密码", "Set a share password", "Хуваалцах нууц үг тохируулна уу"],
    ["请输入分享密码", "Enter the share password", "Хуваалцах нууц үгээ оруулна уу"],
    ["请填写参与者邮箱或手机号", "Enter a participant email or phone", "Оролцогчийн имэйл эсвэл утас оруулна уу"],
    ["请填写论文、页面或分享链接", "Enter a paper, a page, or a share link", "Өгүүлэл, хуудас, эсвэл хуваалцах холбоос оруулна уу"],
    ["请填写群组名称", "Enter a group name", "Бүлгийн нэр оруулна уу"],
    ["请填写要邀请的邮箱或手机号", "Enter the email or phone to invite", "Урих имэйл эсвэл утас оруулна уу"],
    ["请填写邮箱或手机号", "Enter an email or phone", "Имэйл эсвэл утас оруулна уу"],
    ["请选择群组", "Choose a group", "Бүлэг сонгоно уу"],
    ["请再点「转换」继续。", "Choose Convert again to continue.", "Үргэлжлүүлэхийн тулд «Хөрвүүлэх»-ийг дахин дарна уу."],
    ["请粘贴分享链接", "Paste a share link", "Хуваалцах холбоосыг буулгана уу"],
    ["取消", "Cancel", "Цуцлах"],
    ["取消 — 结束笔记生成", "Cancel — stop generating notes", "Цуцлах — тэмдэглэл үүсгэхийг зогсооно"],
    ["取消分享", "Stop sharing", "Хуваалцалтыг цуцлах"],
    ["取消分享 — 参与者会移除并关闭该文档", "Stop sharing — participants lose it and the document closes", "Хуваалцалтыг цуцлах — оролцогчдоос хасагдаж, баримт хаагдана"],
    ["取消中…", "Cancelling…", "Цуцалж байна…"],
    ["取消转移", "Cancel the transfer", "Шилжүүлгийг цуцлах"],
    ["全屏", "Full screen", "Бүтэн дэлгэц"],
    ["全屏阅读", "Read full screen", "Бүтэн дэлгэцээр унших"],
    ["确定", "OK", "Тийм"],
    ["确认", "Confirm", "Баталгаажуулах"],
    ["确认收回主持权，由你重新担任主持人？", "Take the host role back?", "Хөтлөгчийн эрхээ буцааж авах уу?"],
    ["群主", "Group owner", "Бүлгийн эзэн"],
    ["群组", "Group", "Бүлэг"],
    ["群组名称过长", "The group name is too long", "Бүлгийн нэр хэт урт"],
    ["群组图标", "Group icon", "Бүлгийн дүрс"],
    ["删除", "Delete", "Устгах"],
    ["删除伴读数据", "Delete companion data", "Дагалдах өгөгдлийг устгах"],
    ["删除伴读数据，不删除源文件", "Delete companion data. The source file stays", "Дагалдах өгөгдлийг устгана. Эх файл үлдэнэ"],
    ["删除分享", "Delete share", "Хуваалцалтыг устгах"],
    ["删除中…", "Deleting…", "Устгаж байна…"],
    ["上传到云盘", "Upload to the cloud drive", "Үүлэн диск рүү байршуулах"],
    ["上传到云盘 — 手机端「文档」可打开", "Upload to the cloud drive — open it from Documents on the phone", "Үүлэн диск рүү байршуулах — утасны «Баримт»-аас нээнэ"],
    ["上传到云盘失败，请稍后重试", "Upload to the cloud drive failed. Try again later", "Үүлэн диск рүү байршуулж чадсангүй. Дараа дахин оролдоно уу"],
    ["设置密码，4–64 个字符", "Set a password, 4–64 characters", "Нууц үг тохируулах, 4–64 тэмдэгт"],
    ["审核没有保存", "The review was not saved", "Шалгалт хадгалагдсангүй"],
    ["生成笔记", "Generate notes", "Тэмдэглэл үүсгэх"],
    ["生成笔记 — 把视频、音频内容整理成笔记", "Generate notes — turn the video or audio into notes", "Тэмдэглэл үүсгэх — видео, аудиог тэмдэглэл болгоно"],
    ["实验", "Experiment", "Туршилт"],
    ["收到的", "Received", "Хүлээн авсан"],
    ["收到的分享", "Shares you received", "Танд ирсэн хуваалцалт"],
    ["收回主持权", "Take back the host role", "Хөтлөгчийн эрхээ авах"],
    ["书", "Book", "Ном"],
    ["输入问题，Enter 发送", "Ask a question. Enter sends", "Асуултаа бичнэ үү. Enter илгээнэ"],
    ["私密", "Private", "Нууц"],
    ["私密分享才需要密码", "Only a private share needs a password", "Зөвхөн нууц хуваалцалтад нууц үг хэрэгтэй"],
    ["思考过程", "Reasoning", "Бодох явц"],
    ["搜索结果", "Search results", "Хайлтын үр дүн"],
    ["搜索失败", "Search failed", "Хайлт амжилтгүй"],
    ["天", "days", "өдөр"],
    ["停止", "Stop", "Зогсоох"],
    ["停止 — 结束网页笔记", "Stop — end the page note", "Зогсоох — хуудасны тэмдэглэлийг дуусгана"],
    ["同伴", "Companion", "Дагалдагч"],
    ["同步", "Sync", "Синк"],
    ["同步 — 把这份项目的新标注和聊天推到云端", "Sync — push this project's new marks and chat to the cloud", "Синк — энэ төслийн шинэ тэмдэглэгээ, чатыг үүлэн рүү илгээнэ"],
    ["同步 — 把最新标注和聊天推到这份分享", "Sync — push the latest marks and chat to this share", "Синк — хамгийн сүүлийн тэмдэглэгээ, чатыг энэ хуваалцалт руу илгээнэ"],
    ["同步中…", "Syncing…", "Синк хийж байна…"],
    ["拖动调整左右宽度", "Drag to resize the two sides", "Хоёр талын өргөнийг чирж тохируулна"],
    ["网页", "Web page", "Вэб хуудас"],
    ["网页笔记不能跳回播放位置", "A page note cannot jump back to a playback position", "Хуудасны тэмдэглэл тоглуулах байрлал руу буцаж үсэрч чадахгүй"],
    ["未命名群组", "Untitled group", "Нэргүй бүлэг"],
    ["未命名文档", "Untitled document", "Нэргүй баримт"],
    ["未能扩展关键词，已用原文中的英文词检索。", "The keywords could not be expanded. The search used the English words from the original text.", "Түлхүүр үгийг өргөтгөж чадсангүй. Эх текст дэх англи үгээр хайлаа."],
    ["文档", "Document", "Баримт"],
    ["文档管理", "Library", "Баримтын сан"],
    ["文档管理 — 本机文档与云端项目", "Library — documents on this computer and cloud projects", "Баримтын сан — энэ компьютер ба үүлэн төслүүд"],
    ["文件是空的", "The file is empty", "Файл хоосон"],
    ["文件太大，无法上传到云盘", "The file is too large to upload to the cloud drive", "Файл хэт том тул үүлэн диск рүү байршуулахгүй"],
    ["文件已导入，但没有打开", "The file was imported, but it did not open", "Файлыг оруулсан боловч нээгдсэнгүй"],
    ["文件已在桌面知识库中", "This file is already in the desktop knowledge base", "Энэ файл аль хэдийн ширээний мэдлэгийн санд бий"],
    ["文件在磁盘上已被修改", "The file was changed on disk", "Файлыг диск дээр өөрчилсөн"],
    ["我的分享", "My shares", "Миний хуваалцалт"],
    ["我的群组", "My groups", "Миний бүлгүүд"],
    ["无法保存到该位置", "Cannot save to that location", "Тэр байрлалд хадгалж чадсангүй"],
    ["下载失败", "Download failed", "Татаж чадсангүй"],
    ["下载失败：没有拿到文件路径", "Download failed: no file path was returned", "Татаж чадсангүй: файлын зам ирсэнгүй"],
    ["下载失败：没有拿到 PDF，可能这篇论文不存在", "Download failed: no PDF arrived. This paper may not exist", "Татаж чадсангүй: PDF ирсэнгүй. Энэ өгүүлэл байхгүй байж магадгүй"],
    ["下载失败：需要先在主窗口登录", "Download failed: sign in from the main window first", "Татаж чадсангүй: эхлээд үндсэн цонхонд нэвтэрнэ үү"],
    ["下载失败：找不到这篇论文（404）", "Download failed: this paper was not found (404)", "Татаж чадсангүй: энэ өгүүлэл олдсонгүй (404)"],
    ["下载失败：arXiv 拒绝了这次请求，请稍后再试", "Download failed: arXiv refused this request. Try again later", "Татаж чадсангүй: arXiv энэ хүсэлтийг татгалзлаа. Дараа дахин оролдоно уу"],
    ["下载失败：连接 arXiv 超时，请稍后再试", "Download failed: the connection to arXiv timed out. Try again later", "Татаж чадсангүй: arXiv-тай холболт хугацаа хэтрэв. Дараа дахин оролдоно уу"],
    ["下载中…", "Downloading…", "Татаж байна…"],
    ["显示密码", "Show password", "Нууц үгийг харуулах"],
    ["向右移动文件标签", "Move the file tab right", "Файлын хавтсыг баруун тийш зөөх"],
    ["向左移动文件标签", "Move the file tab left", "Файлын хавтсыг зүүн тийш зөөх"],
    ["写入 PDF 失败", "Could not write the PDF", "PDF бичиж чадсангүй"],
    ["新群组名称", "New group name", "Шинэ бүлгийн нэр"],
    ["新主持人的邮箱或手机号", "New host email or phone", "Шинэ хөтлөгчийн имэйл эсвэл утас"],
    ["星", "Star", "Од"],
    ["修改分享", "Edit share", "Хуваалцалтыг засах"],
    ["修改分享 — 调整分享范围、有效期，链接不变", "Edit share — change who can see it and when it expires. The link stays the same", "Хуваалцалтыг засах — хүрээ, хугацааг өөрчилнө. Холбоос хэвээр"],
    ["修改群组名称", "Rename group", "Бүлгийн нэрийг солих"],
    ["需要分享密码", "A share password is required", "Хуваалцах нууц үг хэрэгтэй"],
    ["需要先在主窗口登录，才能上传到云盘", "Sign in from the main window before uploading to the cloud drive", "Үүлэн диск рүү байршуулахаас өмнө үндсэн цонхонд нэвтэрнэ үү"],
    ["选择群组", "Choose a group", "Бүлэг сонгох"],
    ["研究目标、关键字，或论文编号", "Research goal, keywords, or a paper id", "Судалгааны зорилго, түлхүүр үг, эсвэл өгүүллийн дугаар"],
    ["研究目标、关键字或论文编号", "Research goal, keywords, or a paper id", "Судалгааны зорилго, түлхүүр үг, өгүүллийн дугаар"],
    ["邀请的邮箱或手机号", "Invited email or phone", "Урьсан имэйл эсвэл утас"],
    ["邀请邮箱或手机号", "Email or phone to invite", "Урих имэйл эсвэл утас"],
    ["叶", "Leaf", "Навч"],
    ["移除", "Remove", "Хасах"],
    ["移除文档", "Remove document", "Баримтыг хасах"],
    ["移除已过期的分享", "Remove the expired share", "Хугацаа дууссан хуваалцалтыг хасах"],
    ["已按原版式生成新的 PDF，原文件没有改动。", "A new PDF was made in the original layout. The source file was not changed.", "Анхны бүтэцтэй шинэ PDF үүслээ. Эх файл өөрчлөгдөөгүй."],
    ["已按原文检索", "Searched with the original text", "Эх текстээр хайлаа"],
    ["已创建群组", "Group created", "Бүлэг үүслээ"],
    ["已从文档管理移除，源文件仍在", "Removed from the library. The source file is still there", "Баримтын сангаас хаслаа. Эх файл хэвээр"],
    ["已导出", "Exported", "Экспортлов"],
    ["已导出 LaTeX", "Exported LaTeX", "LaTeX экспортлов"],
    ["已导出 PDF", "Exported PDF", "PDF экспортлов"],
    ["已导出项目", "Exported the project", "Төслийг экспортлов"],
    ["已导入文档库", "Imported into the library", "Баримтын санд орууллаа"],
    ["已发给主持人 Agent，回复会作为批注同步给参与者", "Sent to the host agent. The reply will sync to participants as a comment", "Хөтлөгчийн агент руу илгээлээ. Хариу нь оролцогчдод тэмдэглэл болон синк хийгдэнэ"],
    ["已发给主持人审核，通过后由主持人 Agent 回复", "Sent to the host for review. After it is accepted, the host agent replies", "Хөтлөгчид шалгуулахаар илгээлээ. Зөвшөөрсний дараа хөтлөгчийн агент хариулна"],
    ["已发送入群邀请", "Group invitation sent", "Бүлэгт урих илгээлээ"],
    ["已发送主持人转移邀请，等待对方接受", "Host transfer invitation sent. Waiting for them to accept", "Хөтлөгч шилжүүлэх урилга илгээлээ. Зөвшөөрөхийг хүлээж байна"],
    ["已分享", "Shared", "Хуваалцсан"],
    ["已分享 — 到云端项目里可以修改分享范围和有效期", "Shared — open the cloud project to change who can see it and when it expires", "Хуваалцсан — үүлэн төсөл дээр хүрээ, хугацааг засна"],
    ["已分享 — 链接可以修改分享范围、有效期、主持人", "Shared — the link can change the audience, expiry, and host", "Хуваалцсан — холбоосоор хүрээ, хугацаа, хөтлөгчийг засна"],
    ["已复制", "Copied", "Хууллаа"],
    ["已复制分享链接", "Share link copied", "Хуваалцах холбоосыг хууллаа"],
    ["已更新分享", "Share updated", "Хуваалцалтыг шинэчиллээ"],
    ["已更新群组", "Group updated", "Бүлгийг шинэчиллээ"],
    ["已过期", "Expired", "Хугацаа дууссан"],
    ["已忽略", "Ignored", "Үл хэрэгссэн"],
    ["已忽略这条批注，参与者会看到忽略标记", "This comment was ignored. Participants will see that mark", "Энэ тэмдэглэлийг үл хэрэгслээ. Оролцогчид тэр тэмдгийг харна"],
    ["已加入", "Joined", "Нэгдлээ"],
    ["已解散群组", "Group deleted", "Бүлгийг тараалаа"],
    ["已进入桌面知识库", "Added to the desktop knowledge base", "Ширээний мэдлэгийн санд орлоо"],
    ["已取消分享，参与者会移除并关闭该文档", "Sharing stopped. Participants lose it and the document closes", "Хуваалцалтыг цуцаллаа. Оролцогчдоос хасагдаж, баримт хаагдана"],
    ["已取消主持人转移", "Host transfer cancelled", "Хөтлөгч шилжүүлэхийг цуцаллаа"],
    ["已删除伴读数据，源文件仍在", "Companion data deleted. The source file is still there", "Дагалдах өгөгдлийг устгалаа. Эх файл хэвээр"],
    ["已删除分享", "Share deleted", "Хуваалцалтыг устгалаа"],
    ["已上传到云盘", "Uploaded to the cloud drive", "Үүлэн диск рүү байршууллаа"],
    ["已上传到云盘 — 手机端「文档」可打开", "Uploaded to the cloud drive — open it from Documents on the phone", "Үүлэн диск рүү байршууллаа — утасны «Баримт»-аас нээнэ"],
    ["已上传到云盘，手机端「文档」可打开", "Uploaded to the cloud drive. Open it from Documents on the phone", "Үүлэн диск рүү байршууллаа. Утасны «Баримт»-аас нээнэ үү"],
    ["已生成论文解读", "Paper briefing is ready", "Өгүүллийн тайлбар бэлэн"],
    ["已收回主持权", "Host role taken back", "Хөтлөгчийн эрхээ авлаа"],
    ["已通过，等待主持人 Agent 回复", "Accepted. Waiting for the host agent to reply", "Зөвшөөрлөө. Хөтлөгчийн агентын хариуг хүлээж байна"],
    ["已通过，主持人 Agent 会处理这条批注", "Accepted. The host agent will handle this comment", "Зөвшөөрлөө. Хөтлөгчийн агент энэ тэмдэглэлийг хариуцна"],
    ["已同步到云端", "Synced to the cloud", "Үүлэн рүү синк хийлээ"],
    ["已退出群组", "Left the group", "Бүлгээс гарлаа"],
    ["已移出群组", "Removed from the group", "Бүлгээс хаслаа"],
    ["隐藏", "Hide", "Нуух"],
    ["隐藏窗口，已打开的文件会保留", "Hide the window. Open files stay open", "Цонхыг нууна. Нээлттэй файлууд хадгалагдана"],
    ["隐藏密码", "Hide password", "Нууц үгийг нуух"],
    ["永久", "Never expires", "Хугацаагүй"],
    ["邮箱或手机号", "Email or phone", "Имэйл эсвэл утас"],
    ["有效期", "Expires", "Хүчинтэй хугацаа"],
    ["预览", "Preview", "Урьдчилан харах"],
    ["预览还没打开，无法跳转", "The preview is not open, so it cannot jump", "Урьдчилсан харагдац нээгдээгүй тул үсэрч чадахгүй"],
    ["源码", "Source", "Эх код"],
    ["约 N 篇", "About N papers", "Ойролцоогоор N өгүүлэл"],
    ["阅读群组", "Reading groups", "Уншлагын бүлэг"],
    ["阅读群组 — 创建群组、邀请成员、分享给群组", "Reading groups — create a group, invite people, and share with the group", "Уншлагын бүлэг — бүлэг үүсгэж, гишүүн урьж, бүлэгт хуваалцана"],
    ["云端", "Cloud", "Үүл"],
    ["云端项目", "Cloud projects", "Үүлэн төслүүд"],
    ["再次确认覆盖", "Confirm overwrite again", "Дахин дарж хадгалахыг баталгаажуулах"],
    ["摘录被截断，已拒绝写回", "The excerpt was truncated, so it was not written back", "Ишлэл тасарсан тул буцааж бичсэнгүй"],
    ["粘贴分享链接", "Paste a share link", "Хуваалцах холбоос буулгах"],
    ["找不到文件", "File not found", "Файл олдсонгүй"],
    ["这份 PDF 已有文字层，可以直接选择和搜索。", "This PDF already has a text layer. You can select and search it.", "Энэ PDF-д текст давхарга аль хэдийн бий. Шууд сонгож хайж болно."],
    ["这份文档由对方分享，对方是主持人", "Someone else shared this document and is the host", "Энэ баримтыг өөр хүн хуваалцсан бөгөөд тэр хөтлөгч"],
    ["这个视频还没有笔记。点「生成笔记」，或发送「生成笔记」。", "This video has no notes yet. Choose Generate notes, or send Generate notes.", "Энэ видеонд тэмдэглэл алга. «Тэмдэглэл үүсгэх»-ийг дарах, эсвэл «Тэмдэглэл үүсгэх» гэж илгээнэ үү."],
    ["这个文件没有打开", "This file is not open", "Энэ файл нээгдээгүй"],
    ["这个文件不是论文，不能做论文解读", "This file is not a paper, so it cannot be briefed", "Энэ файл өгүүлэл биш тул тайлбар хийх боломжгүй"],
    ["这个项目", "This project", "Энэ төсөл"],
    ["这个页面没有打开", "This page is not open", "Энэ хуудас нээгдээгүй"],
    ["这个音频还没有笔记。点「生成笔记」，或发送「生成笔记」。", "This audio has no notes yet. Choose Generate notes, or send Generate notes.", "Энэ аудиод тэмдэглэл алга. «Тэмдэглэл үүсгэх»-ийг дарах, эсвэл «Тэмдэглэл үүсгэх» гэж илгээнэ үү."],
    ["这篇文档是什么？", "What is this document?", "Энэ баримт юу вэ?"],
    ["这些文章已经取消分享", "These articles are no longer shared", "Эдгээр материалын хуваалцалтыг цуцалсан"],
    ["正在保存…", "Saving…", "Хадгалж байна…"],
    ["正在导出…", "Exporting…", "Экспортлож байна…"],
    ["正在导入…", "Importing…", "Импортлож байна…"],
    ["正在分享…", "Sharing…", "Хуваалцаж байна…"],
    ["正在检索…", "Searching…", "Хайж байна…"],
    ["正在解读…", "Reading the paper…", "Өгүүллийг тайлбарлаж байна…"],
    ["正在理解研究目标并检索…", "Reading the research goal and searching…", "Судалгааны зорилгыг уншиж, хайж байна…"],
    ["正在上传…", "Uploading…", "Байршуулж байна…"],
    ["正在生成论文解读…", "Writing the paper briefing…", "Өгүүллийн тайлбарыг бичиж байна…"],
    ["正在思考", "Thinking", "Бодож байна"],
    ["正在转换", "Converting", "Хөрвүүлж байна"],
    ["正在转换，请稍候。", "Converting. Please wait.", "Хөрвүүлж байна. Түр хүлээнэ үү."],
    ["正在转换…", "Converting…", "Хөрвүүлж байна…"],
    ["指定的人", "Specific people", "Тодорхой хүмүүс"],
    ["主持人", "Host", "Хөтлөгч"],
    ["主持人已忽略这条批注", "The host ignored this comment", "Хөтлөгч энэ тэмдэглэлийг үл хэрэгссэн"],
    ["转换", "Convert", "Хөрвүүлэх"],
    ["转换 — 保留扫描版式，生成可选中、可搜索的 PDF", "Convert — keep the scanned layout and make a PDF you can select and search", "Хөрвүүлэх — сканнердсан бүтцийг хадгалж, сонгож хайж болох PDF үүсгэнэ"],
    ["自动同步到分享失败，可在文档管理里用「同步」重试", "Syncing to the share failed. Use Sync in the library to try again", "Хуваалцалт руу автоматаар синк хийж чадсангүй. Баримтын сан дахь «Синк»-ээр дахин оролдоно уу"],
    ["画面", "Picture", "Дүрс"],
    ["声音", "Sound", "Дуу"],
    ["检查", "Check", "Шалгалт"],
    ["音轨", "Audio track", "Дууны зам"],
    ["语音", "Speech", "Яриа"],
    ["整理", "Write-up", "Эмхлэлт"],
    ["正在记录画面", "Recording the picture", "Дүрс бичиж байна"],
    ["正在记录声音", "Recording the sound", "Дуу бичиж байна"],
    ["正在检查文件", "Checking the file", "Файлыг шалгаж байна"],
    ["正在读取字幕", "Reading subtitles", "Хадмал уншиж байна"],
    ["正在安装视频解码器", "Installing the video decoder", "Видео декодер суулгаж байна"],
    ["正在提取音轨", "Extracting the audio track", "Дууны замыг гаргаж байна"],
    ["正在抽取画面", "Extracting frames", "Кадр салгаж байна"],
    ["正在识别语音", "Recognizing speech", "Яриа таньж байна"],
    ["正在识别画面文字", "Reading text in the picture", "Дүрс дээрх бичгийг таньж байна"],
    ["正在整理笔记", "Writing the notes", "Тэмдэглэлийг эмхэлж байна"],
    ["云端分享失败", "Cloud share failed", "Үүлэн хуваалцалт амжилтгүй"],
    ["云端还没有文档分享，需要更新 Hub", "This Hub does not have document sharing yet. Update Hub", "Энэ Hub дээр баримт хуваалцах хараахан алга. Hub-аа шинэчилнэ үү"],
    ["请先登录 MaClaw Hub", "Sign in to MaClaw Hub first", "Эхлээд MaClaw Hub-д нэвтэрнэ үү"],
    ["伴读", "Companion", "Дагалдан унших"],
    ["Enter 发送，Shift+Enter 换行，↑↓ 翻看已发送的内容，/clear 清空", "Enter sends, Shift+Enter inserts a line, Up/Down walks sent text, /clear clears", "Enter илгээнэ, Shift+Enter шинэ мөр, ↑↓ илгээснийг гүйлгэнэ, /clear цэвэрлэнэ"],
    ["你的讨论会成为批注，同步给能看到这份分享的人。", "Your discussion becomes a comment and syncs to people who can see this share.", "Таны хэлэлцүүлэг тэмдэглэл болж, энэ хуваалцалтыг харж чадах хүмүүст синк хийгдэнэ."],
    ["保存", "Save", "Хадгалах"],
    ["查看文档", "View document", "Баримтыг харах"],
    ["产出物", "Artifacts", "Бүтээл"],
    ["已保存", "Saved", "Хадгаллаа"],
    ["保存中...", "Saving...", "Хадгалж байна..."],
    ["关键帧", "Keyframes", "Гол кадр"],
    ["原文配图", "Source figures", "Эх зураг"],
    ["无法取图", "Could not load the image", "Зургийг авч чадсангүй"],
    ["正在取图…", "Loading image…", "Зургийг авч байна…"],
    ["上传失败", "Upload failed", "Байршуулалт амжилтгүй"],
    ["已过期 · ${date}", "Expired · ${date}", "Хугацаа дууссан · ${date}"],
    ["至 ${date}", "Until ${date}", "${date} хүртэл"],
    ["分享于 ${date}", "Shared on ${date}", "${date}-нд хуваалцсан"],
    ["打开于 ${date}", "Opened on ${date}", "${date}-нд нээсэн"],
    ["当前主持人：${host}", "Current host: ${host}", "Одоогийн хөтлөгч: ${host}"],
    ["主持人：${owner}", "Host: ${owner}", "Хөтлөгч: ${owner}"],
    ["导入知识库失败：${reason}", "Could not import into the knowledge base: ${reason}", "Мэдлэгийн санд оруулж чадсангүй: ${reason}"],
    ["下载失败：${msg}", "Download failed: ${msg}", "Татаж чадсангүй: ${msg}"],
    ["云端分享失败（HTTP ${status}）", "Cloud share failed (HTTP ${status})", "Үүлэн хуваалцалт амжилтгүй (HTTP ${status})"],
    ["确认删除“${name}”？\n源文件会保留。本机的高亮、讨论和阅读记录会被清除。\n此操作不可撤销。", "Delete “${name}”?\nThe source file stays. Highlights, discussion, and reading history on this computer will be cleared.\nThis cannot be undone.", "“${name}”-г устгах уу?\nЭх файл үлдэнэ. Энэ компьютер дээрх тодруулга, хэлэлцүүлэг, уншсан түүх арилна.\nҮүнийг буцаах боломжгүй."],
    ["确认取消分享“${name}”？\n所有参与者会从文档管理移除，并在伴读中关闭该文档。\n本机的文档、高亮、批注和手绘会保留。\n此操作不可撤销。", "Stop sharing “${name}”?\nEvery participant loses it from the library, and the document closes in the companion.\nThe document, highlights, comments, and drawings on this computer stay.\nThis cannot be undone.", "“${name}”-н хуваалцалтыг цуцлах уу?\nБүх оролцогч баримтын сангаас хасагдаж, дагалдан унших цонхонд баримт хаагдана.\nЭнэ компьютер дээрх баримт, тодруулга, тэмдэглэл, зураг үлдэнэ.\nҮүнийг буцаах боломжгүй."],
    ["确认从文档管理移除“${name}”？\n源文件会保留；这份分享已过期。\n本机的高亮、讨论和阅读记录会被清除。\n此操作不可撤销。", "Remove “${name}” from the library?\nThe source file stays. This share has expired.\nHighlights, discussion, and reading history on this computer will be cleared.\nThis cannot be undone.", "“${name}”-г баримтын сангаас хасах уу?\nЭх файл үлдэнэ. Энэ хуваалцлын хугацаа дууссан.\nЭнэ компьютер дээрх тодруулга, хэлэлцүүлэг, уншсан түүх арилна.\nҮүнийг буцаах боломжгүй."],
    ["确认从文档管理移除“${name}”？\n源文件会保留；这份分享仍属于分享者。\n本机的高亮、讨论和阅读记录会被清除。\n此操作不可撤销。", "Remove “${name}” from the library?\nThe source file stays. This share still belongs to the person who shared it.\nHighlights, discussion, and reading history on this computer will be cleared.\nThis cannot be undone.", "“${name}”-г баримтын сангаас хасах уу?\nЭх файл үлдэнэ. Энэ хуваалцалт эзэндээ үлдэнэ.\nЭнэ компьютер дээрх тодруулга, хэлэлцүүлэг, уншсан түүх арилна.\nҮүнийг буцаах боломжгүй."],
    ["确认删除“${name}”的分享？\n链接会立即失效，其他人将无法再打开。\n源文件仍会保留。", "Delete the share for “${name}”?\nThe link stops working immediately, and other people can no longer open it.\nThe source file stays.", "“${name}”-н хуваалцалтыг устгах уу?\nХолбоос даруй хүчингүй болж, бусад хүн дахин нээж чадахгүй.\nЭх файл үлдэнэ."],
    ["已分享 — ${range}，${when}", "Shared — ${range}, ${when}", "Хуваалцсан — ${range}, ${when}"],
    ["收到的分享 — ${range}，${when}", "Received share — ${range}, ${when}", "Ирсэн хуваалцалт — ${range}, ${when}"],
    ["${n} 张配图", "${n} figures", "${n} зураг"],
    ["${n} 张流程图", "${n} diagrams", "${n} диаграмм"],
    ["已导出 PDF，但 ${missing}未能读取，未包含在内", "Exported PDF, but ${missing} could not be read and were left out", "PDF экспортлов, гэхдээ ${missing} уншигдаагүй тул ороогүй"],
    ["${base}，但 ${missing}未能读取，未包含在内", "${base}, but ${missing} could not be read and were left out", "${base}, гэхдээ ${missing} уншигдаагүй тул ороогүй"],
    ["云盘中已有相同内容${held}，未再次上传", "The cloud drive already has the same content${held}, so it was not uploaded again", "Үүлэн диск дээр ижил агуулга ${held} аль хэдийн бий тул дахин байршуулсангүй"],
    ["（${held}）", " (${held})", " (${held})"],
    ["约 ${total} 篇，第 ${page} / ${pages} 页", "About ${total} papers, page ${page} / ${pages}", "Ойролцоогоор ${total} өгүүлэл, ${page} / ${pages}-р хуудас"],
    ["${head}约 ${total} 篇，第 ${page} / ${pages} 页", "${head}About ${total} papers, page ${page} / ${pages}", "${head}Ойролцоогоор ${total} өгүүлэл, ${page} / ${pages}-р хуудас"],
    ["${head}检索完成，没有结果", "${head}Search finished, with no results", "${head}Хайлт дууссан, үр дүн алга"],
    ["${head}这一页没有论文", "${head}This page has no papers", "${head}Энэ хуудсанд өгүүлэл алга"],
    ["这一页没有论文", "This page has no papers", "Энэ хуудсанд өгүүлэл алга"],
    ["已处理 ${done}/${total} 页", "Processed ${done}/${total} pages", "${done}/${total} хуудсыг боловсрууллаа"],
    ["关闭 ${name}", "Close ${name}", "${name}-г хаах"],
    ["打开 ${name}", "Open ${name}", "${name}-г нээх"],
    ["导入 ${name} 到本机", "Import ${name} onto this computer", "${name}-г энэ компьютерт оруулах"],
    ["移除 ${name}", "Remove ${name}", "${name}-г хасах"],
    ["${brand}伴读", "${brand} companion", "${brand} дагалдан унших"],
    ["批注 · 待审核 ${count}", "Comments · ${count} waiting for review", "Тэмдэглэл · ${count} шалгалт хүлээж байна"],
    ["批注", "Comments", "Тэмдэглэл"],
    ["每个项目包含文档、高亮和讨论，共 ${count} 个", "Each project holds the document, highlights, and discussion. ${count} in all", "Төсөл бүр баримт, тодруулга, хэлэлцүүлэг агуулна. Нийт ${count}"],
    ["每个项目包含文档、高亮和讨论，还没有项目", "Each project holds the document, highlights, and discussion. No projects yet", "Төсөл бүр баримт, тодруулга, хэлэлцүүлэг агуулна. Төсөл хараахан алга"],
    ["当前 ${audience} · 至 ${date}", "Now ${audience} · until ${date}", "Одоо ${audience} · ${date} хүртэл"],
    ["当前 ${audience} · 永久", "Now ${audience} · never expires", "Одоо ${audience} · хугацаагүй"],
    ["请联系主持人 ${names}", "Contact the host ${names}", "Хөтлөгч ${names}-тай холбогдоно уу"],
    ["${withdrawn}，如果需要继续阅读，${ask}", "${withdrawn}. To keep reading, ${ask}", "${withdrawn}. Үргэлжлүүлэн уншихын тулд ${ask}"],
    ["确认取消提名 ${name} 接任主持人？", "Cancel the nomination of ${name} as the next host?", "${name}-г дараагийн хөтлөгчөөр нэр дэвшүүлснийг цуцлах уу?"],
    ["主持人 · ${author}", "Host · ${author}", "Хөтлөгч · ${author}"],
    ["成员 · ${owner}", "Member · ${owner}", "Гишүүн · ${owner}"],
    [" · ${count} 人", " · ${count} people", " · ${count} хүн"],
    ["正在识别语音（第 ${index}/${total} 段）", "Recognizing speech (part ${index}/${total})", "Яриа таньж байна (${index}/${total} хэсэг)"],
    ["主持", "Host mark", "Хөтлөгч"],
    ["打开", "Open", "Нээх"],
    ["打开文档或导入项目", "Open a document or import a project", "Баримт нээх эсвэл төсөл оруулах"],
    ["支持一次选择多个文件", "You can choose more than one file", "Нэгээс олон файл сонгож болно"],
    ["搜索", "Find", "Хайх"],
    ["导出文件", "Export file", "Файл экспортлох"],
    ["导出到云盘", "Export to the cloud drive", "Үүлэн диск рүү экспортлох"],
    ["把文件拖到这里，或点击「打开」", "Drop a file here, or choose Open", "Файлаа энд чирж тавина уу, эсвэл «Нээх»-ийг дарна уу"],
    ["文件过大，为避免截断写回，已禁止保存", "This file is too large to save without truncating it, so saving is off", "Файл хэт том тул тасалдуулахгүйгээр хадгалахгүй"],
    ["此文件类型暂不支持预览", "This file type cannot be previewed yet", "Энэ төрлийн файлыг урьдчилан харах боломжгүй"],
    ["${size} 字节", "${size} bytes", "${size} байт"],
    ["重新加载", "Reload", "Дахин ачаалах"],
    ["针对当前文件提问", "Ask about this file", "Энэ файлын талаар асууна уу"],
    ["主持人回复", "Host reply", "Хөтлөгчийн хариу"],
    ["待审核", "Waiting for review", "Шалгалт хүлээж байна"],
    ["通过", "Accept", "Зөвшөөрөх"],
    ["忽略", "Ignore", "Үл хэрэгсэх"],
    ["改为通过", "Change to accepted", "Зөвшөөрсөн болгох"],
    ["改为忽略", "Change to ignored", "Үл хэрэгссэн болгох"],
    ["输入分享密码", "Enter the share password", "Хуваалцах нууц үг оруулах"],
    ["该文档为私密分享，需要密码才能导入。", "This document is a private share. A password is required to import it.", "Энэ баримт нууц хуваалцалт. Оруулахад нууц үг хэрэгтэй."],
    ["arXiv 文献检索", "arXiv search", "arXiv хайлт"],
    ["描述研究目标，检索公开预印本", "Describe a research goal and search public preprints", "Судалгааны зорилгоо бичиж, нээлттэй препринтоос хайна уу"],
    ["没有与「${query}」匹配的论文", "No papers match “${query}”", "“${query}”-д таарах өгүүлэл алга"],
    ["换一种说法描述研究目标，或在上方输入 arXiv 编号（如 2510.16079）后检索。", "Describe the goal another way, or enter an arXiv id (for example 2510.16079) above and search.", "Зорилгоо өөрөөр бичнэ үү, эсвэл дээр arXiv дугаар (жишээ нь 2510.16079) оруулаад хайна уу."],
    ["回到上一页继续查看。", "Go back to the previous page.", "Өмнөх хуудас руу буцаж үргэлжлүүлнэ үү."],
    ["上一页", "Previous page", "Өмнөх хуудас"],
    ["下一页", "Next page", "Дараагийн хуудас"],
    ["打开中", "Currently open", "Нээлттэй"],
    ["未导入", "Not imported", "Оруулаагүй"],
    ["标注 ${count}", "Marks ${count}", "Тэмдэглэгээ ${count}"],
    ["讨论 ${count}", "Discussion ${count}", "Хэлэлцүүлэг ${count}"],
    ["待审核 ${count}", "Review waiting ${count}", "Шалгалт хүлээж байна ${count}"],
    ["导入分享", "Import a share", "Хуваалцалт оруулах"],
    ["正在读取…", "Loading…", "Уншиж байна…"],
    ["还没有本机项目 — 打开或导入一个文档开始", "No local projects yet — open or import a document to start", "Энэ компьютерт төсөл алга — баримт нээж эсвэл оруулж эхлэнэ үү"],
    ["还没有云端项目 — 分享后会出现在这里", "No cloud projects yet — they show up here after you share", "Үүлэн төсөл алга — хуваалцсаны дараа энд гарна"],
    ["用邮箱或手机号邀请，对方在主界面通知里同意后加入。", "Invite by email or phone. They join after accepting the notice in the main window.", "Имэйл эсвэл утсаар урина. Үндсэн цонхны мэдэгдлийг зөвшөөрснөөр нэгдэнэ."],
    ["创建", "Create", "Үүсгэх"],
    ["还没有群组", "No groups yet", "Бүлэг хараахан алга"],
    ["创建后可以邀请同伴一起读同一份文档。", "After you create a group, you can invite people to read the same document.", "Бүлэг үүсгэсний дараа нэг баримтыг хамт унших хүн урьж болно."],
    ["选择左侧的群组查看设置", "Choose a group on the left to see its settings", "Зүүн талын бүлгийг сонгож тохиргоог нь харна уу"],
    ["群组名称", "Group name", "Бүлгийн нэр"],
    ["邀请", "Invite", "Урих"],
    ["还没有成员。邀请后对方会收到通知。", "No members yet. After you invite someone, they get a notice.", "Гишүүн алга. Урьсны дараа мэдэгдэл очно."],
    ["解散群组", "Delete the group", "Бүлгийг тараах"],
    ["成员会失去这份分享的访问权限，文档本身不受影响。", "Members lose access to this share. The document itself stays.", "Гишүүд энэ хуваалцалтыг харах эрхээ алдана. Баримт өөрөө хэвээр."],
    ["确认解散", "Confirm delete", "Тарахыг баталгаажуулах"],
    ["退出群组", "Leave the group", "Бүлгээс гарах"],
    ["退出后不再看到这个群组分享的文档。", "After you leave, you no longer see documents this group shared.", "Гарсны дараа энэ бүлгийн хуваалцсан баримт харагдахгүй."],
    ["确认退出", "Confirm leave", "Гарахыг баталгаажуулах"],
    ["完全公开", "Fully public", "Бүрэн нээлттэй"],
    ["私密分享", "Private share", "Нууц хуваалцалт"],
    ["几天", "A number of days", "Хэдэн өдөр"],
    ["几月", "A number of months", "Хэдэн сар"],
    ["已提名 ${name}，等待对方在通知里接受。", "${name} is nominated. Waiting for them to accept the notice.", "${name}-г нэр дэвшүүллээ. Мэдэгдлийг зөвшөөрөхийг хүлээж байна."],
    ["转移主持权", "Transfer the host role", "Хөтлөгчийн эрх шилжүүлэх"],
    ["对方接受后才转移；你保留管理权，可随时收回或另行指定。", "It transfers only after they accept. You keep admin rights and can take them back or name someone else.", "Зөвшөөрсний дараа л шилжинэ. Та удирдах эрхээ хадгалж, буцааж авах эсвэл өөр хүн томилох боломжтой."],
    ["复制", "Copy", "Хуулах"],
    ["同步更新", "Sync updates", "Шинэчлэл синк хийх"],
    ["导入后文档、高亮和聊天一起存进本机，成为一个可继续阅读的项目。", "After import, the document, highlights, and chat stay on this computer as a project you can keep reading.", "Оруулсны дараа баримт, тодруулга, чат энэ компьютерт төсөл болон үлдэж, үргэлжлүүлэн уншина."],
    ["可选", "Optional", "Заавал биш"],
    ["密码", "Password", "Нууц үг"],
    ["修改", "Edit", "Засах"],
    ["如 2510.16079、arxiv.org/abs/…、视频页面，或伴读分享链接", "For example 2510.16079, arxiv.org/abs/…, a video page, or a companion share link", "Жишээ нь 2510.16079, arxiv.org/abs/…, видео хуудас, эсвэл дагалдан унших холбоос"],
    [" · 待审核 ${count}", " · ${count} waiting for review", " · ${count} шалгалт хүлээж байна"],
    ["${count} 人", "${count} people", "${count} хүн"],
    ["回复 ${name}：${question}", "Reply to ${name}: ${question}", "${name}-д хариу: ${question}"],
    ["黄色", "Yellow", "Шар"],
    ["绿色", "Green", "Ногоон"],
    ["蓝色", "Blue", "Цэнхэр"],
    ["粉色", "Pink", "Ягаан"],
    ["高亮", "Highlight", "Тодруулах"],
    ["取消高亮", "Clear highlight", "Тодруулгыг арилгах"],
    ["选中文本操作", "Selection actions", "Сонгосон текстийн үйлдэл"],
    ["高亮颜色", "Highlight color", "Тодруулгын өнгө"],
    ["${color}高亮", "${color} highlight", "${color} тодруулга"],
    ["正在下载…", "Downloading…", "Татаж байна…"],
    ["还没有管理的群组，先在「阅读群组」里创建一个。", "You do not manage a group yet. Create one under Reading groups.", "Таны удирдах бүлэг алга. «Унших бүлэг» дотор нэгийг үүсгэнэ үү."],
];

const byZh = new Map<string, { en: string; mn: string }>();
const byEn = new Map<string, string>();
const templates: { zh: string; en: string; mn: string; re: RegExp }[] = [];

function escapeRegExp(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

for (const [zh, en, mn] of ROWS) {
    if (!byZh.has(zh)) byZh.set(zh, { en, mn });
    if (!byEn.has(en)) byEn.set(en, mn);
    if (zh.includes("${")) {
        const body = zh.split(/\$\{[^}]+\}/).map(escapeRegExp).join("([\\s\\S]*?)");
        templates.push({ zh, en, mn, re: new RegExp(`^${body}$`) });
    }
}
templates.sort((a, b) => b.zh.length - a.zh.length);

let activeLang = "zh-Hans";

export function setCompanionUiLang(lang: string | undefined | null): void {
    activeLang = normalizeLang(lang);
}

export function getCompanionUiLang(): string {
    return activeLang;
}

export function companionText(lang: string | undefined | null, en: string, zhHans: string, zhHant?: string): string {
    const kind = normalizeLang(lang);
    if (kind === "zh-Hans") return zhHans;
    if (kind === "zh-Hant") return zhHant || zhHans;
    if (kind === "mn") return byEn.get(en) || en;
    return en;
}

function fillFrom(pattern: string, values: string[]): string {
    let index = 0;
    return pattern.replace(/\$\{[^}]+\}/g, () => values[index++] ?? "");
}

export function companionSay(lang: string | undefined | null, zh: string): string {
    const kind = normalizeLang(lang);
    const exact = byZh.get(zh);
    if (exact) return companionText(lang, exact.en, zh);
    for (const template of templates) {
        const match = template.re.exec(zh);
        if (!match) continue;
        const pattern = companionText(lang, template.en, template.zh);
        return fillFrom(pattern, match.slice(1));
    }
    if (kind === "zh-Hans" || kind === "zh-Hant") return zh;
    if (/[\u3400-\u9fff]/.test(zh)) return zh;
    return zh;
}

/** Display helper. Reads the language last set by the companion window. */
export function cs(zh: string): string {
    return companionSay(activeLang, zh);
}

export function companionFill(lang: string | undefined | null, zhTemplate: string, vars: Record<string, string | number>): string {
    const row = byZh.get(zhTemplate);
    const pattern = row ? companionText(lang, row.en, zhTemplate) : companionSay(lang, zhTemplate);
    return pattern.replace(/\$\{([^}]+)\}/g, (_, name: string) => String(vars[name] ?? ""));
}

const NOTE_STEP: Record<string, readonly [string, string, string]> = {
    watch: ["Picture", "画面", "Дүрс"],
    listen: ["Sound", "声音", "Дуу"],
    check: ["Check", "检查", "Шалгалт"],
    audio: ["Audio track", "音轨", "Дууны зам"],
    frames: ["Frames", "画面", "Кадр"],
    speech: ["Speech", "语音", "Яриа"],
    write: ["Write-up", "整理", "Эмхлэлт"],
};

export const COMPANION_NOTE_PROGRESS_ZH = [
    "正在记录画面",
    "正在记录声音",
    "正在检查文件",
    "正在读取字幕",
    "正在安装视频解码器",
    "正在提取音轨",
    "正在抽取画面",
    "正在识别语音",
    "正在识别画面文字",
    "正在整理笔记",
] as const;

const SEGMENT = /^正在识别语音（第 (\d+)\/(\d+) 段）$/;

export function companionNoteTitle(lang: string | undefined | null, zh: string): string {
    const text = zh.trim();
    const segment = SEGMENT.exec(text);
    if (segment) {
        return companionFill(lang, "正在识别语音（第 ${index}/${total} 段）", { index: segment[1], total: segment[2] });
    }
    return companionSay(lang, text);
}

export function companionNoteStepLabel(lang: string | undefined | null, id: string): string {
    const row = NOTE_STEP[id];
    if (!row) return id;
    const kind = normalizeLang(lang);
    if (kind === "zh-Hans" || kind === "zh-Hant") return row[1];
    if (kind === "mn") return row[2];
    return row[0];
}

const NOTE_ALIAS = new Set([
    "generate notes",
    "take notes",
    "make notes",
    "make a note",
    "create a note",
    "тэмдэглэл үүсгэх",
    "тэмдэглэл хийх",
    "тэмдэглэл бичих",
]);

const PAPER_ALIAS = new Set([
    "read the paper",
    "paper briefing",
    "өгүүллийн тайлбар",
    "өгүүллийг тайлбарлах",
]);

function commandKey(text: string): string {
    return text.trim().replace(/[。.!！?？]+$/u, "").toLowerCase();
}

/** English and Khalkha sentences that enter the existing note path. */
export function companionNoteAlias(text: string): boolean {
    return NOTE_ALIAS.has(commandKey(text));
}

/** English and Khalkha sentences that enter the existing paper path. Chinese 论文解读 stays a typed question unless the button sets purpose. */
export function companionPaperAlias(text: string): boolean {
    return PAPER_ALIAS.has(commandKey(text));
}

const COMMAND_BUBBLES = new Set<string>([
    "论文解读",
    "记笔记",
    "生成笔记",
    "已生成论文解读",
    "正在生成论文解读…",
    "这个音频还没有笔记。点「生成笔记」，或发送「生成笔记」。",
    "这个视频还没有笔记。点「生成笔记」，或发送「生成笔记」。",
]);

/** Stored command labels stay Chinese. The bubble shows the interface language. */
export function companionCommandBubble(lang: string | undefined | null, text: string): string {
    return COMMAND_BUBBLES.has(text) ? companionSay(lang, text) : text;
}
