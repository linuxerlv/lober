# dev 分支代码架构与设计审查报告

- **审查日期**：2026-10-21
- **审查对象**：`dev` 分支最新代码（commit `f5c8ee9`，工作树 `.worktree/rbac-integration`）
- **审查范围**：`internal/`、`cmd/`、`web/`
- **审查方法**：静态代码审查 + 软件工程理论分析（SOLID、Clean Architecture、DDD、算法复杂度、并发安全、防御性编程）

---

## 一、架构分层与依赖管理（SOLID 与整洁架构违背）

### 1.1 缺失应用服务层（Application Layer）与工作单元（Unit of Work），导致“胖控制器”与“装配点膨胀”

- **代码位置**：
  - `internal/console/wizard.go:107-133, 259-466`
  - `internal/console/isos.go:180-276`
  - `cmd/lober/deletion.go:21-222`
  - `cmd/lober/supervisor.go:16-226`
  - `cmd/lober/machine.go:69-99`
  - `internal/store/pgstore/hooks.go:124-129`
  - `internal/store/pgstore/templates.go:185-205`
  - `internal/store/pgstore/sources.go:102-133`

- **为什么不合理（理论与后果）**：
  1. **违反整洁架构（Clean Architecture）与单一职责原则（SRP）**：`internal/archtest` 禁止平级业务包（`console`、`iso`、`execution`、`events`）互相引用，但没有设计独立的 **Application Service / UseCase 层**。结果导致职责向两端挤压：
     - **向上挤压到 HTTP 控制器（`console`）与 `main` 装配点（`cmd/lober`）**：`cmd/lober` 作为 Composition Root（装配根），本应只负责依赖注入与生命周期启停，现在却塞入了 220 行的 WAL 删除日志状态机（`deletionJournal`）、220 行的指数退避并发工作池（`sourceSupervisor`）、ESXi `boot.cfg` 按需加载逻辑及 Syslog 租约时长策略（`life = 5400 / 86400`）。
     - **向下挤压到持久化适配器（`pgstore`）**：由于 `store` 契约层拒绝向上暴露事务（Unit of Work），所有跨实体业务校验（如 `ApproveTemplate` 激活重判、`CreateTasks` 准入重判、`saveSource` 里判断主/副介质状态以决定是否把失败的 ISO 重新置回 `acquiring`）都被迫写进 `pgstore` 的 SQL 事务内部，使存储层退化为“带业务编排的事务脚本”。

- **改进方法**：
  1. 在 `store` 层引入轻量级 **Unit of Work**（如 `InTx(ctx, func(tx Store) error)`），将事务边界控制权交还给应用层，保持 `pgstore` 只做纯数据映射与并发锁控制。
  2. 将 `cmd/lober/deletion.go` 和 `cmd/lober/supervisor.go` 下沉到 `internal/iso`（或独立的 `internal/app/iso` 应用服务包），让 `cmd/lober` 回归纯粹的 Wire 装配与进程启停。

---

### 1.2 违反依赖倒置（DIP）与开闭原则（OCP）：第 1 层领域模型沦为“全栈错误码与文案上帝表”

- **代码位置**：
  - `internal/core/model/codes.go:274-277, 383-499`
  - `internal/core/model/registry.go:23, 160`
  - `internal/core/model/authoring.go:102, 148`
  - `internal/core/model/task.go:72`
  - `internal/store/users.go:18-45, 58-79`

- **为什么不合理（理论与后果）**：
  1. **语义级反向依赖（违反 DIP / Common Closure Principle）**：虽然 `archtest` 保证了 `internal/core/model` 在语法上不 `import` 外层包，但 `model/codes.go` 却集中定义了第 2 层（`store` 批量错误）、第 3 层（`render` 渲染错误）、第 4 层（`iso` WIM 解析/SSRF/上传错误）、第 5 层（`console` HTTP 401/403/500 状态文案、CSV 表头错误、登录会话文案）和第 7 层（`execution` 控制状态）的所有常量与中文 UI 文案。任何外层新增一个 HTTP 报错或 CSV 校验规则，都必须修改最内层的 `core/model`。
  2. **领域模型分裂与持久化概念泄漏**：`Host/ISO/Hook/Template/Task` 放在 `core/model` 中且不带 `json` 标签，却全部混入了仅供数据库乐观锁使用的 `Rev int64`（导致 `pgstore/hooks.go:332` 在比对内置 Hook 是否变更时不得不硬编码 `h.Rev = 1` 再调 `reflect.DeepEqual`）；而同属核心领域的 `User`、`Role`、`RoleMenuSettings` 却定义在 `internal/store/users.go` 中并直接打上了 HTTP 层的 `json:"..."` 标签。

- **改进方法**：
  1. `core/model` 只保留领域层自身的 `Problem` 结构与核心领域错误码；各层（`iso`、`render`、`console`、`execution`）在各自包内定义并注册本层的错误码，并通过统一翻译器（i18n / Error Catalog）在边界层渲染文案。
  2. 将 `User`/`Role` 领域值对象迁入 `core/model`（移除持久化层上的 HTTP `json` 标签），或将 `Rev` 提取为存储层的版本包装类型 `Versioned[T]`。

---

### 1.3 违反开闭原则（OCP）：散落各层的 OS 格式 `switch/if` 霰弹式修改（Shotgun Surgery）及引发的跨层 Bug

- **代码位置**：
  - `internal/render/menu.go:25-94`
  - `internal/render/answer.go:29-33, 67-226, 281-329`
  - `internal/render/script.go:28-242`
  - `internal/render/aux.go:15-33`
  - `internal/builtin/builtin.go:33-127`
  - `internal/iso/identify.go:47-195`
  - `internal/iso/extract.go:31-147`
  - `internal/iso/suse.go:19-94`

