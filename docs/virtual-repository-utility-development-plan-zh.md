# 实用工具「虚拟仓库」开发设计文档

> 状态：核心功能已实现；真实 SSH/SFTP 多平台兼容矩阵与发布验收待执行  
> 日期：2026-07-22  
> 范围：MaClaw 桌面端 `实用工具`，Wails + React/TypeScript + Go

## 1. 背景与目标

在桌面端「实用工具」中新增「虚拟仓库」。用户可以把多个实际 Git 或 SVN 工作副本组织成一棵自定义目录树，并以一个逻辑仓库的方式查看状态、提交/推送或回退；也可以只操作树中的某个目录。

“虚拟”仅表示组织视图是虚拟的。每个叶子仓库对应虚拟根目录下的真实目录：本地模式位于桌面机，远程模式位于用户指定的 SSH 服务器。Git/SVN 命令必须在目录实际所在的机器执行。本功能不引入新的版本控制格式，不把多个仓库合并成单个 Git/SVN 仓库，也不实现跨仓库原子事务。

### 1.1 本期目标

- 在 `UtilitiesPage` 首页新增「虚拟仓库」工具卡，点击后进入专用工作台。
- 创建、修改、删除多个虚拟仓库定义；设置名称和本机根目录。
- 在虚拟仓库根目录创建 `.vrepo/`，以其中的 manifest 作为目录树与实际仓库对应关系的权威配置。
- 在虚拟树中创建任意层级的组织目录，并映射到 Git、SVN 工作副本或纯本地物理目录。
- 配置仓库地址、相对本地路径和凭据引用；支持从已保存的同类型凭据中选择“使用保存的密码”。
- 提供独立的“仓库凭据”管理界面，支持新增、修改、删除用户名/密码对。
- 支持整棵虚拟树或选中子树/单仓库执行：刷新状态、提交、推送、提交并推送、回退。
- 提供执行前预览、危险操作确认、逐仓库结果和可重试的部分失败反馈。

### 1.2 明确不做

- 不实现 Git/SVN 客户端本身，不解析或修改 `.git`、`.svn` 内部文件。
- 不承诺多仓库操作的事务性；仓库 A 成功、仓库 B 失败时不自动撤销 A。
- 不支持把密码写入仓库 URL、命令行参数、日志、前端持久化或虚拟仓库定义文件。
- MVP 不处理 Git 合并冲突、rebase、分支创建/合并/删除、SVN 冲突编辑器、历史浏览、diff 编辑器；映射节点可选择已有 branch/tag。
- MVP 不自动执行 force push、`git reset --hard`、`git clean` 或删除未跟踪文件。
- 不把服务器 API 凭据模型或 OAuth `credentials.json` 直接复用为仓库密码库；二者用途和暴露边界不同。

## 2. 需求理解与关键语义

### 2.0 本地与远程根目录

虚拟仓库新增两种部署位置：

- **本地**：根目录是桌面机上的真实目录，维持现有行为。
- **远程（SSH）**：用户提供服务器 IP/域名、SSH 端口、用户名、密码和远程绝对根目录。`.vrepo/manifest.json`、Git/SVN 工作副本与 Local 构建目录都位于远程机器；状态、提交、推送、回退、目录创建和空间统计均通过 SSH/SFTP 在远程执行。

远程连接信息分层保存：

- 远程 `.vrepo/manifest.json` 仍只记录可移植的名称、ID 和节点树，不记录服务器坐标。`remote.host`、`remote.port`、`remote.user` 和远程 `root_path` 保存在桌面机私有索引中，避免把内部服务器地址随 manifest 传播。
- SSH 密码保存在系统钥匙串，索引键为虚拟仓库 ID；前端和 JSON 响应不回显秘密。
- 首次连接必须展示服务器 host key 指纹并由用户确认；确认后固定指纹。后续指纹变化必须阻止连接，不能静默接受。不能复用当前 `corelib/remote` 中尚使用 `InsecureIgnoreHostKey` 的底层拨号实现。
- 远程服务器必须提供 POSIX shell。本期支持 Linux/macOS 类远程主机；Windows SSH Server/PowerShell 作为后续兼容项。

远程仓库的打开方式不是本地目录选择器，而是从最近列表打开，或填写 SSH 坐标与远程根目录后连接。首次创建时通过 SFTP 原子写入 `<remote-root>/.vrepo/manifest.json`。远程根路径必须是绝对 POSIX 路径，映射节点仍只允许安全相对路径；服务端执行命令前对根目录与目标目录分别 `realpath` 并验证目标未越界。

最近虚拟仓库列表及仓库工作台提供“启动编程任务”。本地虚拟仓库创建 `coding_dev` 新标签页，执行目录为本地虚拟仓库根目录；远程虚拟仓库创建 `remote_coding_dev` 新标签页，执行目录为远程根目录，并复用系统钥匙串中的 SSH 密码及虚拟仓库已确认的 host-key 指纹。任务启动不得把密码传回前端、写入任务标签或降级为忽略 host key。若密码缺失、指纹未确认或远程连接失败，则不打开标签页，并隐藏已经创建但未成功预热的任务记录。

### 2.1 领域对象

