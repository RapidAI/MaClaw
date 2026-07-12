# 知识库图片支持 — 需求文档

## 背景

maclaw 的知识库（`corelib/knowledge/`）当前只处理纯文本内容。导入 .docx/.pptx/.pdf 等文档时，内嵌图片被丢弃；独立图片文件在目录导入时被跳过。用户无法通过知识库管理和检索图片资产。

## 功能需求

### FR-1: 文档内嵌图片提取

- FR-1.1: DOCX 导入时，从 `word/media/` 提取所有嵌入图片，关联到段落级位置（前后文本上下文）
- FR-1.2: PPTX 导入时，从 `ppt/media/` 提取所有嵌入图片，关联到幻灯片级位置（幻灯片标题+编号）
- FR-1.3: PDF 导入时，提取嵌入图片（XObject Image），关联到页面级位置（页码+同页文本）
- FR-1.4: .doc（OLE2 格式）导入时，提取 Pictures stream 中的图片，关联到文档级（P1 阶段）
- FR-1.5: 提取的内嵌图片格式支持：PNG、JPEG、GIF、BMP；EMF/WMF 标记为"矢量图"不做 OCR

### FR-2: 独立图片文件导入

- FR-2.1: 目录导入支持 .png/.jpg/.jpeg/.gif/.webp/.bmp 格式的图片文件
- FR-2.2: 导入时需配置"包含图片"开关（默认关闭，避免大量装饰性图片污染知识库）
- FR-2.3: 两遍扫描关联——先处理文本文件建立图片引用映射（Markdown `![](path)`、HTML `<img src>`），再处理图片时查找引用上下文
- FR-2.4: 无引用关联的独立图片，用文件名 + 目录路径 + OCR 文字做描述

### FR-3: 图片描述生成

- FR-3.1: 支持可选的 Vision LLM（OpenAI 兼容接口），用于高质量图片语义描述
- FR-3.2: Vision LLM 配置保存时做 health check（发送测试图片验证 API），通过后标记 `vision_verified: true`
- FR-3.3: Vision LLM 未配置或未验证通过时，走 RapidOCR + 上下文推断
- FR-3.4: Vision LLM 运行时调用失败（key 过期/服务下线），自动降级到 OCR 路径，清除 verified 标记
- FR-3.5: RapidOCR 提取图中文字，结果作为 DocumentNode.Text 的一部分存入索引
- FR-3.6: 上下文推断（兜底）：用文件名 + 所属章节标题 + 前后段落文本生成基础描述

### FR-4: 图片资产存储

- FR-4.1: 图片原文件保存到 `~/.maclaw/knowledge_assets/{source_id}/original.{ext}`
- FR-4.2: 生成列表缩略图 `thumb_120.jpg`（120×120px）
- FR-4.3: 生成详情预览图 `preview_480.jpg`（480px 宽等比缩放）
- FR-4.4: Source 删除时，同步清理对应的资产目录

### FR-5: Agent 检索增强

- FR-5.1: `knowledge_search` 结果中包含图片 Source 信息（图片路径、描述、OCR 文字）
- FR-5.2: `knowledge_context_pack` 组装上下文时包含图片 Card 信息
- FR-5.3: 自动召回注入 system prompt 时，图片 Card 标注 `[图片]` 前缀和原图路径
- FR-5.4: Agent 可通过 `send_file` 发送知识库中的图片给用户

### FR-6: GUI 展示（maclaw 桌面版）

- FR-6.1: 知识库 Source 列表中图片类型显示缩略图和 图标
- FR-6.2: Source 详情页展示 preview_480 预览图 + 描述 + OCR 文字 + 关联文档
- FR-6.3: 文档 Source 详情中，DocumentNode 树显示内嵌图片节点（带缩略图）
- FR-6.4: 导入对话框支持"包含图片"开关
- FR-6.5: 知识库设置中增加 Vision LLM 配置区域（BaseURL/APIKey/Model + 测试按钮）

### FR-7: maclawsrv 用户知识库管理

- FR-7.1: `GET /api/v1/knowledge/sources/{id}/thumbnail` 返回缩略图
- FR-7.2: `GET /api/v1/knowledge/sources/{id}/image` 返回原图
- FR-7.3: `POST /api/v1/knowledge/import/image` multipart 上传单张图片
- FR-7.4: 用户知识库管理页面中图片 Source 显示缩略图

### FR-8: 完整性检查

- FR-8.1: `knowledge doctor` 检查图片 Source 的原始文件是否存在（可能被外部删除）
- FR-8.2: 缩略图缺失时自动重新生成
- FR-8.3: 检查 Vision 描述是否为空（标记为"建议重新处理"）
- FR-8.4: 检查孤立资产目录（Source 已删除但文件残留）

## 非功能需求

- NFR-1: 导入 100 张图片时总耗时 < 5 分钟（不含 Vision LLM 网络延迟）
- NFR-2: Vision LLM 批量调用限流：并发 ≤ 2，每秒 ≤ 5 次
- NFR-3: 超过 50 张图片时，Vision 描述改为异步后处理（先用 OCR 完成导入）
- NFR-4: 缩略图生成使用 Go 标准库，无 CGO 依赖
- NFR-5: 图片资产总容量不设硬上限，由 doctor 维护清理命令支持手动管理
- NFR-6: Vision LLM 配置与主 LLM 配置独立（可指向不同服务/模型）

## 约束

- C-1: PDF 图片提取需要引入 `pdfcpu` 纯 Go 库
- C-2: OCR 依赖 `RapidOCRSidecar`（需 Python 3.10+ 和 `rapidocr-onnxruntime`）
- C-3: DOCX/PPTX 图片提取使用 Go 标准库 `archive/zip`
- C-4: 旧格式 .doc 图片提取为 P1 阶段，.xls 图片提取为 P2 延后
- C-5: EMF/WMF 矢量格式暂不做 rasterize，标记为"矢量图"跳过 OCR