- **为什么不合理（理论与后果）**：
  1. **违反 OCP 与控制耦合（Control Coupling）**：系统支持 7 种自动安装格式（`kickstart`、`preseed`、`autoinstall`、`autoyast`、`agama`、`unattend`、`esxi-ks`）。代码没有将“各格式的行为差异”抽象为多态接口（Strategy Pattern），而是散落在 `catalog`、`iso`、`builtin`、`render` 的数十个 `switch format` 和 `if format == ...` 中。更糟的是，`internal/render/script.go:28` 的 `posixRuntime(r Request, slot model.SnapshotSlot, windows bool)` 用布尔标志位把 Linux、ESXi、Windows 三种运行时塞进同一个 150 行函数，内部包含 **11 处 `if windows` / `if esxi` 分支**，属于教科书式的控制耦合反模式。
  2. **缺乏编译器约束已导致多处跨层契约不一致 Bug**：
     - **ESXi (`esxi-ks`) 导入链路完全断裂**：`iso/identify.go:155-157` 在 `ProbeBootCfg` 分支下未给 `out.Version` 赋值，紧接着第 189 行 `!identifyVersion.MatchString(out.Version)` **无条件拒绝所有 ESXi ISO**；`iso/extract.go:31-147` 的 `switch found.Format` 根本没有 `"esxi-ks"` 分支（直接落入 `default` 报错）；而 `cmd/lober/bootconfig.go:21` 读取 `s.Artifacts["repo"]`，但 `catalog/media.go:83` 和 `render/aux.go:50` 定义的键名却是 `"esxi-tree"`。
     - **Agama (`sles16`/`leap16`) 模板无法通过准入**：`builtin/builtin.go:108` 为 `builtin-agama` 声明了 `Offer: "products"`，但 `iso/identify.go` 与 `iso/suse.go` 从未向 `iso.Offers["products"]` 写入数据，导致 `admission.TemplateUsable` 永远报错 `param_offer_empty`。
     - **Windows (`unattend`) 版本号契约错位**：`iso/identify.go:181` 将 `Editions` 的 `Value` 存为镜像名 `image.Flags`（如 `"ServerStandard"`），但 `builtin/unattend/answer.xml:3,15` 将 `{{param:edition}}` 填入了 `<Key>/IMAGE/INDEX</Key>`（Windows 安装器要求填 1-based 数字索引 `image.Index`）。
     - **`render/menu.go:44-56` 与 `catalog.RequiredRoles` 脱节**：`menu.go` 手写了一遍各格式必需的制品角色（ESXi 只查 `mboot` 漏了 `esxi-tree`，Debian 只查 `kernel/initrd` 漏了 `repo`），而没有调用 `catalog/media.go:69` 已有的 `catalog.RequiredRoles`。

- **改进方法**：
  1. 定义统一的 `FormatAdapter` / `FormatStrategy` 接口（包含引导菜单生成、应答文件组装、运行时脚本生成、媒体识别与提取、必需制品角色声明），每种格式一个独立实现结构体并注册到全局表，用编译期接口实现检查取代散落的 `switch`。
  2. 消除 `posixRuntime(..., windows bool)` 的布尔控制耦合，拆分为独立的 POSIX/Linux、ESXi、Windows 运行时生成器。

---

### 1.4 违反接口隔离（ISP）、单一职责（SRP）与里氏替换原则（LSP）

- **代码位置**：
  - **ISP**：`internal/store/store.go:10-23, 80-148`；`internal/iso/iso.go:23`；`cmd/lober/deletion.go:23`；`cmd/lober/supervisor.go:18`
  - **SRP**：`internal/store/profile.go:16-57`；`internal/console/server.go:43-53`
  - **LSP**：`cmd/lober/taskcontrol.go:11-20`；`cmd/lober/logging.go:13-51`

- **为什么不合理（理论与后果）**：
  1. **ISP 违背**：`store.Store` 聚合了 10 个子接口共 ~55 个方法。尽管注释要求调用方只声明窄接口，`iso.Sources`、`deletionJournal`、`sourceSupervisor` 依然直接依赖了胖接口 `store.Store`；同时 `store.Hooks`（13 个方法）与 `store.Templates`（14 个方法）将“只读查询”、“草稿编辑”、“审批状态流转”、“启动期内置种子同步（`EnsureBuiltin*`）”四个不同角色的职责混在一个接口中。
  2. **SRP 违背**：`internal/store/profile.go` 在存储层契约包里引入了 `image`、`image/png`、`image/jpeg`、`encoding/base64`、`net/mail`，直接在数据库契约层做 Data-URI 解析与 PNG/JPEG 像素尺寸解码；`console.server` 单个结构体挂载了 ~50 个方法，横跨认证、会话、RBAC、CSV 解析、分片上传、向导编排、SSE 推送、OpenAPI 反射生成。
  3. **LSP 违背**：当 `cfg.Machine.Enabled == false` 时注入的 `taskControlStub`（`cmd/lober/taskcontrol.go:19-20`）在 `CanCancel` / `CanRetry` 中返回 `nil`（表示**允许**取消/重试，哪怕任务已经 `done`），但前端一旦据此展示按钮并调用 `Cancel` / `Retry`，却永远返回 `ErrTaskControlUnavailable`（422），前置判定与执行后置条件自相矛盾。此外 `loggingStore`（`cmd/lober/logging.go:13`）作为全局 `store.Store` 装饰器，只拦截记录了 4 个方法，其余 40+ 个仓储写操作完全无日志。