1. **虚拟仓库（Virtual Repository）**：一个用户命名的逻辑工作区，包含一个根目录和一棵节点树。
2. **虚拟目录节点**：仅用于组织，可有子节点；其名称不能包含路径分隔符。
3. **映射节点**：挂载在某个虚拟目录节点上的 Git、SVN 或纯本地目录配置。每个节点都必须映射到 `根目录 + relative_path` 下的真实物理目录。
4. **仓库凭据（Repository Credential）**：按 VCS 类型保存的用户名/秘密组合。仓库节点只保存 `credential_id`，不保存密码。
5. **操作任务（Operation Job）**：对一个或多个仓库执行的状态、提交、推送或回退任务，包含逐项结果。

虚拟仓库采用**根目录自描述**模型：只要选择一个含 `.vrepo/manifest.json` 的根目录，MaClaw 就能恢复虚拟目录树和仓库映射，不依赖某台机器的全局配置。机器相关的凭据绑定、密码、最近打开记录和状态缓存不写入 `.vrepo`。

为避免“每个目录都是 repo”与“还要在虚拟目录树上添加虚拟目录”的歧义，数据模型区分两种节点：**仅组织的虚拟目录**与**映射到真实路径的节点**。映射节点类型为 Git、SVN 或 Local。界面文案区分“新建虚拟目录”“添加仓库”和“添加本地目录”。执行子树版本控制操作时只收集 Git/SVN 节点；Local 节点会列为“不适用”而不是失败。

### 2.2 整体与分目录操作

- **整体操作**：目标为虚拟仓库根节点，收集其下所有已启用仓库。
- **分目录操作**：目标为任意虚拟节点，收集该节点及后代仓库。
- **单仓库操作**：目标为具体仓库节点，仅操作一个真实工作副本。
- 任务开始时生成固定目标快照，执行过程中修改树结构不会改变正在运行的目标集合。
- 默认按树的显示顺序串行执行，降低凭据提示、锁竞争和服务端限流风险；后续可增加可配置并发度。
- 结果语义为 `success`、`partial_success`、`failed`、`cancelled`。部分失败不回滚已成功仓库。

纯本地目录参与整体/子树的目录展示、存在性检查和打开目录操作，但不执行 VCS 命令。它适合放置编译结果、打包产物、部署包或跨仓库共享输出。MVP 不自动复制、清理或发布其中内容，也不会因为版本控制操作而删除这些文件。

### 2.3 Branch / Tag 与检出语义

- Git/SVN 映射可配置 `ref_type`（`branch` 或 `tag`）和 `ref_name`；两者都留空表示使用仓库缺省版本。
- Git 留空时执行普通 `git clone`，由远端 `HEAD` 决定主分支/缺省分支；指定 branch 或 tag 时执行 `git clone --branch <ref> --single-branch`。
- SVN 留空时检出用户填写的仓库 URL；指定 branch/tag 时，本期按标准布局分别检出 `<remote_url>/branches/<ref_name>`、`<remote_url>/tags/<ref_name>`。非标准 SVN 布局由用户直接填写目标 URL，并将 branch/tag 留空。
- Tag 映射按发布快照处理，只允许刷新状态和回退未提交改动；提交、推送、提交并推送在预览阶段阻止。Git tag 检出为 detached HEAD；branch 映射维持正常提交语义。
- 工作目录不存在时状态为 `not_checked_out`，界面提供“检出仓库”；路径非法、越界或符号链接逃逸不会显示检出按钮。

### 2.4 Git 与 SVN 操作对照

| 用户动作 | Git | SVN | 说明 |
|---|---|---|---|
| 刷新状态 | `git status --porcelain=v2 --branch` | `svn status` + `svn info --show-item url` | 只读 |
| 提交 | `git add -A`，再 `git commit -m` | `svn commit -m` | 提交前必须展示变更摘要 |
| 推送 | `git push` | 不适用 | SVN commit 已上传服务器 |
| 提交并推送 | commit 成功后 `git push` | 等同于 `svn commit` | UI 对 SVN 解释差异 |
| 回退已跟踪改动 | `git restore --staged --worktree -- .` | `svn revert -R .` | 危险操作，强确认；不删除未跟踪文件 |

纯本地目录没有提交、推送或回退语义。混合范围执行时，确认页展示 Local 节点数量并标记“跳过（纯本地目录）”；只有 Git/SVN 的失败才影响版本控制任务结果。

“revert”在 Git 中可能指“丢弃工作区改动”或“创建反向提交”。本需求更接近工作副本回退，MVP 明确定义为**丢弃尚未提交的已跟踪改动**。若以后需要撤销历史提交，另设“反向提交”动作，不能与本操作复用。

提交范围默认是仓库内全部变更。虚拟目录只是仓库集合范围，不用于拼接 VCS pathspec。Git 的未跟踪文件会被 `git add -A` 纳入提交；SVN 的未版本化文件不会被自动 `svn add`，结果页应列出并提示用户。

## 3. 现有代码基线与集成位置

### 3.0 远程执行架构

远程模式新增独立适配层，业务层继续使用同一套 preview/job/result 模型：

```text
VirtualRepository service
  ├─ LocalRepositoryRuntime  -> os/exec + 本地文件系统
  └─ SSHRepositoryRuntime
       ├─ known-host fingerprint verifier
       ├─ password from OS keyring
       ├─ SFTP manifest atomic read/write
       └─ SSH session command runner (无 PTY、超时、取消、输出上限、脱敏)
```

远程命令不得拼接未转义的用户字段。命令名由代码白名单生成，路径和提交说明使用 POSIX 单引号转义；SSH 密码只参与握手，不进入远程环境。Git/SVN 仓库凭据沿用仓库凭据管理器，但需在远程会话内通过临时 askpass/标准输入使用，并在命令结束后删除临时文件。远程日志与任务结果应用与本地相同的 512 KiB 上限和秘密脱敏规则。

