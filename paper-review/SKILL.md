---
name: paper-review
description: "论文深度解读 Skill — 下载论文PDF → LLM深度解读（问题/创新点/方法原理/实验分析）→ PDF图片提取 → 生成组会PPT → 生成解读音频MP3。端到端学术论文解读工具。"
license: MIT
requires_env: OPENAI_API_KEY
triggers:
  - 论文解读
  - paper review
  - paper reading
  - 组会PPT
  - 研读论文
required_args:
  - paper_url
metadata:
  version: "1.0"
  category: academic
  author: "RapidAI Research - 安妮"
---

# Paper Review — 论文深度解读工具

端到端论文解读流程：下载论文 → LLM 深度解读 → 提取图片 → 生成 PPT → 生成 MP3 音频。

## 架构概览

```
用户提供论文URL/路径
       │
       ▼
┌──────────────────┐
│  Step 1: 下载PDF  │  (支持 arXiv / DOI / 任意URL)
└────────┬─────────┘
         ▼
┌─────────────────────────┐
│  Step 2: 全文提取 (txt)  │  (PDF→文本，含图片占位标记)
└────────┬────────────────┘
         ▼
┌──────────────────────────────────────────────┐
│  Step 3: LLM 深度解读分析                     │
│   • 解决的问题                               │
│   • 创新点                                   │
│   • 方法本质与核心原理                        │
│   • 详细原理解读                              │
│   • 实验方法与结果分析                        │
└────────┬─────────────────────────────────────┘
         ▼
┌──────────────────┐
│  Step 4: 提取图片 │  (PDF→PNG图片素材)
└────────┬─────────┘
         ▼
┌──────────────────────────────┐
│  Step 5: 生成组会PPT (.pptx)  │  (深度解读内容 + 图片嵌入)
└────────┬─────────────────────┘
         ▼
┌──────────────────────────────┐
│  Step 6: 生成解读音频 (.mp3)  │  (TTS 合成为自然语音)
└────────┬─────────────────────┘
         ▼
      输出文件清单
```

## 环境要求

- Python 3.8+
- 依赖安装：`pip install requests PyPDF2 python-pptx Pillow pydub`
- 如使用 Edge TTS 音频：`pip install edge-tts`
- (可选) pdf2image + poppler 用于图片提取

## Step 1: 下载 PDF

从 arXiv / DOI / 任意 URL 下载论文 PDF。

```bash
python "{baseDir}/steps/download_paper.py" --url "{{paper_url}}" --output "{{output_dir}}"
```

参数：
- `--url`: 论文 URL（arXiv 链接、DOI 链接、或直接 PDF 链接）
- `--output`: 输出目录（可选，默认当前目录）

输出：`{{paper_id}}.pdf`

## Step 2: 全文提取

从 PDF 提取文本内容（含图片标记）：

```bash
python "{baseDir}/steps/extract_text.py" --pdf "{{pdf_path}}" --output "{{output_dir}}"
```

输出：`{{paper_id}}_text.txt`

## Step 3: LLM 深度解读

调用 OpenAI 兼容 API 对论文进行深度解读：

```bash
python "{baseDir}/steps/analyze_paper.py" --text "{{txt_path}}" --output "{{output_dir}}"
```

输出：`{{paper_id}}_review.json` + `{{paper_id}}_review.md`

解读结构：
```json
{
  "title": "论文标题",
  "problem": "解决的问题",
  "novelty": ["创新点1", "创新点2", ...],
  "method_essence": "方法本质/核心思想",
  "method_detail": "详细原理解读",
  "experiments": "实验方法及结果分析",
  "conclusion": "结论与启示"
}
```

## Step 4: 提取图片素材

从 PDF 中提取图片作为 PPT 素材：

```bash
python "{baseDir}/steps/extract_images.py" --pdf "{{pdf_path}}" --output "{{output_dir}}/images"
```

参数：
- `--dpi`: 图片 DPI（默认 200）
- `--format`: 输出格式（默认 png）

输出：`images/page_XX.png` 等

## Step 5: 生成组会 PPT

基于解读内容和图片生成组会 PPT：

```bash
python "{baseDir}/steps/generate_ppt.py" --review "{{review_json}}" --images "{{images_dir}}" --output "{{output_dir}}"
```

PPT 结构（10-15页）：
1. **封面** — 论文标题 / 作者 / 会议
2. **研究背景与动机** — 为什么做这个工作
3. **要解决的问题** — 问题定义与挑战
4. **核心创新点** — 本文的主要贡献
5. **方法总览** — 整体架构图（含原论文图）
6. **方法详解①** — 关键模块/步骤
7. **方法详解②** — 更多细节
8. **核心原理剖析** — 为什么有效
9. **实验设置** — 数据集 / 评价指标 / 基线
10. **实验结果** — 主实验结果（含原论文表格）
11. **消融实验** — 各模块贡献分析
12. **可视化分析** — 可视化结果（含原论文图）
13. **讨论与局限性**
14. **总结与启示**
15. **参考资料 / Q&A**

输出：`{{paper_id}}_presentation.pptx`

## Step 6: 生成解读音频

将完整解读文本合成为 MP3 音频：

```bash
python "{baseDir}/steps/generate_audio.py" --text "{{review_md}}" --output "{{output_dir}}"
```

输出：`{{paper_id}}_audio.mp3`

## 完整运行

一步到位运行所有步骤：

```bash
python "{baseDir}/run.py" --url "{{paper_url}}" --output "{{output_dir}}"
```

## 输出文件

| 文件 | 说明 |
|------|------|
| `{{paper_id}}.pdf` | 原论文 PDF |
| `{{paper_id}}_text.txt` | PDF 提取的纯文本 |
| `{{paper_id}}_review.md` | LLM 深度解读报告 |
| `{{paper_id}}_review.json` | 结构化解读数据 |
| `images/` | 提取的论文图片素材 |
| `{{paper_id}}_presentation.pptx` | 组会 PPT |
| `{{paper_id}}_audio.mp3` | 解读音频 |

## 自定义配置

通过环境变量配置 API：

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `OPENAI_BASE_URL` | API 基础 URL | 由 maclaw proxy 自动注入 |
| `OPENAI_API_KEY` | API 密钥 | 由 maclaw proxy 自动注入 |
| `OPENAI_MODEL` | 模型名称 | `gpt-4o-mini` |
| `TTS_ENGINE` | TTS 引擎 (openai/edge) | `edge` |
| `PPT_LANG` | PPT 语言 (zh/en) | `zh` |