- **改进方法**：
  1. 拆分 `Hooks`/`Templates` 为 Query、Authoring、Approval、Seeder 窄接口；将 `iso.Sources` 等调用方改为声明所需的窄接口。
  2. 将头像图片解码与 Data-URI 校验移至应用/输入校验层，存储层只校验字节长度上限。
  3. 修复 `taskControlStub.CanCancel/CanRetry`，当机器网关禁用时返回明确的 `task_control_unavailable` 问题项，保证 `CanX() == nil` 与 `X()` 可执行性的契约一致。

---

## 二、并发控制、事务边界与分布式一致性设计缺陷

### 2.1 用进程内 `sync.RWMutex` 替代数据库事务锁来维护数据不变量（TOCTOU 与水平扩展失效）

- **代码位置**：
  - `internal/console/server.go:51`（`templateSelection sync.RWMutex`）
  - `internal/console/wizard.go:280-296, 366-367, 412-413, 454-456`
  - `internal/console/lifecycle.go:130-131, 143-144`
  - `internal/store/pgstore/tasks.go:112-122`

- **为什么不合理（理论与后果）**：
  - 领域规则 C23 要求“只能用模板当前的已审批版本（`CurrentApproved`）创建任务”。但 `pgstore.CreateTasks` 在数据库事务里只锁了指定的 `(template_id, template_version)` 单行并检查 `State == Approved`，**没有检查该模板是否已有更新的已审批版本**。
  - 为了补这个漏洞，代码在 HTTP 层 `console.server` 里加了一把全局内存锁 `templateSelection sync.RWMutex`：创建任务前拿 `RLock` 查库算 `CurrentApproved`，审批/废弃模板时拿 `Lock`。
  - **后果**：① 在持有进程内锁期间执行了多达 6 类数据库网络 I/O（`GetTemplate`、`ListTemplateVersions`、`GetISO`、`GetHooks`、`GetHosts`、N 次 `FindOpenTask`、`CreateTasks`），严重阻塞所有其他模板的审批操作；② 一旦部署多个 `lober` 实例，进程内内存锁完全无法阻止跨实例的并发冲突，数据库不变量被击穿。

- **改进方法**：
  - 删除 `console.server.templateSelection` 内存锁；在 `pgstore.CreateTasks` 事务中对父表 `templates` 的 `id` 行加 `FOR SHARE` 锁（或在查询 `template_versions` 时校验 `version == MAX(version) WHERE state = 'approved'`），将 C23 不变量收归数据库事务内。

---

### 2.2 全表锁（`LOCK TABLE users`）滥用：普通用户改个人资料/改密码都会锁死整张用户表

- **代码位置**：
  - `internal/store/pgstore/users.go:69`
  - `internal/console/profile.go:36`
  - `internal/console/password.go:55`

- **为什么不合理（理论与后果）**：
  - 为了防止并发事务同时禁用或降权最后两名管理员，`pgstore.UpdateUser` 在事务第一行**无条件执行**：
    ```sql
    LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE
    ```
  - 由于普通用户修改个人资料（`PUT /console/v1/user/profile`）和修改个人密码（`POST /console/v1/auth/password`）底层也调用 `UpdateUser`，导致**任何一个 `viewer` 修改昵称、邮箱、头像或密码，都会对整张 `users` 表加表级排他写锁**，阻塞全站所有并发的创建用户、修改资料和改密请求。

- **改进方法**：
  - 移除无条件的 `LOCK TABLE users`。先对目标用户行执行 `SELECT ... FOR UPDATE`；仅当检测到目标用户当前是启用状态的 `admin` 且本次操作将其降权或禁用（`current.Role == 'admin' && !current.Disabled && (u.Role != 'admin' || u.Disabled)`）时，按主键顺序对所有启用中的管理员行加行锁（`SELECT username FROM users WHERE role = 'admin' AND disabled = false ORDER BY username FOR UPDATE`）并检查数量 `> 1`。

---

### 2.3 删除门禁（`registrationGate`）并发逃逸与多步写操作缺乏原子性

- **代码位置**：
  - `cmd/lober/deletion.go:104-145` vs. `cmd/lober/supervisor.go:145-150, 191-223`
  - `internal/console/isos.go:239-252`
  - `internal/iso/import.go:333-352`

- **为什么不合理（理论与后果）**：
  1. **删除门禁逃逸**：`deletionJournal.Delete` 在删除 ISO 时获取 `j.gate.Lock()`，随后清理磁盘并删除日志。但在 `sourceSupervisor` 中，只有 `Register` 获取了 `s.gate.RLock()`；**正在执行分片上传的 `Upload`、重试的 `Retry`/`Reimport` 以及后台正在写磁盘的 `FetchOne`/`Import` 协程均未与 `gate` 同步**，Delete 也没有先 Cancel 并等待该 ISO 的在途 worker 退出。在途 worker 可以在 `CleanupDeleted` 和 `discard` 完成后继续向磁盘写入文件，造成永久性的孤儿文件残留；同时已取消/删除的 ISO 键还会永久残留在 `s.failures` 和 `s.retryAt` map 中造成内存泄漏（`supervisor.go:29-30, 177-178`）。
  2. **非原子创建**：`console.createISO`（`isos.go:239-252`）先提交 `CreateISO`，再在循环里逐个调用 `ISOSource.Register`。若第二个来源 URL 校验或 DNS 解析失败，数据库中会残留一条半成品的 `acquiring` ISO 记录。

- **改进方法**：
  1. 在 `deletionJournal.Delete` 删除前，必须通知 `sourceSupervisor` 取消并等待该 `isoID` 关联的所有在途 worker 和 `Upload` 结束，同时清理 `s.failures[key]` / `s.retryAt[key]`。
  2. 将 ISO 记录创建与初始来源登记合并进同一个数据库事务。