同一远程虚拟仓库仍只允许一个写任务；不同仓库可并行。取消任务时关闭 SSH channel，使当前远程进程收到 channel close；对无法确认终止的场景，结果标记为 `cancelled` 并提示用户复查远程工作副本状态。

### 3.1 前端

- `gui/frontend/src/components/pages/UtilitiesPage.tsx` 已承载实用工具首页和问卷子视图，使用 `View` 状态切换，首页工具卡在同一文件中生成。
- `gui/frontend/src/components/pages/UtilitiesPage.css` 定义现有工具卡、工作区、表单与反馈样式。
- 前端通过动态导入 `wailsjs/go/main/App` 调用 Go `App` 方法，适合增加虚拟仓库绑定。
- `ConfirmDialog` 可作为删除定义、删除凭据和回退操作的基础确认组件。
- 现有 `SelectProjectDir` / `SelectWorkingDir` 已证明 Wails 原生目录选择器可用，但新功能应提供语义明确的 `SelectVirtualRepositoryRoot`，避免复用不相关命名。

`UtilitiesPage.tsx` 已超过 2500 行。本功能不能继续把完整工作台堆入该文件；只在其中保留工具卡和入口，主体拆成独立组件与领域 helper。

### 3.2 后端

- `gui/app.go` 中 `App` 是 Wails 绑定入口，已有 `runtime.OpenDirectoryDialog` 用法。
- `gui/coding_workbench_git.go` 有 `runGitInProject`、仓库识别、状态摘要和提交逻辑，可参考其超时、stdout/stderr 捕获方式；虚拟仓库需要单独的、更严格的执行层，避免改变现有 Coding Workbench 语义。
- 仓库中已有 Git 命令封装，但未发现可复用的 SVN 工作台能力。
- 当前 OAuth `FileCredentialStore` 将 token 以权限 `0600` 的 JSON 落盘，适合 OAuth 生命周期，但不满足通用 Git/SVN 明文密码长期保存的安全要求。
- 当前 Go 依赖未提供统一 OS keyring 封装，虚拟仓库需新增专用秘密存储抽象。
- 虚拟仓库定义的权威来源为根目录内 `.vrepo/manifest.json`；`~/.maclaw` 仅维护根目录索引和本机私有数据。

### 3.3 建议文件布局

```text
gui/
  virtual_repository_types.go
  virtual_repository_store.go
  virtual_repository_credentials.go
  virtual_repository_paths.go
  virtual_repository_runner.go
  virtual_repository_git.go
  virtual_repository_svn.go
  virtual_repository_wails.go
  virtual_repository_*_test.go

gui/frontend/src/components/pages/
  UtilitiesPage.tsx
  utilitiesVirtualRepository.ts
  VirtualRepositoryWorkspace.tsx
  VirtualRepositoryWorkspace.css
  VirtualRepositoryTree.tsx
  VirtualRepositoryEditor.tsx
  VirtualRepositoryCredentialManager.tsx
  VirtualRepositoryOperationDialog.tsx
  VirtualRepositoryJobPanel.tsx
  __tests__/VirtualRepositoryWorkspace.test.tsx
```

## 4. 产品与交互设计

### 4.0 新建远程虚拟仓库

新建表单顶部提供“本地 / 远程 SSH”单选。选择远程后显示：服务器、端口（默认 22）、用户名、密码、远程根目录。提供“测试连接”主动作，测试内容包括 SSH 握手、host key 校验、根目录是否存在且为目录、远程 Git 版本，以及 SVN 自动搜索结果。密码编辑留空表示保留钥匙串中的原值。

首次出现未知 host key 时，界面展示算法与 SHA256 指纹并要求确认“信任并保存”；host key 已变化时使用阻断性错误，不提供一键覆盖，用户必须在连接设置中显式删除旧指纹后重新确认。远程详情头部持续显示 `user@host:port · /root/path` 和连接状态，使用户不会误把远程操作当成本地操作。

### 4.1 入口与工作台

在实用工具首页增加与现有视觉体系一致的工具卡：

- 标题：虚拟仓库 / Virtual Repository
- 说明：统一管理多个 Git、SVN 工作副本，按整体或目录提交、推送与回退。
- 行为：点击后进入页面内工作台，不打开新的系统窗口。

工作台采用三栏结构，窄屏时折叠为上下区域：

```text
┌ 虚拟仓库选择器 ─ 新建 ─ 编辑 ─ 仓库凭据 ─ 刷新状态 ┐
├──────────────┬────────────────────────┬─────────────┤
│ 虚拟目录树    │ 选中节点详情 / 变更摘要 │ 操作与任务    │
│ 根节点        │ 路径、类型、地址、状态   │ 提交          │
│ ├─ 服务端     │ modified / untracked   │ 推送          │
│ │  ├─ API Git │ last error / branch    │ 提交并推送     │
│ │  └─ Docs SVN│                        │ 回退          │
│ └─ 客户端     │                        │ 逐仓库结果     │
└──────────────┴────────────────────────┴─────────────┘
```

### 4.2 首次使用

空状态直接解释三步：创建虚拟仓库、选择根目录、添加仓库。点击“新建虚拟仓库”进入内联创建区：

- 名称：必填，在本机配置内唯一。
- 根目录：必填，通过原生目录选择器选择；保存前后端校验存在且为目录。
- “保存并添加第一个仓库”是主动作，“取消”为次动作。

