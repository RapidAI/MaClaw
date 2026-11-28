# 内嵌兜底中文字体

`NotoSansSCSubset.ttf` 是 PDF 生成的最后兜底字体：裸机（无系统
中文字体的云主机）上检测落到它，保证 `generate_pdf` 永远可用。
见 `generator.go` 的 `embeddedFallbackFontData` 与检测顺序注释。

## 来源与许可

- 上游：google/fonts 仓库 `ofl/notosanssc/NotoSansSC[wght].ttf`
  （Noto Sans SC 可变字体，TrueType 轮廓）。
- 许可：SIL Open Font License 1.1（见同目录 `OFL.txt`，必须随字体
  一起分发）。OFL 允许修改与再分发；子集化后的字体不得使用
  Reserved Font Name 'Source'，本文件名为 Noto Sans SC，不冲突。

## 子集化方式（重新生成步骤）

1. 用 fonttools `varLib.instancer` 把可变字体分别钉在 `wght=400`
   （Regular，`NotoSansSCSubset.ttf`）和 `wght=700`（Bold，
   `NotoSansSCSubset-Bold.ttf`），去掉 fvar/gvar 等可变表。
2. 用 `pyftsubset` 裁剪到 GB2312 全部字符（约 6763 个汉字 + 全角
   标点）+ ASCII 可打印字符 + Latin-1 Supplement 与 Latin Extended-A
   （U+00A0–U+017F，覆盖法德西等西欧文字）+ 常见排版符号
   （—–…“”‘’℃ 等），共约 7740 字符，去 hinting，保留 name 表中的
   版权与许可条目。
3. 每个产物约 2.2MB（原可变字体约 17.8MB）。粗体必须存在：否则
   fallback 路径下标题和强调全部渲染成常规体重，而在下载不可达的
   主机上 fallback 是永久状态。

依赖：`pip install fonttools`。验证：`go test ./corelib/docgen/
-run TestEmbeddedFallback` 会检查嵌入、CJK 覆盖与真实渲染。