---

### 2.4 部分写接口绕过或缺失乐观并发控制（OCC `Rev`）

- **代码位置**：
  - `internal/console/users.go:139-157`（`disableUser`）
  - `internal/store/store.go:104, 142`（`RenameHook`、`RenameTemplate`）
  - `internal/store/pgstore/sources.go:219-239`（`DeleteSource`）

- **为什么不合理（理论与后果）**：
  - `disableUser`（`DELETE /console/v1/users/:username`）不接收客户端 `rev`，而是先 `GetUser` 读出最新 `u.Rev`，再原地传给 `UpdateUser`——这把乐观锁退化成了盲写（Blind Write），会静默覆盖另一位管理员刚刚并发修改的角色或属性。
  - `RenameHook` / `RenameTemplate` 的身份表没有 `rev` 字段，并发重命名存在静默覆盖（Lost Update）；`DeleteSource` 既不校验 `rev`，也没有像 `saveSource` 那样先锁父表 `isoRow` 检查是否已 `deprecated`。

- **改进方法**：
  - `DELETE /console/v1/users/:username` 和 `DeleteSource` 补充 `rev` 查询参数（与其他 `DELETE` 接口保持一致）；在 `hooks` / `templates` 身份表引入 `rev` 或统一递增版本 `rev`。

---

## 三、数据访问层与 I/O 算法复杂度设计缺陷（性能与可扩展性）

### 3.1 分片上传 `SaveRange` 的 $O(N^2)$ 数据库读写放大

- **代码位置**：
  - `internal/store/pgstore/sources.go:240-303`
  - `internal/iso/upload.go:83, 139, 149`

- **为什么不合理（理论与后果）**：
  - `store/sources.go:53` 的契约注释写明：`SaveRange 在锁住来源的事务里合并区间`。
  - 但 `pgstore.SaveRange` 的实际实现**从未在数据库表 `iso_upload_ranges` 中合并或删除相邻/重叠的行**！每次上传一个分片，它就 `INSERT` 一行，然后在同一个事务里调用 `readRanges` 执行 `SELECT * FROM iso_upload_ranges WHERE iso_id = ? AND role = ? ORDER BY start`，把**历史上所有分片行全部拉回 Go 内存**，再用 `slices.SortFunc` 排一次序并在内存中合并累加 `total`。
  - 更糟糕的是，`iso/upload.go` 在处理单个分片时先后调用了 **3 次** `readRanges`（第 83 行 `RangesOf`、第 139 行 `SaveRange` 内部、第 149 行 `RangesOf`）。
  - **后果**：以 1MB 分片上传一个 10GB 的 Windows/RHEL ISO（共 10,000 个分片），`readRanges` 会被调用 30,000 次，累计从 PostgreSQL 读取并传输 **约 1.5 亿行记录**，越传越慢直至超时崩溃。

- **改进方法**：
  - 在 `SaveRange` 事务中，用 SQL 直接查询与 `[start, start+length]` 相交或相邻的区间（`WHERE start <= ? AND start + length >= ?`），计算合并后的新 `[minStart, maxEnd]`，删除被合并的旧行并插入合并后的单行。这样连续上传时表里永远只有 **1 行**记录，单次操作复杂度从 $O(N)$ 降为 $O(1)$，整体上传从 $O(N^2)$ 降为 $O(N)$。

---

### 3.2 远程下载 `writeFetch` 按每次 Socket `Read` 同步执行 `fsync()` + 数据库事务提交

- **代码位置**：`internal/iso/source.go:232-270`

- **为什么不合理（理论与后果）**：
  - 在 `writeFetch` 循环中，`r` 是 HTTP 响应流 `resp.Body`。网络 Socket 上的 `r.Read(buf)` 不会凑满 256KiB 才返回，而是每次返回当前 TCP 接收缓冲区/TLS 记录中的数据（通常为 4KiB ~ 32KiB）。
  - 代码在**每一次 `r.Read(buf)` 循环体内**都同步调用了 `f.Sync()`（触发操作系统磁盘 `fsync`）和 `s.store.SaveSource(...)`（开启并提交一次 PostgreSQL 写事务，锁 `isos` 行和 `iso_sources` 行并写 WAL）。
  - **后果**：下载一个 5GB 的 ISO 镜像会触发 **16 万 ~ 100 万次 `fsync` 系统调用和数据库写事务**，磁盘 IOPS 和 PostgreSQL WAL 被瞬间打满，且由于 `resumeOffset`（第 75 行）本来就在恢复时以磁盘实际文件大小为准（`st.Size()`），如此高频的数据库落盘毫无必要。

- **改进方法**：
  - 引入进度刷盘阈值（例如每累计写入 16MiB~32MiB 或每间隔 1~2 秒，以及在流结束 `io.EOF` 时），才执行一次 `f.Sync()` 和 `SaveSource` 数据库进度更新。

---

### 3.3 `COLLATE "C"` 导致 PostgreSQL 索引全面失效 + 核心过滤列缺失索引

- **代码位置**：
  - `internal/store/pgstore/hosts.go:212, 222`
  - `internal/store/pgstore/isos.go:176, 188`
  - `internal/store/pgstore/hooks.go:223, 404, 621-625`
  - `internal/store/pgstore/tasks.go:165, 251, 266`
  - `internal/store/pgstore/users.go:128, 131`
  - `internal/store/pgstore/schema.go:60-207`