删除虚拟仓库仅删除 MaClaw 中的定义和凭据引用，**绝不删除根目录或任何工作副本文件**。确认框必须明确这一点。

### 4.3 树编辑

右键菜单和显式工具栏同时提供：

- 新建目录
- 添加仓库
- 重命名
- 编辑仓库
- 移动到……
- 删除节点

键盘支持方向键导航、Enter 展开/收起、F2 重命名、Delete 打开确认框；拖拽移动可作为增强项，MVP 以“移动到……”保证可访问性。

添加/编辑仓库表单：

- 节点名称
- 类型：Git / SVN / 纯本地目录
- 本地相对目录
- 仓库地址（仅 Git/SVN 显示）
- 凭据：无凭据 / 使用保存的密码 / 新建凭据（仅 Git/SVN 显示）
- 启用状态

纯本地目录必须选择或填写一个真实相对目录，不允许成为只有名称的占位节点。详情区展示物理路径、存在性、文件数/占用空间（占用空间统计按需触发，避免打开页面时扫描大型构建目录）以及“在文件管理器中打开”。

仓库地址不得包含内嵌密码。若检测到 `scheme://user:password@host`，阻止保存并提示改用凭据管理器。根目录与相对目录组合后必须仍位于根目录内；拒绝 `..` 越界、绝对路径、符号链接逃逸和与另一个仓库节点重复的规范化路径。

“仓库地址”是期望远端。首次保存或执行刷新时：

- 本地目录已是对应类型工作副本：读取实际远端并比较；不一致时显示警告，不自动改 remote/relocate。
- 本地目录存在但不是工作副本：标记为“未初始化”；为避免覆盖已有内容，不自动检出。
- 本地目录不存在或为空：标记为“未检出”，允许用户明确点击“检出仓库”。Git/SVN 按节点 branch/tag 配置检出；留空时使用主分支/缺省分支（或 SVN 原始 URL）。

对于纯本地目录：保存时目标已存在则校验其为目录；目标不存在时允许用户明确勾选“创建目录”后由后端创建，否则拒绝保存。不得自动把 Git/SVN 工作副本识别并改成 Local；类型始终由用户明确选择。

### 4.4 凭据管理器

从工作台顶部“仓库凭据”进入侧栏或页面内子视图，不使用层层嵌套模态框。列表只显示：

- 显示名称
- 类型（Git/SVN）
- 用户名
- 适用范围提示（可选 host/realm）
- 被多少仓库引用
- 更新时间

秘密永不回显。编辑凭据时密码框为空代表“不修改密码”；只有输入新值才覆盖。删除正在被引用的凭据时列出受影响仓库，确认后这些仓库改为“无凭据”，不级联删除仓库。

“使用保存的密码”的筛选规则：

1. 仅展示与当前仓库类型相同的凭据。
2. 如果填写了 host/realm，优先展示匹配项。
3. 允许用户明确选择其他同类型凭据，但给出作用域不匹配提示。

### 4.5 操作流程

所有写操作使用相同的“预检 → 确认 → 执行 → 结果”流程：

1. **预检**：解析目标子树、规范化路径、探测客户端、刷新状态、验证凭据引用、检查是否有实际可操作内容。
2. **确认**：展示目标仓库数、逐仓库变更数量、将执行的逻辑步骤；不展示带秘密的原始命令。
3. **执行**：显示当前仓库、总进度、可取消状态。取消只阻止尚未启动的仓库，并尽力终止当前子进程。
4. **结果**：逐仓库显示成功/跳过/失败、耗时和脱敏错误；支持仅重试失败项。

提交需要统一提交说明。空提交说明不允许继续。对于 Git 的“提交并推送”，只有该仓库 commit 成功或无须 commit 且已有待推送提交时才 push。对 SVN，“推送”按钮禁用并解释“SVN 提交即上传”；整体混合选择时，“推送”只作用于 Git，确认页明确列出 SVN 将被跳过。

回退是破坏性动作：

- 必须先刷新状态并展示将受影响的已跟踪文件数量。
- 二次确认文案包含仓库数量与“未提交修改将无法从 MaClaw 恢复”。
- 默认不处理未跟踪/未版本化文件；界面明确显示“未跟踪文件将保留”。
- 若仓库存在冲突、rebase/merge 中间态或 SVN 锁定/冲突状态，预检阻止批量回退，要求用户到原生工具处理。

## 5. 数据模型与持久化

### 5.1 根目录 `.vrepo` 配置

每个虚拟仓库根目录下创建专用配置目录：

```text
<virtual-root>/
  .vrepo/
    manifest.json       # 权威配置：名称、ID、虚拟树、物理目录映射
    README.md           # 可选；说明格式版本和“不得保存秘密”
  services/
    api/                # Git/SVN 工作副本
  build/
    release/            # 纯本地编译结果目录
```

`.vrepo/manifest.json` 是虚拟仓库定义的单一权威来源。它适合随整个根目录复制、备份或交给另一台机器使用，也允许用户主动纳入上层版本控制。文件只包含非秘密、可移植信息，使用 UTF-8 JSON、临时文件 + rename 原子写入。

不在 `.vrepo` 内保存：密码、token、系统安全存储引用、机器绝对路径、命令输出、任务历史、锁文件或状态缓存。`relative_path` 一律相对虚拟仓库根目录。

