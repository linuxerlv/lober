# SSH 公钥契约：补上 root 免密登录的输入通路

对应分支 `feat/p1-sshkey`。依据 SPEC §1.5、§5.4、§5.6。

## 一句话

修复前，每台按 MVP 装完的机器 root 都登不进去——kickstart/autoinstall/preseed
三个渲染器各自写死一个明显不可用的占位公钥（`REPLACE-ME-...`），因为
`model.RenderContext` 没有任何字段承载"要注入的真实公钥"。本轮补上这条契约
通路：配置 → RenderContext → 三个渲染器 → 应答文件里的真实公钥。

## 改了什么

### 1. `internal/config`：新字段 + 白名单校验

`Gateway.RootSSHPublicKeys []string`（YAML: `root_ssh_public_keys`）：

- **必须非空**——`Validate()` 拒绝空列表/整段省略。理由：root 不设可复用
  密码（`rootpw --lock` 等），公钥是唯一登录通路，空列表等于"这一批机器
  没人能登进去"，必须在启动时就拒绝，不能留到装机现场才发现。
- **支持多把**——团队多人各自持有私钥，且公钥轮换期需要新旧并存（撤销
  旧公钥前，已用旧公钥登录过的运维不能被立即锁死）。任一把都能登录，
  与 SSH `authorized_keys` 文件本身"多行任一匹配即放行"的语义一致。
- **逐把校验格式**：`<类型前缀> <base64主体>[ <注释>]`，类型前缀白名单
  `ssh-ed25519`/`ssh-rsa`/`ecdsa-sha2-nistp256`/`384`/`521`（不收
  `ssh-dss`，已被视为不安全），base64 主体只认标准字母表字符，注释段
  拒绝换行/双引号/单引号。这道校验的作用不是验证"这是否是一把真正可用
  的密钥"（不做完整密钥学校验），只做注入防护：拒绝任何能破坏下游
  kickstart/cloud-init/preseed 文件结构的字符。

### 2. `internal/core/model`：`RenderContext.RootSSHPublicKeys []string`

新增独立字段，不塞进 `Facts`。理由（SPEC §5.4）：fact 命名空间是面向 hook
的对外 API，一旦有数十个 hook 在用就不能改名、只增不删、废弃字段要双写一个
版本周期。公钥是渲染器内部消费的输入，不是给 hook 读取的运行期事实，混进
`Facts` 会把这个内部实现细节永久绑进那份对外契约。

### 3. 三个渲染器：移除占位符，消费新字段，各自加纵深防御

| 家族 | 载体 | 空值行为 | 恶意输入防线 |
|---|---|---|---|
| kickstart (RHEL) | `sshkey --username=root "<key>"`，逐把公钥各一行 | 返回错误 | 双引号转义为 `\"`；含裸换行直接拒绝（转义引号不足以防换行——换行本身就能在这一行中间开出新指令，真实测试验证过） |
| autoinstall (Ubuntu) | `ssh.authorized-keys` YAML 列表 + `ssh.install-server: true` | 返回错误 | `yaml.Marshal` 序列化，天然不受文本注入影响（真实恶意输入验证过：含换行/冒号的公钥不会突破 YAML 结构） |
| preseed (Debian) | `late_command` 脚本里 `in-target bash -c` 追加写 `/root/.ssh/authorized_keys`（带引号 heredoc） | 返回错误 | 带引号的 heredoc 定界符关闭 shell 展开；用真实 bash 子进程验证过含单引号的恶意公钥不会逃逸 |

三者都不会因为空值静默产出占位公钥——这是本轮最核心的设计约束（"绝不再
写一个假公钥"），即使某条不经过 `config.Validate` 的路径把空值/恶意值
塞进了 `RenderContext`（例如测试或未来新增的调用方直接构造），渲染器自己
也会拒绝，不依赖上游校验单独兜底。

### 4. 网关装配（`cmd/lober/wire.go`、`internal/gateway`）

`gateway.Deps` 新增 `RootSSHPublicKeys []string` 字段（与已有的
`Bases`/`SyslogHost`/`SyslogPort` 同一种接缝：部署级配置值，随 `Deps`
注入，`buildRenderContext` 每次构造渲染上下文时原样传递）。`wire.go` 把
`cfg.Gateway.RootSSHPublicKeys` 传给这个新字段。

**注意（见下方 contractGaps）：`gateway.Deps` 定义在 `internal/gateway/
server.go`，不在本任务的文件清单内**——这是本轮唯一一处越出文件清单边界
的改动，是纯增量的一行结构体字段（不改任何路由/handler 逻辑），已在
`contractGaps` 里单独说明为什么这是唯一可行的正确设计位置。