- **为什么不合理（理论与后果）**：
  1. **索引排序规则（Collation）不匹配导致全表扫描**：所有分页查询都使用了 `WHERE col COLLATE "C" > ? ORDER BY col COLLATE "C"`，但 `rows_*.go` 和 `schema.go` 中的列定义与索引定义**均未声明 `COLLATE "C"`**。在绝大多数默认使用 `en_US.UTF-8` 或 `C.UTF-8` 初始化的 PostgreSQL 实例上，默认排序规则的 B-Tree 索引**无法用于 `COLLATE "C"` 的范围比较与排序**，导致所有分页列表查询被迫退化为全表扫描 + 内存排序（Seq Scan + Sort）。
  2. **多处高频查询缺失索引**：
     - `ListISOs` 支持按 `family`、`series`、`state` 过滤并按 `(name, id)` 排序，但 `isos` 表除主键和 `sha256` 外**没有任何二级索引**。
     - `ListHooks` / `ListTemplates`（`hooks.go:610-614`）使用相关子查询 `SELECT max(w.version) FROM hook_versions w WHERE w.hook_id = ... AND w.state = 'approved'`，但缺少 `(hook_id, state, version)` 复合索引。
     - `ListHosts` 的 `attrs ->> ? = ?` 与 `ListTemplates` 的 `series @> ?` 均无 GIN 索引。

- **改进方法**：
  - 在 `schema.go` 与 GORM 标签中为需要按字节序分页的键列或索引显式声明 `COLLATE "C"`（或直接依赖主键本身的排序规则，去掉查询中多余的 `COLLATE "C"`）；补齐 `isos(name, id)`、`hook_versions(hook_id, state, version DESC)`、`template_versions(template_id, state, version DESC)` 等必要索引。

---

### 3.4 任务信号 `Execution.Signals` JSONB 整列重写放大（$O(S^2)$）与控制器层 N+1 查询

- **代码位置**：
  - `internal/execution/service.go:107-151`
  - `internal/store/pgstore/execution.go:22-34, 50-56`
  - `internal/console/wizard.go:123-131, 312-322`
  - `internal/console/authoring_helpers.go:83-90`
  - `internal/iso/recover.go:50-78`

- **为什么不合理（理论与后果）**：
  1. **JSONB 写放大与重复去重**：`task_events` 表已经建有唯一索引 `idx_task_events_key (task_id, key) WHERE key <> ''` 用于信号去重。但 `model.Execution` 又在 `tasks.execution` JSONB 列里维护了一个最多 16,384 项（上限 4MiB）的 `Signals map[string]string`。每次机器上报一个信号，`Service.Signal` 都要把整行 `Task`（含大体积 `Snapshot` 和整个 `Signals` map）读出来，追加一个键后再把整个 `tasks.execution` JSONB 列完整重写回数据库，造成 $O(S^2)$ 的 PostgreSQL TOAST 写放大与死元组膨胀。
  2. **N+1 查询**：`wizardHosts`（`wizard.go:125`）对每页最多 500 台主机在 `for` 循环中逐台调用 `FindOpenTask`（产生 500 次 SQL 查询），尽管 `pgstore/tasks.go:177` 内部明明已经有批量查询函数 `openTasksOf(tx, sns)`；同理 `hookCandidates`（`authoring_helpers.go:84`）在循环中对每个 Hook 调用 `ListHookVersions`。

- **改进方法**：
  1. 在 `store.Tasks` 接口暴露批量方法 `FindOpenTasks(ctx, sns []string) (map[string]bool, error)` 供 `wizardHosts` 和 `wizardBatch` 调用。
  2. 将 `Execution` 中的信号进度简化为紧凑的阶段/槽位水位线（Watermark）或直接利用 `task_events` 的唯一键做幂等判定，避免在 `tasks` 主表热行上反复重写大体积 `Signals` map。

---

### 3.5 `schema.go` 脆弱的 PostgreSQL 反编译字符串精确比对

- **代码位置**：`internal/store/pgstore/schema.go:60-207, 233-385`

- **为什么不合理（理论与后果）**：
  - `checkSchema` 通过读取 `pg_get_expr` 和 `pg_get_constraintdef` 的反编译字符串与代码中硬编码的字符串（如 `"state <> ALL (ARRAY['done'::text, ...])"`）做精确相等比对，且在 `compareTable`（第 350、380 行）中**将数据库中多出的任何索引或约束视为致命错误并拒绝启动服务**。
  - **后果**：① 生产环境中 DBA 为排查慢查询手工建了一个辅助索引，服务重启就会直接崩溃拒绝启动；② PostgreSQL 小版本或大版本升级若微调 `pg_get_constraintdef` 的括号或类型转换格式，服务同样无法启动；③ 反过来，`checkSchema` 只查了列名列表，**完全没查列的字段类型（Data Type）和 `NOT NULL` 属性**。

- **改进方法**：
  - 采用标准化的版本迁移机制（如内置 SQL migration 版本表 `schema_migrations`）；在启动自检中改为“子集检查”（只校验必需的表、列类型、非空约束和必需索引存在，允许 DBA 创建额外索引）。

---

## 四、状态机协议、幂等性、安全边界与可移植性缺陷

### 4.1 `Service.Boot` 在并发冲突时直接返回 `found=false`，导致机器误从本地盘启动

- **代码位置**：
  - `internal/execution/service.go:46-66`
  - `internal/machine/http.go:130-138`

- **为什么不合理（理论与后果）**：
  ```go
  for range 8 {
      ...
      next, err = s.commit(ctx, task, next, ...)
      if errors.Is(err, store.ErrConflict) {
          return model.Task{}, false, nil // <--- BUG：这里没有 continue，而是直接返回 found=false, err=nil！
      }
      return next, err == nil && !next.State.Finished(), err
  }
  ```
  - 在 `Service.Boot` 的重试循环中，当 `s.commit` 遇到乐观锁冲突（`store.ErrConflict`）时，第 62 行没有像 `Answer`（第 98 行）和 `Signal`（第 146 行）那样 `continue` 重试，而是直接返回了 `model.Task{}, false, nil`（导致 `for range 8` 循环永远只跑 1 次，第 66 行成了死代码）。
  - 更严重的是，网关 `machine.boot`（`internal/machine/http.go:135-137`）看到 `found == false && err == nil`，会立即给 PXE 机器返回 `#!ipxe\nexit || sanboot --no-describe --drive 0x80\n`，**直接指示机器放弃网络装机、从本地硬盘启动**！