```go
type VirtualRepository struct {
    Version   int                     `json:"version"`
    ID        string                  `json:"id"`
    Name      string                  `json:"name"`
    RootPath  string                  `json:"root_path,omitempty"` // API 返回；本地 manifest 中省略
    Remote    *VirtualRepositoryRemote `json:"remote,omitempty"`
    Nodes     []VirtualRepositoryNode `json:"nodes"`
    CreatedAt time.Time               `json:"created_at"`
    UpdatedAt time.Time               `json:"updated_at"`
}

type VirtualRepositoryRemote struct {
    Host string `json:"host"`
    Port int    `json:"port,omitempty"` // 默认 22
    User string `json:"user"`
}

type VirtualRepositoryNode struct {
    ID           string                  `json:"id"`
    ParentID     string                  `json:"parent_id,omitempty"`
    Name         string                  `json:"name"`
    Order        int                     `json:"order"`
    Repository   *RepositoryBinding      `json:"repository,omitempty"`
}

type RepositoryBinding struct {
    Kind         string `json:"kind"` // git | svn | local
    RelativePath string `json:"relative_path"`
    RemoteURL    string `json:"remote_url,omitempty"`
    RefType      string `json:"ref_type,omitempty"` // branch | tag；ref_name 留空时一并省略
    RefName      string `json:"ref_name,omitempty"`
    Enabled      bool   `json:"enabled"`
}
```

根目录不写入 manifest，由 manifest 所在目录的父目录推导，因此整体移动后仍然有效。节点采用扁平列表 + `parent_id`，便于移动、排序、校验环和局部更新。读取时一次性校验：格式版本受支持、ID 唯一、父节点存在、无环、路径不越界、物理路径不重复、类型合法。Local 节点的 `remote_url` 必须为空。配置损坏时不静默清空；保留原文件并返回可诊断错误。

### 5.2 本机索引与凭据绑定

`~/.maclaw/virtual-repositories-index.json` 只保存最近打开的根目录、虚拟仓库 ID 和最后打开时间，不是权威配置。用户也可直接选择任意含 `.vrepo/manifest.json` 的目录打开；索引丢失不影响恢复虚拟仓库。

仓库与本机凭据的对应关系保存到 `~/.maclaw/virtual-repository-bindings.json`，键为 `virtual_repository_id + repository_node_id`，值为本机 `credential_id`。这样复制或提交 `.vrepo` 时不会泄露用户名、凭据名称或系统 secret 引用。Local 节点没有凭据绑定。

### 5.3 凭据模型

```go
type RepositoryCredentialMetadata struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    Kind        string    `json:"kind"` // git | svn
    Username    string    `json:"username"`
    Scope       string    `json:"scope,omitempty"` // host or SVN realm
    SecretRef   string    `json:"secret_ref"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

- 元数据放入 `~/.maclaw/virtual-repository-credentials.json`；仓库节点到凭据的关系放入本机 bindings 文件，均不进入 `.vrepo`。
- 密码/令牌通过 `RepositorySecretStore` 保存，配置和接口中只流转 `secret_ref`。
- `RepositorySecretStore` 接口至少提供 `Put`、`Get`、`Delete`、`Available`。
- 首选系统安全存储：Windows Credential Manager、macOS Keychain、Linux Secret Service。
- 如果系统安全存储不可用，MVP 默认不允许“保存密码”，仍允许会话内临时输入；是否提供加密文件降级必须单独评审主密钥来源，不能退化为明文 JSON。

会话临时密码只保存在 Go 进程内存中，不返回列表 API，不写日志；任务完成或应用退出后清除。前端仅在新增/修改表单提交瞬间持有输入值，提交完成后立即清空组件状态。

## 6. 后端设计

### 6.1 分层

```text
Wails API
  └─ VirtualRepositoryService
      ├─ ConfigStore
      ├─ CredentialMetadataStore ── RepositorySecretStore
      ├─ PathValidator
      ├─ RepositoryInspector
      └─ OperationRunner
          ├─ GitAdapter
          └─ SVNAdapter
```

- `ConfigStore`：定义 CRUD、树校验和原子持久化。
- `CredentialMetadataStore`：凭据元数据 CRUD 与引用计数。
- `RepositorySecretStore`：系统安全存储适配；禁止日志打印 secret。
- `PathValidator`：根目录、相对路径、真实路径和符号链接边界校验。
- `RepositoryInspector`：探测客户端版本、工作副本类型、状态和实际远端。
- `OperationRunner`：生成固定目标快照，串行执行，发布进度事件，维护可取消 job。
- `GitAdapter` / `SVNAdapter`：只接受结构化参数，不接受整段 shell 字符串；统一超时、输出上限、错误分类和脱敏。

### 6.2 VCS 客户端选型

可以集成 Go 实现的客户端，但 Git 与 SVN 不应强行采用同一种技术路线：

- **Git 推荐混合实现**：引入 `github.com/go-git/go-git/v5`（正式开发时再评估是否直接升到 v6）处理仓库探测、remote、分支和只读状态等稳定能力；提交、push、复杂工作区状态和回退仍通过系统 `git` CLI adapter。`go-git` 是活跃维护的纯 Go 实现，但其官方文档也说明尚缺 merge 等主要 porcelain 能力，不能假设与原生 Git 完全等价。
- **SVN 推荐使用系统 CLI**：现有纯 Go SVN 库成熟度和协议覆盖不足。例如 `github.com/cespedes/svn` 明确标注 very early stage，当前只覆盖有限协议/读操作，无法可靠承担工作副本 status、commit、revert、HTTPS 认证、锁与冲突处理。`goki.dev/vci/v2` 虽提供 Git/SVN 统一接口，但底层仍是命令行封装，版本为开发版，不能减少客户端依赖或认证风险。
- **Local adapter** 不依赖任何 VCS 客户端，只做路径存在性、目录创建、按需统计和打开文件管理器。