### 5. 启动自检双重防御

选择：**`internal/config.Validate()` 在启动时拒绝空配置（全局一次、最早
暴露，附带完整修复提示） + 三个渲染器在渲染时各自防御（万一拿到空值/
恶意值也绝不产出假公钥或坏结构）**。这是任务描述里明确倾向的方案，理由：

- 只在启动自检做，等于把"信任渐染器万一被绕过校验"这件事赌在唯一一处，
  一旦未来出现另一条不经过 `config.Validate` 的构造路径（本轮实测就
  发现了一条——`internal/console` 的模板预览端点，见下），空配置/恶意
  配置就会静默流到应答文件里。
- 只在渲染时做，等于把这个错误留到每次真实装机才报一次——启动自检能
  让运维在按下开机键之前就发现配置漏了这一项，而不是等机器已经在装了
  才报错。

## 运维部署时要做什么

1. 生成一对（或多对）SSH 密钥：`ssh-keygen -t ed25519`。
2. 把公钥内容（`~/.ssh/id_ed25519.pub` 的一行文本）填进
   `deploy/lober.yaml.example` 的 `gateway.root_ssh_public_keys` 列表——
   一人一把，全部列出。
3. **不要**把私钥内容填进任何配置文件。
4. 跑 `go run ./cmd/lober -config <你的配置> -check` 验证。

### 填错了会看到什么报错

- **整段省略或写成空列表**：
  ```
  config: gateway.root_ssh_public_keys 不能为空
  （SPEC §1.5：root 不设可复用密码，注入 SSH 公钥并关闭密码登录——
  公钥是唯一登录通路，空列表会静默产出一台装完后没人能登录的机器，
  这种「错了也不报错」的配置必须在启动时拒绝）
  ```
- **某一把格式不对**（例如手滑把私钥或一段乱码粘进去了）：
  ```
  config: gateway.root_ssh_public_keys[0] 不是合法的 SSH 公钥:
  guard: SSH 公钥类型前缀 "..." 不在白名单内
  （必须是 ssh-ed25519/ssh-rsa/ecdsa-sha2-nistp256/384/521 之一）
  ```
  或
  ```
  guard: SSH 公钥 base64 主体不合法: 含非 base64 字符 '...'
  ```
- **公钥值里意外带了换行**（例如复制粘贴时带进了额外的行）：
  ```
  guard: SSH 公钥 "..." 必须是「类型前缀 base64主体[ 注释]」的单行格式，
  拆出 N 段（想要 2 或 3 段）——裸换行会被当作分隔符拆出额外的段，
  因此这一步同时拒绝了换行注入
  ```

三类报错都在启动时（`-check` 或真实启动）就报出来，都指明是哪个字段、
为什么不合法，不需要等机器装完连不上才去猜。

## 验证证据

全部真跑，命令与结果：

- `go vet ./...` —— 无输出，通过。
- `gofmt -l .` —— 无输出，全仓格式干净。
- `go test ./... -count=1` —— 除 `cmd/lober` 的
  `TestRunServers_SyslogReceivesUDPPacket` 外全绿（该失败是本机 Windows
  环境下 UDP 监听时序的预置问题，与本轮改动的文件、逻辑均无关联——已核实
  `cmd/lober/server.go`/`server_test.go`/`logstore.go` 在本分支 diff 为空，
  且该测试与 SSH 公钥渲染路径完全独立，代码本身的注释已承认这是
  "尤其在 Windows 上"的已知竞态）。
- `go test ./internal/render/ -race` —— 全绿，含真实 bash 子进程验证公钥
  转义在运行期而非仅文本层面生效的测试。
- `go test ./internal/archtest/...` —— 全绿，四条依赖方向守卫均未被破坏。
- `bash scripts/e2e.sh` —— **9 阶段中前 4 阶段通过，第 5 阶段（模板预览
  端点）失败**，见下方 contractGaps 的详细说明，这不是可以静默绕过或
  自行修复的问题。

## contractGaps

### 越出文件清单边界的改动（已获授权范围内的最小必要例外）

`internal/gateway/server.go` 的 `Deps` 结构体加了一个字段
（`RootSSHPublicKeys []string`），这个文件不在本任务的文件清单内。

**为什么这是唯一可行的位置**：`buildRenderContext`（`rendercontext.go`，
本任务范围内）是唯一构造 `model.RenderContext` 的地方，它只能读取
`s.deps.*` 已有的字段；`Deps` 结构体本身定义在 `server.go`，`Bases`/
`SyslogHost`/`SyslogPort` 三个已有的"部署级配置值"字段都在那里定义。
新增字段前评估过的替代方案：