- **改进方法**：
  - 将 `internal/execution/service.go:62` 的 `return model.Task{}, false, nil` 改为 `continue`。

---

### 4.2 必选 Hook 失败破坏 `slot-end` 接收、破坏重试幂等性并导致目标机 Outbox 死循环

- **代码位置**：
  - `internal/render/script.go:161-168`
  - `internal/render/signal_queue.go:22-36`
  - `internal/execution/protocol.go:108-120, 147-154, 171-188`
  - `internal/execution/service.go:94-95, 120-122`

- **为什么不合理（理论与后果）**：
  1. **`hook-report` 提前置 `TaskFailed` 导致紧随其后的 `slot-end` 恒报 409**：
     - 机器端脚本（`script.go:161-168`）在必选 Hook 失败（`$RC -ne 0`）时，先发 `hook-report`，紧接着发送 `slot-end` 并退出。
     - 但服务端在收到 `hook-report`（`RC != 0 && hook.Required`）时，立即在 `protocol.go:171-174` 将 `task.State` 置为 `TaskFailed`。
     - 几毫秒后当机器发来 `slot-end` 时，`service.go:120`（`task.State.Finished()`）和 `protocol.go:118` 直接拒绝该信号并返回 HTTP 409！同时 `protocol.go:180-184` 还要求槽位内*所有*实例都有 `hook-report` 才接受 `slot-end`，一旦第 1 个必选实例失败提前终止，后面的实例根本不会运行，`slot-end` 永远无法通过校验。
     - 在 Linux `firstboot` 阶段，被 409 拒绝的 `slot-end` 会一直卡在目标机 `/var/lib/lober/outbox-finalize` 目录中，`lober-signals.timer` 每 30 秒重试一次并永远失败。
  2. **失败信号的二次重试丧失幂等性**：`service.go:120` 在调用 `advance`（内含第 108 行的重复信号幂等检查）**之前**就判断了 `if scope == Install && task.State.Finished() { return ErrCredential }`。若必选 Hook 的失败 `hook-report` 在服务端已落库（状态变为 `TaskFailed`），但响应包在网络上丢失，客户端重发同一个 `hook-report` 时会被直接拦回 409，破坏了重放幂等契约。
  3. **阶段倒退与跨槽位乱序**：`Service.Answer`（`service.go:95`）将阶段设为 `"packages"`（预算 3600s），随后 `pre` 槽位启动时 `advance`（`protocol.go:149`）又把阶段倒退回 `"pre"`（预算 600s）；且 `protocol.go:147-154` 未拒绝 `TimingRank` 已经推进到 `2`（`post`）之后迟到的 `pre` 信号，也未校验同一时机多个槽位（`Seq`）之间的先后顺序。

- **改进方法**：
  1. 在 `Service.Signal` 中，将 `advance` 的幂等重复检查（`old == value`）置于 `task.State.Finished()` 拦截之前。
  2. 当必选 Hook 失败时，允许对应的异常 `slot-end`（`sig.RC != 0`）作为该槽位的收尾信号幂等落库（或在必选 Hook 已失败时不要求后续未执行的 `hook-report`），避免目标机 Outbox 队列永久卡死。
  3. 严格校验 `sig.Timing` 的单调性（禁止高 Rank 之后接受低 Rank 信号）及前置槽位完成状态。

---

### 4.3 安全与权限边界缺陷（RBAC 脱节、改密绕过、DoS 风险、Syslog 污染）

- **代码位置**：
  - **RBAC 菜单与 API 脱节**：`internal/console/menus.go:64-142` vs. `internal/console/server.go:163-166`
  - **自改密校验绕过与会话未吊销**：`internal/console/server.go:157`、`internal/console/users.go:92-138`
  - **低权限角色泄露敏感快照**：`internal/console/perm.go:38`、`internal/console/tasks.go:56-62, 271-306`
  - **内存与算力 DoS**：`internal/console/hosts.go:183`、`errors.go:124-134`、`auth.go:72-91`、`sessions.go:14-79`、`audit.go:105-142`
  - **Syslog 租约碰撞与放大攻击**：`internal/events/syslog.go:48-96`、`cmd/lober/machine.go:88-97`

