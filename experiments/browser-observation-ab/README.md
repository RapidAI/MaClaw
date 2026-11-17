# 浏览器观察对照实验（合成夹具）

这个目录是独立测量工具。它不包含 OpenBot 的实现代码。OpenBot 仓库只在固定提交上阅读，许可证是 PolyForm Noncommercial 1.0.0。

页面都是合成的，绑定在 `127.0.0.1`。没有支付、没有真实验证码，密码框拒绝填写。

## 同一模型怎么跑

Bot1 已经配好 GLM 5.3 Flash 官方直连。这个夹具不读取、不打印、不改那份生产配置。仓库里的官方兼容调用把模型 id 写成 `glm-5.3-flash`，基址形如 `https://open.bigmodel.cn/api/paas/v4`。`coding/paas` 和 `opencode/glm-5.3-flash-free` 不是这一路。

本云环境没有这份凭证。父流程在持有 Bot1 配置的机器上二选一：

1. 本机回环代理。代理进程自己使用已有配置，只听 `127.0.0.1`，只转发 chat completions，强制 `model=glm-5.3-flash`，日志去掉 `Authorization`。夹具侧设置 `MACLAW_BENCH_LOOPBACK=1`、`MACLAW_BENCH_MODEL_BASE_URL=http://127.0.0.1:<port>/v1`，不设置供应商密钥。
2. 由父进程把密钥放进夹具进程的环境变量，不要放进命令行参数，也不要写进夹具会打开的文件。变量名是 `MACLAW_BENCH_MODEL_API_KEY`。基址和模型名仍用上面的官方值。

两种都还要 `MACLAW_BENCH_MODEL_NAME=glm-5.3-flash`、`MACLAW_BENCH_MODEL_VISION=1`、`MACLAW_BENCH_CONFIRM=1`。设置 `MACLAW_BENCH_CONFIG_PATH` 或 `MACLAW_BENCH_READ_APP_CONFIG=1` 会直接拒绝。返回体里的 `model` 若不是 `glm-5.3-flash`，该臂中止，不算对照。错误文本会打码。截图臂若被端点拒绝图片，也中止，不改成纯文本再继续。页面仍是合成的，不发布、不付款。

未满足时 `node run.mjs --mode agent` 退出码为 2，模型调用数为 0。接口自检：`node --test src/agent-gate.test.mjs`。

## 已经会跑的部分

```bash
cd experiments/browser-observation-ab
npm install
node run.mjs --mode mechanical
```

机械测量比较三条对称策略，两臂都有：

- `reobserve_each`：每个原语前都重新观察
- `reuse_one_observation`：一次观察，随后的动作复用这次快照或这次截图里的目标；目标不在里面才再观察
- `batched_tool`：一次工具回合里跑完全部原语。对话框后才出现的按钮会在同一回合里再观察一次，这是正确性要求，不是额外的模型调用

坐标臂不在每个原语之间强制截图。bbox 只作为执行器的地面真值，单独记在 `harness_bbox_ms`，不算进截图生成时间，也不换算成 token。

原始事件在 `results/<时间>/repeat-*/trace.jsonl`。

## 源码结论（阅读提交）

OpenBot `f694dea63e971afc3fd856d6ccff6c5a286ce347`：

- 快照工具给出紧凑行：`ref role "name"`。ref 维持到导航或下一次快照。
- `click` / `type` 默认只回状态；`snapshot: true` 才再拍快照。
- `fill_fields` 一次最多 16 个已检查过的普通字段，一次最终快照，拒绝密码、验证码和 `cc-` 字段。
- `run_script` 一次工具调用最多 200 个浏览器步骤。
- 有 iframe、canvas/video 或语义覆盖很低时，`snapshot` 的 `image: auto` 会另附一张图。
- 跨域 iframe 的 `contentDocument` 读不到；无障碍树按 frame，另有目标会话。
- OpenCode 免费推荐列表含 `opencode/glm-5.3-flash-free`。旁边的耗时注释是 2026-11-11 的编码任务排序，不是这次浏览器测量。

MaClaw `4490db65e924e57c08ef36488f075a1f11e23538`：

- `browser` 观察先走 DOM（开放 shadow 和同域 iframe），再合并主会话无障碍树，再在已附加的 iframe 会话里观察。
- `browser_task_run` 的快速批处理每步不观察，结束时观察一次；`emitProgress` 仍按步更新界面。界面更新次数不等于模型调用次数。
- 视觉只在画布或可点 ref 很少时做一次 OCR 文本，不是每步把截图交给模型。

这些是源码事实。夹具数字在 `results/`，不能当成 GLM 的任务成功率。

## 合成编辑器的失败注入

`node run.mjs --mode richtext` 另外跑一组编辑器故障。页面仍然是合成的，不登录、不发布。

- 上传先把编辑器标成 `aria-busy`，过一会儿才出现图片。正确策略是等待后再观察，不立刻再选一次文件。立刻重试会被记成重复图片，这是失败样本，不是成功。
- 焦点在链接框时 Ctrl+A 改不到正文。检测后应把焦点放回编辑器。
- Ctrl+A 被注入成“再复制一份正文”而不是替换。键盘 Ctrl+Z 用替换前的备份还原。这条路径不调用编辑器 API。
- 混合臂才调用夹具自己的 `editorApi.replaceDocument` / `undo`。替换前先压入整篇备份。这条结果单独标记，不算语义快照获胜。
- 插入 arXiv 链接不会增加图片数，完整性检查拒绝用链接代替图片。测试不去请求 arxiv.org。

公开的 `editor.csdn.net/md` 首屏只是空的 `#app`。当时取到的 `app.chunk.18384971.js` 里没有 `CodeMirror` 这个标识。混合臂不是 CSDN 的内部接口。

购物加整理文件的 135 步、19 分 46 秒不属于这篇编辑任务，不要拿来当 CSDN 耗时。
