# 浏览器观察对照实验（合成夹具）

这个目录是独立测量工具。它不包含 OpenBot 的实现代码。OpenBot 仓库只在固定提交上阅读，许可证是 PolyForm Noncommercial 1.0.0。

页面都是合成的，绑定在 `127.0.0.1`。没有支付、没有真实验证码，密码框拒绝填写。

## 同一模型怎么跑

Bot1 已核对的直连是 `https://open.bigmodel.cn/api/anthropic`，协议 Anthropic，成员 `智谱编程/glm-5.3-flash`。天气 PDF 那次 13 轮、339 秒不是这次对照。不假定该模型接受图片。截图臂会附 PNG；响应若拒绝图片，该臂记为 `vision_rejected`，不改成纯文本。语义臂不发图片。`coding/paas` 和 OpenCode 免费 id 不是这一路。

在获准调用的主机上，密钥只留在该进程环境里：

```bash
cd experiments/browserobservation
export MACLAW_BENCH_MODEL_BASE_URL=https://open.bigmodel.cn/api/anthropic
export MACLAW_BENCH_PROTOCOL=anthropic
export MACLAW_BENCH_MODEL_NAME='智谱编程/glm-5.3-flash'
export MACLAW_BENCH_CONFIRM=1
export MACLAW_BENCH_PER_ARM_CALLS=5
export MACLAW_BENCH_MAX_CALLS=60
export MACLAW_BENCH_SEED=bench-1
export MACLAW_BENCH_OUT=/tmp/maclaw-bench
# MACLAW_BENCH_MODEL_API_KEY 由该主机环境提供，不要 echo
node run.mjs --mode agent
```

回环代理时加 `MACLAW_BENCH_LOOPBACK=1`，基址改为 `http://127.0.0.1:<port>/anthropic`，协议仍是 anthropic，不放供应商密钥。不要设置 `MACLAW_BENCH_CONFIG_PATH`。

未满足时 `node run.mjs --mode agent` 退出码为 2，不会发请求。满足之后同一命令按相同任务、每臂次数和 seed 调用 `runBoundedArms`。这台机器没有授权路由，所以还没有真实模型结果。假服务契约不是真实模型：

```bash
node --test agent-gate.test.mjs transport.test.mjs
go test -count=1 -timeout 2m ./experiments/browserobservation
```

`--mode microbench` 只测脚本化观察开销。生成物写到临时目录，不进源码树。

## 已经会跑的部分

```bash
cd experiments/browserobservation
npm install
node run.mjs --mode microbench
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