- **为什么不合理（理论与后果）**：
  1. **前端菜单权限与后端接口鉴权两张皮**：管理员通过 `PUT /console/v1/roles/:role/menus` 移除了某个角色的菜单（如移除 `tasks` 或 `hosts`），仅仅隐藏了前端导航栏，而后端 `authorize()`（`server.go:163`）依然只查硬编码的 `PermsOf(u.Role)`，该角色仍可直接调用全部 API。
  2. **绕过原密码校验与角色变更未踢下线**：`server.go:157` 允许处于 `MustChangePassword` 状态的管理员调用 `PUT /console/v1/users/:username` 修改自身，而 `updateUser`（`users.go:92`）**不校验 `CurrentPassword` 且不校验新旧密码是否相同**，也未对密码最小长度/复杂度做任何限制（`"1"` 即可通过）；此外修改用户 `Role` 时（`users.go:134`）未调用 `s.sessions.dropUser(username)` 吊销旧会话。
  3. **敏感凭据向 `viewer` 暴露**：所有角色默认拥有 `PermTaskRead`（`allPerms[:5]`），任何只读 `viewer` 都能调用 `GET /console/v1/tasks/:id` 和 `POST /console/v1/tasks/:id/preview` 查看包含明文密码/密钥参数的固化 `Snapshot` 和完整 Kickstart/Unattend 应答文件。
  4. **公网/内网 DoS 向量**：
     - `importHosts`（`io.ReadAll(c.Request.Body)`）和 `Bind`（`json.NewDecoder(c.Request.Body)`）**没有任何 `http.MaxBytesReader` 限制**，发送超大请求体即可触发 OOM。
     - `POST /console/v1/auth/login` 每次请求无条件执行 600,000 次 PBKDF2-SHA256 计算且无并发信号量或限流器，并发重放登录请求即可耗尽全部 CPU 核心，连带拖垮同进程的 `/v1/` 装机网关。
     - `exportAudit`（`audit.go:111`）将全量审计表分页读入单个内存 `bytes.Buffer` 再一次性返回，未采用流式输出（Streaming）。
  5. **UDP Syslog 源 IP 租约缺陷**：`ObserveIP`（`syslog.go:48`）仅按源 IP 绑定任务且租约长达 24 小时。在 NAT/DHCP 复用场景下会导致不同机器日志串台；且任务结束后租约不清理，24 小时内来自该 IP 的**每一个 UDP 包都会在单协程里同步查一次数据库 `GetTask`**（`syslog.go:78`）。

- **改进方法**：
  1. 从 `server.go:157` 的 `MustChangePassword` 白名单中移除 `PUT /console/v1/users/:username`，强制统一走校验原密码的 `POST /console/v1/auth/password`；在 `updateUser` 变更 `Role` 时同步清理或刷新会话；增加密码最小长度（如 $\ge 8$ 位）校验。
  2. 为 `console` 所有请求挂载 `http.MaxBytesReader` 中间件；为 PBKDF2 计算增加固定容量的并发信号量（如 `chan struct{}` 限制同时派生的协程数）与 IP/账号限流；`exportAudit` 改为直接向 `c.Writer` 流式刷新 CSV。
  3. 对非管理员或无敏感权限的角色脱敏 `Task.Snapshot` 中的敏感参数；在任务终态时主动驱逐或缓存 `syslog` IP 租约。

---

### 4.4 OpenAPI 生成器的 DRY 违背与契约漂移

- **代码位置**：
  - `internal/console/openapi.go:26-167, 234-243`
  - `internal/console/routes.go:10-25`
  - `internal/console/hosts.go:13-19`
  - `internal/console/profile.go:8-14`
  - `internal/console/users.go:10-24`

- **为什么不合理（理论与后果）**：
  1. **18 个 Query/Header 结构体仅作反射摆设（违反 DRY）**：`routes.go` 中注册的 `hostQuery`、`taskQuery`、`chunkHeaders` 等 18 个结构体在运行时从不被 Handler 绑定（Handler 全靠手写 `c.Query(...)` 解析），导致文档契约与运行时解析逻辑极易漂移。
  2. **`required` 判定逻辑错误导致生成错误的 OpenAPI 契约**：`openapi.go:241` 将所有 json 标签未加 `,omitempty` 的字段一律标记为 `required`。结果 `POST /console/v1/hosts` 在 OpenAPI 中要求必填 `rev`（新建主机根本没有 `rev`），`PUT /console/v1/user/profile` 要求必填 `displayName/email/avatar/bio` 全部四个字段，与运行时 `Bind()` 只校验 `binding:"required"` 的行为严重不符。
  3. **每次请求重复反射计算**：`GET /openapi.json`（`routes.go:25`）每次请求都重新反射遍历所有路由和结构体生成 Spec，未做单例缓存。

- **改进方法**：
  - 在请求结构体中只依据 `binding:"required"` 生成 OpenAPI 的 `required` 列表（区分 Request DTO 与 Response DTO）；让 Handler 统一使用 `c.ShouldBindQuery` / `c.ShouldBindHeader` 绑定 `routes.go` 中声明的 Query/Header 结构体；用 `sync.OnceValue` 缓存 `OpenAPI()` 的生成结果。

---

### 4.5 跨平台测试可移植性缺陷与源码树污染

- **代码位置**：
  - `internal/render/signal_recovery_test.go:46-64`
  - `internal/render/cleanup_security_test.go:36-46`
  - `internal/render/runtime_lifecycle_test.go:44, 57`
  - `internal/render/script_test.go:63-188`
  - 工作树中已产生的污染目录：`internal/render/C:/Users/mgr/AppData/Local/Temp/...`