因此第一版不追求“完全纯 Go、无需安装 Git/SVN”。统一的是 MaClaw 自己的 `RepositoryAdapter` 接口和错误模型，而不是底层实现。推荐接口：

```go
type RepositoryAdapter interface {
    Inspect(ctx context.Context, target RepositoryTarget) (RepositoryStatus, error)
    Preview(ctx context.Context, op RepositoryOperation, target RepositoryTarget) (OperationPreview, error)
    Execute(ctx context.Context, op RepositoryOperation, target RepositoryTarget) (OperationResult, error)
}
```

后续如果 Go SVN 库的工作副本与认证能力成熟，可替换 `SVNCLIAdapter`，不改变 `.vrepo` 格式、Wails API 或前端交互。Phase 0 必须用真实仓库做兼容性矩阵，比较 go-git 与原生 Git 对 submodule、LFS、worktree、credential helper、文件权限和换行转换的行为；检测到不支持的仓库特征时直接走 CLI。

#### SVN 可执行文件发现与用户指定

SVN 节点首次加载或执行操作前，后端按以下顺序寻找 `svn` 客户端：

1. 用户在本机设置中明确指定的 SVN 可执行文件路径。
2. 当前进程 `PATH` 中的 `svn`（使用 `exec.LookPath`）。
3. 操作系统常见安装位置，例如 Windows 的 TortoiseSVN/VisualSVN/SlikSVN/CollabNet 命令行目录，以及 macOS/Linux 的常见 bin 目录。
4. Windows 可在只读前提下查询已安装程序信息或 App Paths；不得递归扫描整个磁盘。

找到候选后必须执行 `svn --version --quiet` 验证：文件存在、是可执行文件、能在限定时间内正常启动，并记录解析出的版本。只凭文件名或目录存在不能视为可用。

若自动搜索不到，工作台显示“未找到 SVN 命令行工具”，提供：

- “指定 svn 可执行文件”按钮，打开原生文件选择器。
- 当前平台的安装说明入口。
- “重新搜索”按钮。

用户选择后立即执行版本校验；校验失败时不保存并展示具体原因。有效路径保存到本机 `~/.maclaw/virtual-repository-local-settings.json`，不得写入 `.vrepo/manifest.json`，因为不同机器的安装路径不同。应用每次启动及每次 SVN 操作前检查该路径是否仍有效；失效后重新执行自动搜索，仍找不到则要求用户重新指定。

设置界面同时显示来源（用户指定 / PATH / 自动发现）、完整路径和版本，并提供“更改”“恢复自动搜索”。用户指定路径优先级最高，但不得静默切换到同名的其他程序；只有指定路径失效时才启动重新发现流程。

### 6.3 Wails API 草案

读接口可以直接返回结构体；为与现有 Utilities 动态绑定风格保持兼容，也可首期使用 JSON 字符串。无论采用哪种形式，前后端共享字段名必须集中定义并测试。

```go
ListVirtualRepositories() (string, error)
GetVirtualRepository(id string) (string, error)
SaveVirtualRepository(inputJSON string) (string, error)
DeleteVirtualRepository(id string) error
SelectVirtualRepositoryRoot(initialPath string) (string, error)

ListRepositoryCredentials(kind string) (string, error)
SaveRepositoryCredential(inputJSON string) (string, error)
DeleteRepositoryCredential(id string) (string, error)

GetVCSClientStatus(kind string) (string, error)
SearchVCSClient(kind string) (string, error)
SelectVCSClientExecutable(kind string) (string, error)
SetVCSClientExecutable(kind, executablePath string) (string, error)
ResetVCSClientExecutable(kind string) (string, error)

InspectVirtualRepository(inputJSON string) (string, error)
PreviewVirtualRepositoryOperation(inputJSON string) (string, error)
StartVirtualRepositoryOperation(inputJSON string) (string, error)
CancelVirtualRepositoryOperation(jobID string) error
GetVirtualRepositoryOperation(jobID string) (string, error)
```

进度通过 Wails event `virtual-repository:job-updated` 发布；页面卸载时取消订阅。Job 仅保存在本次应用进程中，保留最近有限条完成记录，避免无界增长。

### 6.4 进程与认证

- 使用 `exec.CommandContext`，设置 `cmd.Dir` 为已校验的仓库真实路径；禁止拼接 shell 命令。
- Git 启动前检查系统 `git`；SVN 使用“用户指定路径 → PATH → 常见安装位置”的发现结果。找不到时返回结构化的 `client_not_found`，前端直接引导用户指定，而不是笼统失败。
- 每仓库设置合理超时：状态 30 秒、提交 2 分钟、推送 5 分钟；批量任务允许用户取消。
- stdout/stderr 限长并脱敏 URL userinfo、Authorization、password/token 常见字段。
- Git HTTPS 认证使用受控的临时 askpass helper 和进程环境变量/安全 IPC，不把密码放入参数；设置 `GIT_TERMINAL_PROMPT=0`，避免后台任务挂起。helper 文件使用私有临时目录并在任务后删除。
- SVN 优先使用 `--username` 配合受控认证方式；若客户端仅支持 `--password` 参数，必须确认平台进程列表泄露风险并优先改用临时 config-dir/auth cache。实现前为 Windows/macOS/Linux 各做一次可行性验证，不满足安全要求时该平台只支持会话交互或外部客户端缓存。
- 默认禁止 Git/SVN 自行把 MaClaw 提供的密码永久缓存到其明文 auth cache；是否允许交给外部 credential helper 由用户另行选择。
- SSH 地址不需要用户名/密码对；交由系统 SSH agent/keychain。若用户为 SSH URL 选择密码凭据，预检给出不兼容提示。