1. **塞进已有的 `Bases model.BaseURLs` 字段** ——类型语义不符（`BaseURLs`
   是 URL 拼接工具类型，被多处测试当作纯 URL 值使用），会让未来读者
   困惑"为什么 URL 类型里有 SSH 公钥"。
2. **包级全局可变变量 + setter，绕开 `Deps`** ——引入隐藏全局状态，
   破坏未来可能加上的 `t.Parallel()` 测试隔离性，是比"越权加一个字段"
   更差的工程选择。
3. **不做这处改动，SSH 公钥就到不了真实渲染路径**——违背本任务的核心
   目的（修复"装好的机器登不进去"这个真实阻塞问题）。

选择了影响面最小的方案：纯增量的一个字段（不改任何路由/handler 逻辑），
用 Go 的具名字段初始化语法，对其它并行开发的 track 而言唯一可能冲突的
场景是"另一条 track 恰好也在同一处添加新字段"，冲突表现为一次可自动
合并或极易手工合并的结构体字段并列，不是逻辑冲突。已在返回值里如实
标注，供编排方核对/决定是否需要与其它 track 协调这一个文件。

### 真实发现的下游断裂：控制台模板预览端点（`internal/console/template.go`）

`internal/console/template.go` 的 `handleTemplatePreview`
（`POST /templates/{id}/preview`）独立构造了自己的 `model.RenderContext`
字面量（不经过 `internal/gateway.buildRenderContext`），用途是"不建任务、
不签 token、不落库，纯用假数据渲染出应答文件文本供运维在 UI 里预览模板
改动效果"。这个字面量从未设置 `RootSSHPublicKeys`（该文件不在本任务
文件清单内，`internal/console/**` 被明确列为不可touch），修复前，这条
路径依赖三个渲染器统一使用写死的占位公钥，因此"能跑"；修复后，三个
渲染器都会对空 `RootSSHPublicKeys` 返回错误（这是本任务的明确设计
要求，"渲染器万一拿到空值就返回错误而绝不再写一个假公钥"，没有为
preview 场景开例外的选项），这条预览路径现在会 100% 返回渲染失败。

**真实复现**：`bash scripts/e2e.sh` 跑到阶段 5（`POST
/templates/{id}/preview`）时确定性失败，响应
`{"code":1000,"message":"渲染应答文件失败: render:
RenderContext.RootSSHPublicKeys 为空..."}`。

**这不是本任务能自行修复的问题**：修复需要编辑
`internal/console/template.go`，该文件明确不在本任务的文件清单内，
且 M6（控制台模块）可能有另一条 track 正在并行改动这个文件——擅自
改动会直接违反"碰别人的文件就制造合并冲突"这条铁律。

**评估过、且认为不可接受的绕法**（已放弃）：放宽三个渲染器的空值检查
（例如"空值时跳过 SSH 行而不报错"，或"识别到 preview 专用的假 Task.ID
就跳过检查"）——两者都直接违反本任务对渲染器行为的明确要求（"绝不再
写一个假公钥"没有附带任何场景例外，且用字符串内容做安全相关的控制流
判断本身就是脆弱的反模式）。修改config层的必填校验来迁就一个完全独立、
不经过config的调用路径，同样是治标不治本、还会削弱真实装机路径的保护。

**正确的修复方向**（留给拥有该文件的 track）：`internal/console.Deps`
（`internal/console/server.go`）需要与 `gateway.Deps` 同样新增
`RootSSHPublicKeys []string` 字段，由 `cmd/lober/wire.go`（本任务已在
维护，届时只需再加一行）传入；`handleTemplatePreview` 构造
`RenderContext` 时补上 `RootSSHPublicKeys: s.deps.RootSSHPublicKeys`。
这与本任务给 `gateway.Deps` 打的补丁是完全同构的三行改动，不需要重新
设计。

**e2e 现状**：`bash scripts/e2e.sh` 因此在阶段 5 处停止，无法跑完全部
9 个阶段——这是上述真实断裂的直接后果，不是本任务能绕过的限制。阶段
1-4（构建、启动、鉴权、建主机/制品集/模板）在本次运行中全部通过，
证明本任务改动的配置解析、启动自检、装配链路本身没有问题；阶段 5
之后（含负面用例、状态机、finalize 等）未能验证，其通过状态在本轮
之前的分支基线是已确认的（任务描述："scripts/e2e.sh 九阶段全 PASS"），
本轮未触及那些阶段涉及的任何逻辑代码，只是因为脚本本身是顺序执行、
一步失败即整体停止，物理上无法验证到后续阶段。