- **为什么不合理（理论与后果）**：
  - 在 Windows 上运行测试时，`t.TempDir()` 返回 Windows 原生路径 `C:\Users\...`，而测试代码直接将其拼入脚本并调用 `exec.Command("bash", "-s")`（在 Windows 上命中了 WSL `C:\Windows\System32\bash.exe`）。WSL `bash` 将含盘符与反斜杠的 `C:\Users\...` 当作当前目录 `internal/render/` 下的相对路径，并把 `:` 和 `\` 映射为 Unicode 私有区字符 `\uf03a` (`:`) 和 `\uf05c` (`\`)，直接在源码目录下创建了 `internal/render/C:/...` 乱码目录树，且 `t.TempDir()` 结束后无法清理。
  - 此外，`cleanup_security_test.go:45` 使用 `"PATH=" + filepath.ToSlash(bin) + ":" + os.Getenv("PATH")`，在 Windows 下 `C:/Users/...` 中的冒号 `:` 会把 POSIX `PATH` 环境变量截断成 `C` 和 `/Users/...` 两段。

- **改进方法**：
  - 编写统一的跨平台测试 Shell 辅助函数：在调用外部 `bash`/`sh` 时，通过挂载工作目录（`cmd.Dir = dir`）并在脚本内一律使用相对路径 `./var/lib/lober` 和 `./bin`，彻底消除 Windows 盘符（`C:`）穿透进 POSIX Shell 的问题，并清理 `internal/render/` 下的残留目录。

---

## 五、前端（`web/apps/web-ele`）架构与工程化缺陷

### 5.1 违反单一事实来源（SSOT）与 DRY：多套重复与废弃实现并存

- **代码位置**：
  - **两套用户管理**：`src/api/console/accounts.ts` + `src/views/access/users.vue` vs. `src/api/console/user.ts` + `src/views/users/index.vue`
  - **两套改密页面**：`src/views/_core/authentication/change-password.vue`（走 `/auth/password`）vs. `src/views/account/password.vue`（走管理员接口 `updateUser`，普通用户调必报 403，且因路由未配 `meta.menuId` 被守卫过滤成死路由）
  - **两套 SSE 客户端**：`src/composables/use-task-stream.ts`（312 行未使用的 `fetch` 流客户端，且期望的 `phase/ts/message` 字段与后端实际推送的 `kind/at/fields` 完全对不上）vs. `src/views/tasks/detail.vue:97-166`（手写 `EventSource` 把 `access_token` 拼在 URL query 中泄露到日志，且在 SSE 已连接乃至任务已结束后仍每 5 秒无条件 `setInterval` 轮询后端）

- **改进方法**：
  - 删除未使用的 `src/views/users/index.vue`、`src/views/account/password.vue`、`src/components/audit-timeline.vue`；统一使用基于 OpenAPI `generated.ts` 类型的 API 模块。
  - 修正 `use-task-stream.ts` 的事件数据结构以对齐后端 `EventItem`，在 `views/tasks/detail.vue` 中改用 `useTaskStream`（通过 `Authorization: Bearer` 头认证，消除 URL Query Token 泄露），并在任务进入终态（`done/failed/cancelled`）时停止轮询。

---

### 5.2 路由守卫每次点击同步阻塞请求 `/user/info` 与全局错误重复弹窗

- **代码位置**：
  - `web/apps/web-ele/src/router/console-access.ts:121-163`
  - `web/apps/web-ele/src/api/request.ts:62-71`

- **为什么不合理（理论与后果）**：
  1. `console-access.ts:132` 在 `router.beforeEach` 中对**每一次前端路由跳转**都同步执行 `await synchronize()` -> `GET /console/v1/user/info`，导致每次点击菜单都有一次完整的网络 RTT 延迟，且一旦网络抖动即中断前端跳转（实际上 `useConsoleSessionRefresh` 已经做了 30s 轮询和窗口聚焦刷新）。
  2. `request.ts:62` 注册了全局 `errorMessageResponseInterceptor` 自动弹出 `ElMessage.error`，而各个业务页面在 `catch` 块中又手动调用了一次 `ElMessage.error`，导致每次接口报错都在界面上弹出两个重复的错误提示框。

- **改进方法**：
  - 在 `router.beforeEach` 中仅当 `authStore.sessionUser` 为空时才阻塞拉取 `/user/info`，其余导航直接基于内存态放行，依靠后台定时/聚焦轮询和 401/403 响应拦截器做权限同步。
  - 移除业务视图中冗余的 `ElMessage.error` 调用（或在 `request.ts` 支持静默配置项），统一由结构化 `<ProblemList>` 或拦截器单点提示。

---

## 六、建议优先整改路线图（Priority Roadmap）

| 优先级 | 类别 | 关键整改项 |
| :--- | :--- | :--- |
| **P0（功能阻断与数据安全）** | 状态机 / OS 导入 / 并发安全 | 1. 修复 `execution/service.go:62` `Boot` 冲突误判为无任务（防止机器误从本地盘启动）<br>2. 修复必选 Hook 失败后 `slot-end` 被拒 409 与重放非幂等问题（`execution/protocol.go` & `service.go`）<br>3. 修复 ESXi 版本识别与提取缺失、Agama `products` Offer 缺失、Windows `edition` Image Index 错位<br>4. 修复 `deletionJournal` 与在途 `Upload`/`FetchOne`/`Import` 的并发竞态 |
| **P1（严重性能瓶颈与 DoS）** | 存储层 / 网关 / 控制台 | 1. 重写 `pgstore.SaveRange` 在 SQL 内合并区间，消除分片上传 $O(N^2)$ 读放大<br>2. 为 `iso.writeFetch` 增加 16MB/1s 刷盘阈值，消除每 TCP 包一次 `fsync` + DB 事务<br>3. 移除 `pgstore.UpdateUser` 无条件的 `LOCK TABLE users`<br>4. 修复 `COLLATE "C"` 索引失效问题并补齐 `isos`/`hook_versions` 索引<br>5. 为 `console` 增加 `MaxBytesReader`、PBKDF2 并发限制及密码复杂度与自改密校验 |
| **P2（架构演进与工程整洁）** | 架构解耦 / 前后端契约 | 1. 将 C23 `CurrentApproved` 校验移入 `pgstore.CreateTasks` 事务，移除 `console` 进程内 `templateSelection` 锁<br>2. 抽取 `FormatStrategy` 多态接口替代散落在 4 层的 `switch format`<br>3. 将 `cmd/lober` 中的 `deletionJournal` 和 `sourceSupervisor` 下沉至内部业务包<br>4. 清理前端重复的改密/用户/SSE 组件，修复 `openapi.go` 的 `required` 推导规则 |