### 6.5 路径安全

每次执行，而不仅是保存时，都重新进行以下检查：

1. 根目录存在且为目录。
2. `relative_path` 是清理后的相对路径，不为空、不含越界段。
3. `Join(root, relative)` 后做 `Abs`、`EvalSymlinks`，结果仍在根目录内。
4. 目标目录是期望类型的工作副本。
5. 保存后的配置版本与任务预检版本一致；若定义已变更，要求重新预检。

`.vrepo` 自身是控制数据而不是业务仓库。添加映射节点时禁止 `relative_path` 指向 `.vrepo` 或其后代；批量操作不会把 `.vrepo` 当成仓库或 Local 输出目录。

Windows 路径比较需处理卷名和大小写；不要依赖简单字符串前缀判断。

## 7. 前端实现设计

### 7.1 状态与组件边界

`UtilitiesPage` 的 `View` 增加 `virtual-repository`，首页卡片只执行 `setView('virtual-repository')`。`VirtualRepositoryWorkspace` 内部维护：

- 定义列表、当前定义和选中节点
- 加载/保存/探测状态
- 仓库状态映射
- 编辑器与凭据管理器当前模式
- 当前 job 与最近结果

纯函数放在 `utilitiesVirtualRepository.ts`：树构建、子树目标收集、表单校验、状态汇总、操作按钮可用性。这样可用 Vitest 做细粒度测试。

### 7.2 状态与可访问性

- 加载定义和刷新状态使用局部 skeleton；不能让整个 Utilities 页面空白。
- 树使用正确的 `role="tree"` / `treeitem` 和 `aria-expanded`，选中状态不只依赖颜色。
- Git 与 SVN 用文字徽标加图标；脏、干净、错误状态同时有文字。
- 保存、提交、推送、回退具备 default/hover/focus/disabled/loading/error 状态。
- 回退按钮使用危险色，但普通状态和 Git/SVN 类型不滥用红色。
- 操作进行中锁定会改变目标集合的编辑动作；仍允许浏览状态和查看日志。
- 所有关键文案提供简体中文和英文；沿用当前 `lang` / `isZh` 模式，后续可迁入统一 i18n。

## 8. 错误模型与审计

后端将错误归类为稳定 code，前端按 code 给出可执行建议：

- `client_not_found`
- `path_invalid` / `path_outside_root`
- `not_working_copy`
- `remote_mismatch`
- `credential_missing` / `credential_unavailable`
- `authentication_failed`
- `working_copy_locked`
- `conflict_detected`
- `nothing_to_commit`
- `push_rejected`
- `timeout` / `cancelled`
- `command_failed`

日志只记录 job ID、仓库 ID、操作类型、耗时、退出码和脱敏摘要。不得记录密码、完整带查询参数 URL、askpass 环境内容。产品层结果保留在内存；若后续要求落盘审计，应另设保留期和“清除历史”能力。

## 9. 测试计划

### 9.1 Go 单元测试

- 配置 CRUD、原子写入、损坏文件不被静默覆盖。
- `.vrepo` 自动发现、manifest 版本校验、根目录整体移动后重新打开、本机索引丢失后的恢复。
- 树校验：重复 ID、孤儿、环、非法名称、稳定排序。
- 路径校验：`..`、绝对路径、Windows 卷、大小写、符号链接逃逸、重复规范化路径。
- 凭据元数据 CRUD、同类型筛选、引用计数、删除引用处理。
- secret store 使用 fake 实现验证 secret 不进入配置 JSON、返回结构或日志。
- Git/SVN 命令参数生成、超时、取消、输出截断和脱敏。
- SVN 客户端发现顺序、常见位置去重、版本验证超时、用户路径优先、指定路径失效后的重新搜索。
- 操作目标快照、串行顺序、部分失败、跳过禁用节点、只重试失败项。

### 9.2 适配器集成测试

- 使用临时目录创建本地 Git bare remote + 两个工作副本，覆盖 status、commit、push、push rejected、revert。
- 对 Git adapter 增加 go-git/CLI 结果一致性测试，并覆盖 submodule、LFS、worktree 等回退到 CLI 的判定。
- 若 CI 安装了 SVN，则以临时 `svnadmin create` 仓库覆盖 checkout、status、commit、revert；未安装时明确 skip。
- 认证通过 fake askpass/临时认证端点验证，断言进程参数和错误输出不包含 secret。
- Windows 重点验证路径与进程取消；macOS/Linux 验证 keychain/secret service 可用性探测。

### 9.3 前端测试

- 首页显示虚拟仓库卡，点击进入工作台，返回不破坏其他 Utilities 状态。
- 空状态创建、根目录选择取消、必填和重复名称校验。
- 树的创建/移动/删除、键盘操作和子树目标计算。
- 同类型凭据筛选、秘密不回显、删除被引用凭据的确认。
- 混合 Git/SVN 操作预览、SVN push 跳过说明。
- 回退二次确认、运行中禁用、取消、部分失败和重试失败项。
- Wails binding 不存在或调用失败时显示可恢复错误，不白屏。

### 9.4 手工验收矩阵

| 场景 | Windows | macOS | Linux |
|---|---:|---:|---:|
| Git HTTPS + 保存凭据 | 必测 | 必测 | 必测 |
| Git SSH + agent | 必测 | 必测 | 必测 |
| SVN HTTPS + 保存凭据 | 必测 | 必测 | 必测 |
| SVN 自动发现/手工指定/路径失效恢复 | 必测 | 必测 | 必测 |
| 根目录含中文/空格 | 必测 | 必测 | 必测 |
| 混合 5+ 仓库部分失败 | 必测 | 抽测 | 抽测 |
| 回退保留未跟踪文件 | 必测 | 必测 | 必测 |

## 10. 分阶段开发计划

### Phase 0：安全与命令可行性 Spike

- 验证三平台系统安全存储选型和失败降级行为。
- 验证 Git askpass 与 SVN 非交互认证方案不会把 secret 暴露到命令行和日志。
- 固化“回退”语义和 Git/SVN 命令矩阵。
- 产出：最小验证程序、威胁检查结果、最终依赖选择。

退出条件：没有可接受的 secret 存储或 SVN 认证方案时，不进入保存密码功能开发；允许先交付不保存密码的会话模式。

### Phase 1：定义与只读工作台

- 新增 Utilities 卡片、页面路由与独立工作台组件。
- 完成 `.vrepo/manifest.json` 创建/发现、虚拟仓库/树节点 CRUD、本机最近打开索引、目录选择、配置存储和路径校验。
- 完成 Git/SVN 客户端探测、工作副本识别、状态刷新和远端校验。
- 完成空状态、错误状态、树键盘操作和基础测试。

退出条件：用户可以稳定建立混合仓库树，并看到每个仓库的真实状态；不含任何写操作。

### Phase 2：凭据管理

- 实现 secret store 抽象与选定平台适配。
- 完成凭据新增、修改、删除、引用计数与同类型筛选。
- 接入 Git/SVN 非交互认证并完成 secret 泄露测试。

退出条件：密码不进入配置文件、返回结构、命令日志和进程参数；安全存储不可用时行为清晰可控。

### Phase 3：提交与推送

- 实现预检/确认/job/progress/results 通用链路。
- 实现单仓库、子树和整体提交；实现 Git push、提交并推送；明确 SVN 行为。
- 实现取消、部分失败、仅重试失败项和结果脱敏。

退出条件：混合 5 个仓库的任务能正确报告逐项结果，某一项失败不阻断结果收集且不会误报整体成功。

### Phase 4：安全回退与收尾

- 实现只回退已跟踪未提交改动，保留未跟踪文件。
- 增加冲突/中间态阻断、二次确认、危险操作测试。
- 完成多平台手工验收、文案、性能与可访问性复核。

退出条件：回退目标、不可恢复影响和保留内容在预览/确认/结果三处一致；通过发布验收矩阵。

## 11. 交付清单

- Go 配置、凭据、路径、VCS adapter、job runner 与 Wails API。
- React 工作台、树、编辑器、凭据管理器、操作确认与结果面板。
- Git/SVN 集成测试与前端 Vitest。
- 中英文文案和用户帮助说明。
- 安全说明：密码存储位置、SSH 凭据边界、回退语义、批量操作非事务性。
- 构建后重新生成/校验 Wails bindings，并执行前端 `npm test`、`npm run build` 和相关 Go tests。

## 12. 待产品确认项

以下问题不阻塞 Phase 0/1，但必须在写操作进入实现前确认：

1. “revert”是否确认指**丢弃未提交改动**；若还需要撤销历史提交，应作为另一个明确动作。
2. 已确认：目录不存在或为空时，由用户明确点击后执行 Git clone / SVN checkout；branch/tag 留空时使用主分支/缺省分支。
3. Git 提交是否默认包含所有未跟踪文件。本规划默认 `git add -A`，确认页会明确展示。
4. Linux 系统安全存储不可用时，是只允许会话密码，还是要投入开发带主密码的加密文件降级。
5. 是否允许用户开启并行执行；本规划默认串行，先保证结果可解释和认证稳定。

## 13. 验收标准

- 用户可从实用工具进入虚拟仓库工作台并返回。
- 新建虚拟仓库后，根目录存在 `.vrepo/manifest.json`；复制或移动整个根目录后仍可重新打开并恢复目录与映射关系。
- 用户可创建带根目录的虚拟仓库和任意层级目录树，添加 Git/SVN 仓库且不能越出根目录。
- Git/SVN 节点可选择 branch/tag；留空后检出远端缺省分支/原始 SVN URL，指定后检出对应版本；Git tag 写操作被明确阻止。
- 用户可添加映射到真实路径的纯本地目录，用于编译结果等内容；版本控制操作会明确跳过该节点且不修改其中内容。
- 用户可维护仓库凭据；新建同类型仓库能选择“使用保存的密码”；秘密不回显、不落普通配置、不出现在日志或命令行。
- 用户可对根、子树或单仓库刷新、提交、Git 推送、提交并推送，并获得逐仓库结果。
- SVN 在推送语义上不会误导：commit 即上传，单独 push 明确不可用或被跳过。
- 用户可在强确认后回退已跟踪未提交改动，未跟踪文件保持不变。
- 混合批量任务发生部分失败时显示 `partial_success`，保留完整逐项结果并可只重试失败项。
- 删除虚拟仓库或节点绝不删除真实文件；删除凭据不会删除仓库定义。
- Git/SVN 客户端缺失、认证失败、路径失效、冲突与超时都有稳定、可操作的错误提示。
