# C ISO 来源 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
> 本计划提供接口契约与验收标准，不提供源码。测试与实现体由你在 TDD 循环中写出，以真实测试输出为准。

**目标：** 让 ISO 来源从「只能填登记服务器本地路径」扩展到三种（本地路径 / 远程下载 / 浏览器上传），两条传输通道都支持跨进程重启的断点续传，并顺带修掉「来源不落库、导入不可重试」这个现存缺口。

**架构：** 新增 `artifact_sources` 表承载来源，一个制品集多条来源记录（按 `Role` 区分 iso / netboot / winpe-*），全部 `complete` 才触发导入。远程下载用 HTTP `Range` 续传，上传用分片 PUT 后拼接。字节落在 `/srv/lober/incoming/<sourceID>/`（**不在 nginx 直发路径下**）。导入触发从「登记请求的生命周期内」改为「装配点从 Store 读来源后触发」，于是重试与重启恢复都成为可能。

**技术栈：** Go 1.27、GORM/PostgreSQL、`net/http`（Range 与自定义 `DialContext` 做 SSRF 防护）、gin（分片 PUT 流式落盘）、Element Plus + `fetch`（前端分片上传）

**设计依据：** `docs/superpowers/specs/2026-09-29-c-iso-source-design.md`（commit `051179c`）

---

## 架构约束（每个任务都必须遵守）

1. **`internal/core/model` 不得 import 任何项目内包**（`internal/archtest/deps_test.go:145` 守卫）。
2. **七个模块（store / statemachine / render / gateway / console / provider / imports）两两之间不得互相 import**（`deps_test.go:273` 守卫，**含测试 import**）。跨模块只在 `cmd/lober` 装配。
3. **实现代码里不得出现 `time.Now()`**，时间全部来自注入的 `contract.Clock`（`console.Deps.Clock` 已有此字段，`internal/console/server.go:69`）。
4. **错误信息用中文且给可行下一步**，沿用 `internal/console/task.go:63-76` 的风格。水位不足要说清还缺多少字节，不是只说「空间不足」。
5. **fail loudly，不静默降级。**
6. **注释解释「为什么」并引用 SPEC 小节**，包括为什么没选另一条路。
7. **提交信息用中文**，格式 `类型(范围): 简述`。
8. **控制台响应统一 `{"code":0,"data":...,"message":""}` 且同时给真实 HTTP 状态码**，码表在 `internal/apierr/apierr.go:38-48`。本计划**不新增业务码**——复用 `CodeBadRequest`(1001)、`CodeNotFound`(1004)、`CodeConflict`(1005)、`CodeValidationFailed`(1009)。

## 契约变更流程

`internal/core/contract` 里已发布的签名改动是独立的「契约变更」任务：改接口 → 改 conformance 套件 → 改内存实现 → 改 postgres 实现。**顺序不可颠倒**——`internal/core/contract/conformance` 是库代码（不是 `_test.go`），两个实现包都 import 它，先加断言才能让「套件变绿即集成完成」这条机械规则成立。

## 测试命令约定

```bash
export LOBER_TEST_PG_DSN='postgres://lober_test:lober_test_pw@localhost:5432/lober_test?sslmode=disable'
make check          # 收口：fmt → vet → test → race，四步全绿才算做完
go build ./...      # 每个任务末尾必须绿
```

**本机已知的 4 个既存失败用例**（`internal/render`，Windows/msys 环境问题，与本计划无关，不要试图修）：

```
TestInspect_DiskCollectionFailureStillProducesNICOutput
TestInspect_DiskFunctionOutputExactKeyFormat
TestInspect_NICFunctionOutputExactKeyFormat
TestExec_RequiredHookFailureAbortsSectionEvenWithLaterHooks
```

判断是否引入回归的标准：`go test ./internal/...` 的失败集合**恰好等于**上面这 4 个。本计划不碰 `internal/render`，这 4 个应当始终保持原样。

前端检查命令（本机 node 不在 PATH、无 pnpm、`node_modules/.bin/` 的 shim 用不了）：

```bash
export PATH="/c/Program Files/nodejs:$PATH"
cd web/apps/web-ele && node ../../node_modules/vue-tsc/bin/vue-tsc.js --noEmit --skipLibCheck
cd web && node node_modules/eslint/bin/eslint.js --fix <文件>
```

**不要跑 `vite build`** 除非确实要验证打包：它的 outDir 指向 `internal/console/ui/dist/` 且构建会**清空**那里，而那里有两个被 git 跟踪的占位文件（`index.html`、`_placeholder-keeps-embed-underscore-mode.js`），少了它们 `go:embed` 编译不过。真要 build，跑完立刻 `git restore internal/console/ui/dist/`。

---

## 文件结构

### 为什么这样切

**来源是独立聚合，不是制品集的附属字段。** 一个制品集有多条来源（Debian 要 ISO 加
netboot，Windows 还要 WinPE 四件套），且来源有自己的生命周期（`pending → fetching →
complete`）与自己的写入节奏（每 8 MB 更新一次 `ReceivedBytes`）。塞进 `ArtifactSet`
会让每次刷新进度都重写整行制品集，也让「制品集 ready 但某条来源还在传」这种中间态无法
表达。因此 postgres 侧新建 `internal/store/postgres/source/` 子包，与现有八个聚合包
（`artifact`/`template`/`hook`/`task`/`host`/`event`/`fact`/`audit`）平级。

**传输实现放 `cmd/lober`，不放 `internal/console`。** 下载与拼接都要读写文件系统、发
外部 HTTP 请求，而 console 是 HTTP handler 层。更硬的理由：触发导入的 `ArtifactImporter`
实现已经在装配点（`cmd/lober/importrunner.go:69`），传输完成后要直接接上它，放在别处
就得再穿一层接口。console 侧只定义窄接口（与 `console.ArtifactImporter`
`internal/console/artifact.go:48` 同一种接缝）。

**SSRF 校验单独一个文件且不依赖 http.Client。** 它是纯函数（URL 字符串 + 解析出的 IP →
合法与否），要能脱离网络单测。把它和下载循环混在一个文件里会让「重定向每跳重校」这条
规则只能靠起真实 HTTP 服务器来验证。

**上传的三个端点与制品集四个端点分文件。** `internal/console/artifact.go` 已经 363 行且
承载了制品集的全部四个端点加两个窄接口定义；来源相关的 5 个端点（登记 / 列表 / 分片 /
完成 / reimport）另起 `source.go`，否则那个文件会超过 700 行。

| 文件 | 职责 |
|---|---|
| **model 层** | |
| 创建 `internal/core/model/source.go` | `ArtifactSource`、`SourceKind`、`SourceRole`、`SourceState` 与校验 |
| **契约与存储层** | |
| 修改 `internal/core/contract/store.go` | 新增 `ArtifactSourceStore` 窄接口 |
| 修改 `internal/core/contract/conformance/store.go` | 来源读写的可执行断言（**先于两个实现**） |
| 修改 `internal/core/fake/store.go` | 内存实现 |
| 创建 `internal/store/postgres/source/{row.go,store.go}` | GORM 行类型与实现 |
| 修改 `internal/store/postgres/open.go:86-97` | `AllModels()` 加 `source.Models()` |
| 修改 `internal/store/postgres/schemacheck.go` | 若来源表有索引，加结构断言 |
| **控制台层** | |
| 创建 `internal/console/source.go` | 5 个端点 + `SourceFetcher` 窄接口定义 |
| 修改 `internal/console/artifact.go:176-245` | 创建端点接受三种来源，落来源记录而非直接转发路径 |
| **装配点** | |
| 创建 `cmd/lober/ssrf.go` | URL 与 IP 校验（纯函数，不碰网络） |
| 创建 `cmd/lober/downloader.go` | Range 续传下载循环 + 进度刷新 |
| 创建 `cmd/lober/uploadstore.go` | 分片落盘、拼接、sha256 校验 |
| 创建 `cmd/lober/sourcerecover.go` | 启动时扫 `fetching` 来源恢复 + 过期清理 |
| 修改 `cmd/lober/importrunner.go:69-91` | `TriggerImport` 改为从 Store 读来源组装 `ImportSource` |
| 修改 `cmd/lober/preflight.go` | 加「`incoming/` 不在 `artifacts/` 之下」断言 |
| 修改 `cmd/lober/wire.go` | 装配上面四个新类型 |
| **配置与前端** | |
| 修改 `internal/config/config.go:227-230` | `Store` 加 `IncomingPath`；新增 `Source` 配置节 |
| 修改 `deploy/lober.yaml.example` | 新配置项样例 |
| 修改 `web/apps/web-ele/src/api/console/artifact.ts` | 来源相关类型与 5 个端点 |
| 修改 `web/apps/web-ele/src/views/artifact-sets/index.vue` | 来源三选一 + 进度条 |
| 创建 `web/apps/web-ele/src/composables/useChunkedUpload.ts` | 分片上传与重试，独立于视图 |
| **文档** | |
| 修改 `docs/contracts/{http-console,filesystem}.md`、`SPEC.md` | 契约与范围决定同步 |

## 任务依赖顺序

```
1        model 层（纯类型，无依赖）
2-4      契约 → conformance → fake → postgres
5-6      创建端点改造 + 导入解耦 + reimport（出口：登记后重启仍能触发导入）
7        SSRF 校验（纯函数，可与 5-6 并行）
8-9      下载循环 + 重启恢复
10-12    上传三端点 + 拼接校验
13       容量水位与过期清理
14-15    前端
16       文档
```

---

# 阶段一：model 层

### 任务 1：`model.ArtifactSource` 与三个枚举

**目标：** 把来源做成独立领域类型，并把「哪些状态转换合法」「续传起点怎么算」这两条规则
收敛在 model 层。

**文件：**
- 创建：`internal/core/model/source.go`
- 测试：`internal/core/model/source_test.go`

**接口契约：**

`ArtifactSource` 结构体与三个枚举的取值域**照抄设计 §2 那段**（13 个字段、3 + 6 + 4 个
常量），不在此重复。本任务要定的是取值域之外的四个判定：

```go
func (k SourceKind) Valid() bool
func (r SourceRole) Valid() bool
func (s SourceState) Valid() bool

// ResumeOffset 返回续传起点：库记录的 ReceivedBytes 与盘上实际大小的**较小值**。
func ResumeOffset(recordedBytes, onDiskBytes int64) int64

// ChunkCount 按 totalBytes 与 chunkSize 算分片总数；两者任一非正数时返回 0。
func ChunkCount(totalBytes, chunkSize int64) int

// ValidChunkIndex 判定分片序号是否在 [0, ChunkCount) 内。
func ValidChunkIndex(n int, totalBytes, chunkSize int64) bool
```

`SourceRole` 的六个常量名要与设计 §2 的字符串值对得上（`winpe-boot-sdi` 这类带连字符的
值，Go 常量名用 `SourceRoleWinPEBootSDI`）。

- 三个 `Valid` 的空串**都不合法**。与 `Family.Valid`（spec A 任务 3）同一方向：来源没有
  合理默认值，猜一个会让字节落到错的地方或跳过校验。
- `ResumeOffset` 取较小值是安全关键，**方向不可反**（设计 §4）：库领先于盘时若按库的值
  续传，中间那段字节永远不会被写入，产出一个长度正确、内容错位的文件——sha256 校验能
  发现它，但要等整个 5 GB 传完才发现。两个入参任一为负时返回 0。
- `ValidChunkIndex` 是路径穿越的第一道闸门（设计 §6.1）：分片序号进落盘路径，负数或
  超界的序号必须在进文件系统之前拒绝。

- [ ] **红：** 覆盖：三个 `Valid` 各自的合法值全集 + 空串 + 一个拼错值；`ResumeOffset`
      四类（库=盘、库>盘、库<盘、含负数）；`ChunkCount` 的整除与不整除两种（5 GB /
      8 MB 不整除，最后一片是残片，算错会让 `ValidChunkIndex` 拒绝最后一片）；
      `ValidChunkIndex` 的 -1 / 0 / 末片 / 末片+1。
      运行 `go test ./internal/core/model/ -run "TestSource|TestResumeOffset|TestChunk" -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。

- [ ] **重构 + commit：** `ResumeOffset` 的注释必须写明「取较小值」的方向与写反的后果
      （引用设计 §4 与 §9 风险 1）——这是本计划最容易被后来者「优化」掉的一行。commit
      （`feat(model): 新增制品来源类型与续传起点计算`）。

**风险：** `SourceRole` 的六个取值与 `console.ImportSource` 的字段（`ISOPath`、
`NetbootPath`，`internal/console/artifact.go:63-80`）以及 spec B §4 的 `WinPESources`
四个字段是一一映射关系。**命名要对得上**——`winpe-boot-sdi` 对应 `BootSDIPath`。映射在
任务 6 落地，此处先把名字定死，免得两处各起一套名字后再对齐。

---

# 阶段二：契约与存储层

### 任务 2：`ArtifactSourceStore` 契约与 conformance 断言

**目标：** 把来源的存储行为写成可执行契约，让「套件变绿即集成完成」这条规则成立。

**文件：**
- 修改：`internal/core/contract/store.go` —— 新增 `ArtifactSourceStore`，加入
  `Store` 组合接口（`store.go:397`）
- 修改：`internal/core/contract/conformance/store.go`

**接口契约：**

```go
// ArtifactSourceStore 承载制品来源（一个制品集多条，按 Role 区分）。
type ArtifactSourceStore interface {
    CreateArtifactSource(ctx context.Context, src model.ArtifactSource) (model.ArtifactSource, error)
    GetArtifactSource(ctx context.Context, id string) (model.ArtifactSource, error)

    // ListArtifactSourcesBySet 返回某制品集的全部来源，按 Role 升序（稳定顺序供
    // UI 与错误文案用）。制品集不存在时返回空切片而非 ErrNotFound——「没有来源」
    // 与「制品集不存在」的区分归调用方，本方法不替它判断。
    ListArtifactSourcesBySet(ctx context.Context, artifactSetID string) ([]model.ArtifactSource, error)

    // ListArtifactSourcesByState 按状态列出来源，供启动恢复与过期清理扫描。
    ListArtifactSourcesByState(ctx context.Context, states ...model.SourceState) ([]model.ArtifactSource, error)

    // UpdateArtifactSourceProgress 只改 ReceivedBytes / TotalBytes / UpdatedAt。
    // 下载与上传的高频刷新走这条，不触碰 State 与校验值——避免一次进度刷新把
    // 并发写入的 State 覆盖回旧值。
    UpdateArtifactSourceProgress(ctx context.Context, id string, receivedBytes, totalBytes, now int64) error

    // UpdateArtifactSourceState 只改 State / ActualSHA256 / FailReason / UpdatedAt。
    UpdateArtifactSourceState(ctx context.Context, id string, state model.SourceState, actualSHA256, failReason string, now int64) (model.ArtifactSource, error)

    DeleteArtifactSource(ctx context.Context, id string) error
}
```

- **进度与状态分成两个方法**，与既有 `ArtifactSetStore.UpdateArtifactSetState` /
  `SetArtifactSetContent` 的拆分（`store.go:234-249`）同一理由：一个高频、一个低频，合成
  一个方法会让每次进度刷新都得先读一遍当前 State 再写回，中间那个窗口足够让另一个
  goroutine 的状态变更被覆盖。
- `now` 作为显式参数传入，**不在 Store 实现里取时钟**——架构约束 3。
- 制品集删除时级联删来源：由 postgres 侧的外键 `ON DELETE CASCADE` 保证，fake 侧手工
  实现同等行为。conformance 要有断言覆盖这一点。

**要加的断言类别**（每类一个 `t.Run` 子测试）：

| 断言 | 要验证什么 |
|---|---|
| 来源往返 | 写入含全部字段的来源，读回逐字段相符 |
| 按制品集列出 | 同一制品集写 3 条不同 Role，读回 3 条且按 Role 升序；另一个制品集的来源不混入 |
| 按状态列出 | 写入 4 种状态各一条，按单状态与多状态查各自返回正确子集 |
| 进度刷新不动状态 | 写入 `fetching` 来源 → 调进度更新 → 读回 State 仍是 `fetching`、`ReceivedBytes` 已变 |
| 状态更新不动进度 | 反向：调状态更新 → `ReceivedBytes` 不变 |
| 级联删除 | 删制品集后该制品集的来源全部消失 |
| 大字节数 | `TotalBytes` 写 5 GB（超 `int32`），读回相符——列类型选成 `integer` 只有在超 21 亿时才暴露 |

- [ ] **红：** 按上表加断言。运行 `go test ./internal/core/fake/ -run TestConformance -count=1`，
      预期 FAIL（fake 尚未实现 `ArtifactSourceStore`）。这个 FAIL 就是契约已就位的证据。

- [ ] **绿：** 本任务**不实现**，绿灯在任务 3、4。此处的「绿」是确认套件本身能编译且失败
      原因是「实现缺方法」而非「套件写错」——`go vet ./internal/core/contract/...` 绿。

- [ ] **重构 + commit：** 套件里的时刻值必须从 `fixedNow` 加减偏移量取（套件头部
      `conformance/store.go:30-44` 详述了时间基准漂移问题），不要新引入 `time.Now()`。
      commit（`test(contract): 新增制品来源 Store 契约与断言`）。

**风险：** 把 `ArtifactSourceStore` 加进 `Store` 组合接口（`store.go:397`）会让所有实现
`contract.Store` 的类型立刻编译失败，包括 fake 与 postgres。**这是刻意的中间态**——不要
为了让 `go build ./...` 绿而在本任务里顺手实现那两个包。只保证 `go vet
./internal/core/contract/...` 绿；全仓绿在任务 4 末尾。

---

### 任务 3：fake 内存实现

**目标：** 让 conformance 套件在内存实现上全绿，为并行模块与控制台测试提供替身。

**文件：**
- 修改：`internal/core/fake/store.go`

- [ ] **红：** `go test ./internal/core/fake/ -run TestConformance -count=1`，预期 FAIL
      （承接任务 2）。

- [ ] **绿：** 实现六个方法至套件全绿。`ListArtifactSourcesBySet` 返回**新分配的切片**，
      不要把内部 map 里的值直接交出去——调用方改一条来源的字段就改到了「库里」的数据，
      而 postgres 实现不会有这个行为，两者分叉就让 conformance 套件失去意义。
      级联删除要在 `DeleteArtifactSet` 里手工实现（fake 没有外键）。

- [ ] **重构 + commit：** `go test ./internal/core/fake/ -count=1` 全绿。commit
      （`feat(fake): 内存 Store 支持制品来源`）。

**风险：** fake 的 `DeleteArtifactSet` 现在只删制品集本身。加级联时要注意**顺序**：先删
来源再删制品集，反过来会在删除中途失败时留下一批孤儿来源记录（它们的 `ArtifactSetID`
指向一个已不存在的制品集，而 `ListArtifactSourcesByState` 的扫描会把它们当成待恢复的
下载任务，启动时反复尝试下载一个没人要的文件）。

---

### 任务 4：postgres 实现与 schemacheck

**目标：** 让 conformance 套件在 PostgreSQL 实现上全绿，索引结构断言随新表落地。

**文件：**
- 创建：`internal/store/postgres/source/row.go` —— GORM 行类型与 `Models()`
- 创建：`internal/store/postgres/source/store.go` —— 六个方法
- 修改：`internal/store/postgres/open.go:86-97` —— `AllModels()` 加 `source.Models()`

**行类型的列**（跟着 `internal/store/postgres/artifact/row.go:27-69` 的 tag 风格写）：

```go
type Row struct {
    ID             string `gorm:"column:id;primaryKey"`
    ArtifactSetID  string `gorm:"column:artifact_set_id;index"`
    Kind           string `gorm:"column:kind"`
    Role           string `gorm:"column:role"`
    URI            string `gorm:"column:uri"`
    ReceivedBytes  int64  `gorm:"column:received_bytes;not null;default:0"`
    TotalBytes     int64  `gorm:"column:total_bytes;not null;default:0"`
    ExpectedSHA256 string `gorm:"column:expected_sha256;not null;default:''"`
    ActualSHA256   string `gorm:"column:actual_sha256;not null;default:''"`
    State          string `gorm:"column:state;index"`
    FailReason     string `gorm:"column:fail_reason;not null;default:''"`
    CreatedAt      int64  `gorm:"column:created_at"`
    UpdatedAt      int64  `gorm:"column:updated_at"`
}
```

**唯一约束决策：** `(artifact_set_id, role)` 建**唯一**索引——一个制品集的同一个角色只能
有一条来源。没有它，重复调用登记端点会产生两条 `role=iso` 的来源，而任务 6 的「全部
complete 才触发导入」会看到两条记录中的一条永远停在 `pending`，导入永不触发且没有任何
错误信息。

- [ ] **红：** `go test ./internal/store/postgres/ -run TestConformance -count=1`
      （需 `LOBER_TEST_PG_DSN`），预期 FAIL。

- [ ] **绿：** 实现至全绿。`ReceivedBytes` / `TotalBytes` 必须是 `int64` 映射到 PG 的
      `bigint`——GORM 对 Go 的 `int` 在某些平台会映射成 `integer`，5 GB 超出 `int32` 范围
      写入时报错，而那个错误只在传超过 2 GB 的 ISO 时出现（任务 2 的「大字节数」断言
      兜住这一点）。

- [ ] **重构 + commit：** **索引断言必须同步加**——`AutoMigrate` 有两个不报错的失效点
      （复合索引漏标某个参与列会**静默降级成单列索引**；已存在的索引**永不更新**）。
      `(artifact_set_id, role)` 是复合唯一索引，两个列都要标对。往
      `internal/store/postgres/schemacheck.go` 的 `wantIndexes` 加一条，WHERE 与列清单
      **照 PostgreSQL 规范化后的实际输出写**（`!=` 变 `<>`、`''` 变 `''::text`、每层
      比较外加括号）——先在 psql 里对新表建一次索引，查 `pg_indexes.indexdef` 抄真实
      输出，**不要凭猜**（该文件头 `schemacheck.go:13-22` 写明了这个探针跑法）。
      `make check` 全绿后 commit（`feat(store): postgres 支持制品来源与索引断言`）。

**风险：** 外键 `ON DELETE CASCADE` 靠 GORM tag 表达在 `AutoMigrate` 下**不可靠**——它对
已存在的表不会补加约束（与「已存在的索引永不更新」同源）。两个选择：在
`artifact.Store.DeleteArtifactSet` 里显式删来源（与 fake 行为一致，跨包调用要经
`TxRunner` 保证原子性），或在 `source` 包的 `Models()` 里用 GORM 的 `constraint` tag 并
**在 schemacheck 里加一条外键存在性断言**。选前者：它不依赖 `AutoMigrate` 的行为，且
`internal/store/postgres/tx.go` 已有 `TxRunner`。

---

# 阶段三：来源落库与导入解耦

### 任务 5：创建端点接受三种来源

**目标：** `POST /artifact-sets` 从「直接把路径转发给 importer」改成「落来源记录」，这是
修掉 §1.1 那个缺口的第一半。

**文件：**
- 修改：`internal/console/artifact.go:176-245` —— 请求体与 handler
- 修改：`internal/console/artifact_test.go`

**请求体契约：**

```go
type createArtifactSetRequest struct {
    Name    string                  `json:"name"`
    Family  string                  `json:"family"`   // spec A/B 引入，此处一并接受
    Sources []createSourceRequest   `json:"sources"`
}

type createSourceRequest struct {
    Kind           string `json:"kind"`             // local | url | upload
    Role           string `json:"role"`             // 缺省视为 iso
    URI            string `json:"uri"`              // local 是路径，url 是链接，upload 留空
    TotalBytes     int64  `json:"totalBytes"`       // upload 必填（前端知道文件大小）
    ExpectedSHA256 string `json:"expectedSha256"`   // 可选
}
```

**向后兼容：** 旧入参 `sourcePath` 与 `netbootPath`（`artifact.go:179-184`）**保留为兼容
字段**，`Sources` 为空且 `sourcePath` 非空时，等价于一条 `{kind:local, role:iso}` 加一条
可选的 `{kind:local, role:netboot}`。理由：七个 e2e 脚本（`scripts/e2e*.sh`）都用旧形态
调这个端点，一刀切会让它们全部失败，而那些脚本是 RHEL/Debian 两条链路当前唯一的回归
护栏。兼容路径在 handler 里转换成 `Sources` 后走同一条路，**不要写两套落库逻辑**。

**校验**（每条都要有对应测试）：`name` 非空（沿用 `strings.TrimSpace` 而非 binding
`required`，理由见 `artifact.go:194-198`：后者不拒纯空格）；`Sources` 与兼容字段不能同时
为空；每条来源的 `Kind`/`Role` 经 `Valid()`；`role` 在同一请求内不重复；`kind=local` 或
`url` 时 `URI` 非空；`kind=upload` 时 `TotalBytes` 为正。

**行为：** 落制品集（`state=importing`）与 N 条来源记录（`kind=local` 直接置
`complete`，`URI` 就是最终路径；`url`/`upload` 置 `pending`）。**不再在 handler 里调
`TriggerImport`**——触发改由任务 6 的 `SourceFetcher` 在来源齐备后完成。

- [ ] **红：** 覆盖：新形态单条 iso 来源；新形态 iso + netboot 两条；旧形态 `sourcePath`
      兼容；旧形态带 `netbootPath`；`Sources` 与兼容字段都空（400）；`role` 重复（400）；
      `kind` 拼错（400）；`kind=upload` 但 `totalBytes=0`（400）；`kind=local` 落库后
      state 直接是 `complete`；`kind=url` 落库后是 `pending`。
      运行 `go test ./internal/console/ -run TestCreateArtifactSet -count=1 -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。落制品集与落来源必须在**同一个事务**里（`contract.TxRunner`）：
      制品集落成功而来源落失败会留下一个永远停在 `importing`、没有任何来源、也没有任何
      错误信息的制品集——运维在列表里只能看到它转圈。

- [ ] **重构 + commit：** commit（`feat(console): 制品集登记接受三种来源`）。

**风险：** 现在 handler 里对 `s.deps.ArtifactImporter` 判空后才调用（`artifact.go:237`），
判空的理由写在 `artifact.go:231-236`（可为 nil，为 nil 时制品集停在 `importing`）。改造
后这个判空点消失，但**同样的降级语义要转移到任务 6 的 `SourceFetcher` 判空上**，并把那段
注释的理由一并搬过去——否则下一个人会以为这是漏了判空。

---

### 任务 6：导入触发解耦与 `reimport` 端点

**目标：** 让导入从「登记请求的生命周期内触发」改成「装配点从 Store 读来源后触发」，于是
重试与重启恢复都成为可能。这是修掉 §1.1 缺口的第二半。

**文件：**
- 创建：`internal/console/source.go` —— `SourceFetcher` 窄接口定义 + `reimport` 端点
- 修改：`cmd/lober/importrunner.go:69-91` —— `TriggerImport` 改为读 Store 组装
- 测试：`internal/console/source_test.go`、`cmd/lober/importrunner_test.go`

**接口契约：**

```go
// SourceFetcher 是控制台触发「开始取某条来源的字节」需要的最窄接口。
// 接口定义在 console 包内、实现放 cmd/lober 装配点——与 ArtifactImporter
// （internal/console/artifact.go:48）完全同一种接缝：console 不能 import
// internal/imports，也不该知道 HTTP 下载与文件拼接。
type SourceFetcher interface {
    // TriggerFetch 异步开始取该来源。语义是「立即返回」，实现必须自己起 goroutine。
    TriggerFetch(ctx context.Context, sourceID string)

    // TriggerImportIfReady 检查某制品集的全部来源是否都 complete，是则触发导入。
    // 幂等：重复调用不会重复触发已在跑的导入。
    TriggerImportIfReady(ctx context.Context, artifactSetID string)
}
```

`cmd/lober` 侧 `importRunner.TriggerImport` 的签名改为不再接收 `console.ImportSource`，
改为自己按 `Role` 从 `ArtifactSourceStore` 读出来组装：`iso` → `ISOPath`，`netboot` →
`NetbootPath`，四个 `winpe-*` → `WinPE`（spec B §4 的 `WinPESources`）。

**新增端点：** `POST /artifact-sets/:id/reimport`，注册进 `routesArtifactSet`
（`internal/console/artifact.go:112-117`），权限码沿用 `codeArtifactImport`，审计动作名
`artifact_set.reimport`。前置条件：该制品集全部来源 `complete` 且制品集
`state in (failed, importing)`。不满足时 409 + `CodeConflict`，文案要说清是哪条来源还没
齐备（含 `Role` 与 `receivedBytes/totalBytes`）。

- [ ] **红：** 覆盖：`reimport` 在全部来源 complete + 制品集 failed 时返回成功并触发一次
      导入；某条来源仍 `pending` 时 409 且文案含该来源的 Role 与进度；制品集不存在时 404；
      制品集已是 `ready` 时 409。`importRunner` 侧：三条来源（iso + netboot + 一个
      winpe-*）能正确组装成 `ImportSource` 的三个字段；缺 `role=iso` 的来源时报错而非用
      空路径去 Run（空 `isoPath` 会让 `imports.Job.Run` 报一个指向空字符串的打开失败，
      运维看不出是来源没配还是文件真的不存在）。
      运行 `go test ./internal/console/ ./cmd/lober/ -run "TestReimport|TestImportRunner" -count=1 -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。`SourceFetcher` 判空沿用 `ArtifactImporter` 的既有接缝
      （`artifact.go:231-236`）：为 nil 时来源停在 `pending`，不 panic。

- [ ] **重构 + commit：** `go test ./internal/archtest/ -count=1` 绿——`SourceFetcher` 的
      接口定义必须在 console 包内，实现在 `cmd/lober`。commit（`feat: 导入触发与登记请求
      解耦，新增 reimport 端点`）。

**风险：** `importrunner.go:62-68` 的注释写明 ctx 由调用方用 `context.WithoutCancel` 剥离
了 HTTP 请求的取消信号，并明确「本方法自己不重复剥离」。改造后 `TriggerImport` 的调用方
从 handler 变成 `SourceFetcher`（也在装配点），那里**没有 HTTP 请求上下文可剥离**——要用
一个进程级的 ctx（`cmd/lober` 的主 ctx），不要传 `context.Background()` 然后指望进程退出
时导入能干净停下。同时把那段注释更新掉，否则它会指向一个已经不存在的调用点。

---

# 阶段四：远程下载

### 任务 7：SSRF 校验（纯函数）

**目标：** 把「这个 URL 能不能去连」做成不碰网络的纯判定，这样「重定向每跳重校」才能脱离
真实 HTTP 服务器来测。

**文件：**
- 创建：`cmd/lober/ssrf.go`
- 测试：`cmd/lober/ssrf_test.go`
- 修改：`internal/config/config.go` —— 新增 `Source` 配置节的 `URLAllowlist []string`

**接口契约：**

```go
// urlGuard 按配置校验下载目标。allowCIDRs 为空表示不启用白名单。
type urlGuard struct {
    allowCIDRs []netip.Prefix
}

// CheckURL 校验 URL 本身（scheme 与主机形态），不做 DNS 解析。
func (g urlGuard) CheckURL(raw string) error

// CheckIP 校验一个已解析出的 IP 是否允许连接。
func (g urlGuard) CheckIP(ip netip.Addr) error
```

**五条规则**（设计 §6.2，每条一个测试）：

| 规则 | 判据 |
|---|---|
| scheme 白名单 | 只允许 `http` / `https`。`file://`、`gopher://`、`ftp://`、空 scheme 全拒 |
| loopback | 拒 `127.0.0.0/8`、`::1`、`0.0.0.0`、`[::]` |
| 链路本地 | 拒 `169.254.0.0/16`、`fe80::/10` |
| 解析后再校验 | `CheckIP` 在**每次建连前**被调用，靠自定义 `DialContext` 挂进 `http.Transport`——这是唯一能防 DNS rebinding 的位置（任务 8 接线） |
| 白名单 | `allowCIDRs` 非空时，IP 必须落在其中某个 CIDR 内 |

- `CheckIP` 还要拒**未指定地址**与**多播**（`ff00::/8`、`224.0.0.0/4`）。理由与 loopback
  同源：那些地址不是合法的镜像站地址，放过它们只是给绕过留一条没必要的路。
- **私网地址（`10/8`、`172.16/12`、`192.168/16`）不拒**——气隙内网的镜像站正是私网地址，
  拒了这条通道就完全不可用。收紧靠 `allowCIDRs` 配置项，不靠硬编码。这一条要写进注释，
  否则后来者会以为是漏了。

- [ ] **红：** 覆盖：四种非法 scheme + 两种合法；loopback 的 IPv4 与 IPv6 各一；
      `0.0.0.0` 与 `[::]`；链路本地 IPv4 与 IPv6 各一；多播；私网地址在**无白名单**时
      放行、在**有白名单且不含它**时拒绝；白名单含它时放行；`CheckURL` 对畸形 URL
      （无 host、端口非数字）报错。
      运行 `go test ./cmd/lober/ -run TestURLGuard -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。用 `net/netip` 而非 `net.IP`——后者的 `IsLoopback()` 对
      IPv4-mapped IPv6 地址（`::ffff:127.0.0.1`）的判定容易被绕过，`netip.Addr.Unmap()`
      能显式处理这一层。测试里必须有一个 `::ffff:127.0.0.1` 的用例。

- [ ] **重构 + commit：** commit（`feat(cmd): 下载目标的 SSRF 校验`）。

**风险：** `CheckURL` 只校验 URL 字符串，**它单独用是不安全的**——真正的防护在
`CheckIP` + 自定义 `DialContext`。函数注释必须写明这一点并指向任务 8 的接线位置，否则
后来者会以为调了 `CheckURL` 就安全了，而那正是 DNS rebinding 能绕过的形态。

---

### 任务 8：Range 续传下载循环

**目标：** 把一条 `kind=url` 的来源从 `pending` 拉到 `complete`，中途断掉能从已落地字节
处接着下。

**文件：**
- 创建：`cmd/lober/downloader.go`
- 测试：`cmd/lober/downloader_test.go`
- 修改：`cmd/lober/wire.go` —— 装配

**接口契约：**

```go
// downloader 实现 console.SourceFetcher 的下载半边。
type downloader struct {
    sources    contract.ArtifactSourceStore
    clock      contract.Clock
    guard      urlGuard
    incomingDir string
    client     *http.Client   // Transport 挂了 guard.CheckIP 的 DialContext
}

// fetch 取一条来源的字节直到齐备或失败。同步执行，调用方负责起 goroutine。
func (d *downloader) fetch(ctx context.Context, sourceID string) error
```

**循环行为**（设计 §4）：

1. 读来源，`ResumeOffset(src.ReceivedBytes, 盘上 data 的实际大小)` 算起点。
2. 起点 > 0 时发 `Range: bytes=<起点>-`；置 `state=fetching`。
3. 远端 **206** → 以 `O_APPEND` 追加写；**200**（不支持 Range）→ 截断 `data` 从头开始、
   起点归零；**416** → 说明远端文件比已落地字节还短，置 `failed` 而非当成已完成。
4. 每满 8 MB **或** 每 2 秒（取先到者）刷新一次进度。
5. 齐备 → 算 sha256 → `ExpectedSHA256` 非空时比对 → `complete` 或 `failed`。
6. `complete` 后调 `TriggerImportIfReady`。

**重定向：** `http.Client.CheckRedirect` 最多 3 跳，每跳对新 URL 调 `guard.CheckURL`。
IP 层的校验由 `DialContext` 自动覆盖每一跳（不需要额外接线，但要有测试证明）。

- [ ] **红：** 用 `httptest.Server` 覆盖：完整下载一次成功；带 `Range` 的续传（服务器按
      `Range` 头返回 206 与剩余字节，断言最终文件内容完整）；服务器返回 200 忽略 Range
      时截断重下；416 置 failed；`ExpectedSHA256` 不符时置 `failed` 且 `FailReason` 非空；
      无 `Content-Length` 时 `TotalBytes` 保持 0 而下载仍能完成；重定向 3 跳内成功；
      重定向第 2 跳指向 `127.0.0.1` 被拒；重定向超过 3 跳报错。
      **「fsync 先于更新库」**：构造一个在进度刷新后立即中断的场景，断言盘上字节数
      ≥ 库记录的 `ReceivedBytes`（反向不成立才是 bug）。
      运行 `go test ./cmd/lober/ -run TestDownloader -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。**顺序是 `f.Sync()` 然后才 `UpdateArtifactSourceProgress`**，
      不可颠倒（设计 §4）：先写库后落盘会在断电时产生「库说收了 3 GB，盘上只有 2.9 GB」，
      续传从错误偏移接着写，产出一个长度对、内容错位的文件。宁可重传一段也不要错位。

- [ ] **重构 + commit：** commit（`feat(cmd): 远程下载与 Range 续传`）。

**风险：** `httptest.Server` 监听在 `127.0.0.1` 上，而任务 7 的 guard **拒绝 loopback**。
测试里必须给 guard 注入一个「允许 loopback」的测试配置（多加一个 `allowLoopback bool`
字段，生产装配恒为 false），否则每个下载测试都会被自己的安全校验拒掉。**不要为了让测试
跑起来而放宽生产默认值**——那会让 SSRF 防护在生产环境静默失效。

---

### 任务 9：启动恢复与 `incoming/` 位置断言

**目标：** 进程重启后在途下载能接着跑，且 `incoming/` 绝不落在 nginx 直发路径下。

**文件：**
- 创建：`cmd/lober/sourcerecover.go`
- 测试：`cmd/lober/sourcerecover_test.go`
- 修改：`cmd/lober/preflight.go` —— 加位置断言
- 修改：`internal/config/config.go:227-230` —— `Store` 加 `IncomingPath`

**接口契约：**

```go
// recoverSources 扫 state=fetching 的来源，对每条重新触发取字节。
// 返回恢复了几条，供启动日志记录。
func recoverSources(ctx context.Context, sources contract.ArtifactSourceStore, fetcher console.SourceFetcher) (int, error)

// checkIncomingOutsideArtifacts 断言 incomingPath 不是 artifactPath 的子目录，
// 也不等于它。两者都先经 filepath.Abs 与符号链接解析。
func checkIncomingOutsideArtifacts(incomingPath, artifactPath string) error
```

**配置：** `Store.IncomingPath` 默认值 `/srv/lober/incoming`（与
`docs/contracts/filesystem.md` 的新增契约一致）。`Validate` 里调
`checkIncomingOutsideArtifacts`，不通过则启动失败。

- [ ] **红：** 覆盖 `recoverSources`：库里 2 条 `fetching` + 1 条 `complete` + 1 条
      `pending` 时只对那 2 条触发（`pending` 的不管——它可能是等着用户上传的 upload 来源，
      服务端主动去取毫无意义）；空库返回 0 不报错。
      覆盖 `checkIncomingOutsideArtifacts`：`incoming` 是 `artifacts` 的直接子目录（拒）；
      是深层子目录（拒）；两者相等（拒）；两者是兄弟目录（放行）；`incoming` 是
      `artifacts` 的父目录（**放行**——制品树在来源目录之下不构成泄露，反向才是）；
      用符号链接把 `incoming` 指进 `artifacts`（拒，这是 `filepath.EvalSymlinks` 的价值，
      纯字符串前缀比较会漏掉它）。
      运行 `go test ./cmd/lober/ -run "TestRecoverSources|TestCheckIncoming" -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。子目录判定**不要用 `strings.HasPrefix`**——`/srv/lober/artifacts2`
      的前缀是 `/srv/lober/artifacts`，会被误判成子目录。用 `filepath.Rel` 后检查结果是否
      以 `..` 开头。

- [ ] **重构 + commit：** `go run ./cmd/lober -config deploy/lober.yaml.example -check`
      通过。同时改 `deploy/nginx/` 的配置注释，写明 `incoming/` 刻意不在任何 `location`
      之下。commit（`feat(cmd): 来源启动恢复与 incoming 目录位置断言`）。

**风险：** `recoverSources` 在启动时跑，而 `-check` 模式（`go run ./cmd/lober -check`，
systemd 的 `ExecStartPre` 用它）**不该真的开始下载**——那会让一次配置检查悄悄拉起几 GB
的传输。位置断言要进 `-check`，恢复动作不要进。这两者的分界写进注释。

---

# 阶段五：分片上传

### 任务 10：来源列表与分片登记端点

**目标：** 让前端能查到「哪些片已经收到」，这是断点续传的前提。

**文件：**
- 修改：`internal/console/source.go` —— 加 2 个端点
- 测试：`internal/console/source_test.go`

**端点契约：**

| 端点 | 行为 |
|---|---|
| `GET /artifact-sets/:id/sources` | 返回该制品集全部来源；upload 来源额外带 `receivedChunks []int`（已落地的分片序号，升序） |
| `POST /artifact-sets/:id/sources` | 登记一条来源（补登记，如先建制品集再加 netboot）；返回 `sourceId` 与服务端的 `chunkSize` |

两者都注册进 `routesArtifactSet`（`internal/console/artifact.go:112-117`），权限码
`codeArtifactImport`，审计动作名 `artifact_source.list` / `artifact_source.create`。
`chunkSize` 由服务端决定（配置项，默认 8 MB）并回给前端——**不接受前端指定**，否则
`ValidChunkIndex` 的边界（按 `TotalBytes / chunkSize` 算）在两边可能不一致。

`receivedChunks` 的来源是扫 `data.part/` 目录里的文件名。**只列文件大小正确的片**：
上传中途断开会留下一个不完整的片文件，把它算作「已收到」会让前端跳过重传，最终拼出一个
中间少了几个字节的文件。末片的期望大小是 `TotalBytes % chunkSize`（余数为 0 时是
`chunkSize`）。

- [ ] **红：** 覆盖：列表返回 3 条来源且按 Role 升序；upload 来源的 `receivedChunks` 反映
      磁盘实际（预先造 3 个分片文件，断言返回 `[0,1,2]`）；**大小不足的分片不计入**（造一个
      半截的片，断言它不在返回里）；末片大小按余数判定（造一个正确的末残片，断言计入）；
      制品集不存在时 404；`POST` 重复登记同一个 `role` 时 409（唯一索引，任务 4）。
      运行 `go test ./internal/console/ -run "TestListSources|TestCreateSource" -count=1 -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。扫目录属于 IO，而 console 是 handler 层——扫描能力由装配点经
      `SourceFetcher` 的一个新方法（`ReceivedChunks(sourceID string) ([]int, error)`）注入，
      不要在 console 里直接 `os.ReadDir`。

- [ ] **重构 + commit：** commit（`feat(console): 来源列表与补登记端点`）。

**风险：** `data.part/` 里的文件名是分片序号，用 `os.ReadDir` 读出来是**字符串排序**
（`"10"` 排在 `"2"` 前面）。必须 `strconv.Atoi` 后按数值排序，否则 `receivedChunks` 返回
一个乱序列表，前端据此算「缺哪些片」会算错。

---

### 任务 11：分片 PUT 端点

**目标：** 接收单个分片并流式落盘，幂等，且路径穿越无处可施。

**文件：**
- 修改：`internal/console/source.go` —— 加 1 个端点
- 创建：`cmd/lober/uploadstore.go` —— 落盘实现
- 测试：`internal/console/source_test.go`、`cmd/lober/uploadstore_test.go`

**端点：** `PUT /artifact-sets/:id/sources/:sourceId/chunks/:n`

**接口契约：**

```go
// SourceChunkWriter 是控制台写分片需要的最窄接口，实现在 cmd/lober。
type SourceChunkWriter interface {
    // WriteChunk 把 r 的内容落成第 n 片。重复写同一片直接覆盖（幂等）。
    // 返回落地的字节数。
    WriteChunk(ctx context.Context, sourceID string, n int, r io.Reader) (int64, error)

    ReceivedChunks(ctx context.Context, sourceID string) ([]int, error)
}
```

**请求体是裸字节流**，不是 multipart。`Content-Type: application/octet-stream`。理由：
multipart 要解析边界，而 gin 的 `c.FormFile` 会把整片读进内存或临时文件
（`MaxMultipartMemory` 默认 32 MB，8 MB 的片会进内存但更大的片会额外落一次临时文件，
等于同一份字节写两遍盘）。裸流直接 `io.Copy` 到目标文件。

**校验顺序**（前一条不过就不做后一条）：
1. 来源存在且 `Kind == upload`、`State != complete`（已完成的来源不接受新片）
2. `n` 经 `model.ValidChunkIndex(n, src.TotalBytes, chunkSize)` —— 这是路径穿越的闸门
3. 请求体大小不超过 `chunkSize`（末片可以更小）

`sourceID` 来自 URL 但**不直接进路径**：先用它查库，查到的记录的 `ID` 才用于拼路径。查不到
就 404，于是 `../` 之类的值在进文件系统之前就被数据库拒绝了（设计 §6.1）。

**不写审计**（设计 §6.3）：一张 5 GB 的 ISO 有 640 片，逐片写审计会把审计表冲垮。但
**RBAC 与认证照走**——端点权限码 `codeArtifactImport`。

- [ ] **红：** 覆盖：正常写一片后 `ReceivedChunks` 含它；同一片重复 PUT 返回成功且内容被
      覆盖（写两次不同内容，断言第二次的内容生效）；`n = -1`（400）；`n` 超界（400）；
      `sourceId` 是 `../etc/passwd`（404，且**文件系统上没有任何新文件产生**——断言
      `incoming/` 之外无写入）；来源 `Kind != upload`（409）；来源已 `complete`（409）；
      请求体超过 `chunkSize`（413 或 400）；未认证时 401。
      运行 `go test ./internal/console/ ./cmd/lober/ -run "TestPutChunk|TestWriteChunk" -count=1 -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。`WriteChunk` 用 `io.Copy` 到一个 `O_CREATE|O_TRUNC|O_WRONLY`
      打开的文件，**写完 `Sync()`**——不 sync 的片在断电后可能是空文件，而任务 10 的
      「只列大小正确的片」会把它识别成缺片并重传，这是正确的降级；但如果不 sync 且大小
      恰好对（page cache 里有、盘上没有），拼接时会读到一段零字节。

- [ ] **重构 + commit：** 显式设 `router.MaxMultipartMemory`（即使不用 multipart，防止
      将来有人加了 multipart 端点时继承一个 32 MB 的默认值）。commit
      （`feat(console): 分片上传端点`）。

**风险：** gin 对 `PUT` 的裸 body 不做任何大小限制，`io.Copy` 会一直读到客户端停止发送。
必须用 `http.MaxBytesReader` 包一层（上限 `chunkSize`），否则一个恶意客户端用一个声明
`totalBytes=100` 的来源往单片里灌几十 GB，磁盘被打满而所有校验都还没来得及跑。

---

### 任务 12：`complete` 端点与拼接校验

**目标：** 把 N 个分片拼成 `data`、算 sha256、置 `complete` 并触发导入。

**文件：**
- 修改：`internal/console/source.go` —— 加 1 个端点
- 修改：`cmd/lober/uploadstore.go` —— 拼接与校验
- 测试：同上两个测试文件

**端点：** `POST /artifact-sets/:id/sources/:sourceId/complete`

**接口契约：**

```go
// AssembleSource 把全部分片按序号拼成 data，算 sha256 并写回来源状态。
// 缺片时返回错误，错误信息列出缺哪些序号。同步执行，调用方起 goroutine。
func (u *uploadStore) AssembleSource(ctx context.Context, sourceID string) error
```

**行为：**
1. 校验分片齐备（`ReceivedChunks` 的长度 == `ChunkCount`，且序号连续无缺）。缺片则
   **不拼接**，返回 409 且文案列出缺失序号（前端据此只重传缺的片）。
2. 按序号升序 `io.Copy` 拼成 `data`，`Sync()`。
3. 算 sha256，`ExpectedSHA256` 非空时比对，不符置 `failed`。
4. 成功 → 置 `complete`、`ActualSHA256` 写回 → **立即删 `data.part/`**（设计 §9 风险 6：
   分片与拼接结果同时存在会让磁盘峰值翻倍）。
5. 调 `TriggerImportIfReady`。

端点本身**异步触发拼接后立即返回**（拼接 5 GB 要读写各一遍，同步跑会让请求挂几分钟），
与 `ArtifactImporter` / `ArtifactVerifier` 已确立的「异步 job，端点立即返回」纪律一致
（`internal/console/artifact.go:91-96`）。写审计（设计 §6.3）。

- [ ] **红：** 覆盖：3 片齐备时拼出的 `data` 内容等于三片顺序拼接；缺中间一片时 409 且
      文案含缺失序号；`ExpectedSHA256` 相符时置 `complete`；不符时置 `failed` 且
      `FailReason` 非空；拼接成功后 `data.part/` 已删除；来源 `Kind != upload` 时 409；
      重复调 `complete`（已 `complete`）时幂等返回成功而不重复拼接。
      运行 `go test ./internal/console/ ./cmd/lober/ -run "TestCompleteSource|TestAssemble" -count=1 -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。拼接顺序必须按**数值**排序（承接任务 10 的风险），字符串排序
      会把第 10 片拼在第 2 片前面——产出的 ISO 文件大小完全正确、sha256 校验失败，而如果
      运维没填 `ExpectedSHA256`，这个文件会一路走到导入阶段才以「ISO9660 结构损坏」失败。

- [ ] **重构 + commit：** commit（`feat(console): 分片拼接与完成端点`）。

**风险：** 拼接过程中若进程被杀，`data` 是半截的而 `data.part/` 还在。拼接期间来源用
`fetching` 状态，于是 `recoverSources`（任务 9）会扫到它——但任务 9 写的时候只有下载一条
路径，它会对一个 upload 来源调下载逻辑（`URI` 是空的，下载立刻失败并把来源置 `failed`，
而分片明明都还在）。**本任务必须回去改 `cmd/lober/sourcerecover.go`**：按 `Kind` 分流，
`upload` 走重新拼接（分片都在，重拼幂等），`url` 走下载。这是本计划里唯一一处后续任务要
回改前面任务文件的地方，`sourcerecover.go` 的注释要写明两条分支各自的理由。

---

# 阶段六：容量与清理

### 任务 13：大小上限、磁盘水位与过期清理

**目标：** 让「打满磁盘」这件事在开始接收之前就被拒绝，并让没人管的半截来源自己消失。

**文件：**
- 修改：`internal/config/config.go` —— `Source` 配置节补三项
- 修改：`cmd/lober/uploadstore.go`、`cmd/lober/downloader.go` —— 接水位检查
- 创建：`cmd/lober/sourcecleanup.go` —— 过期清理循环
- 测试：`cmd/lober/sourcecleanup_test.go`、`internal/config/config_test.go`

**配置契约：**

```yaml
source:
  chunk_size: 8388608          # 8 MB，分片大小；服务端权威值，前端从端点取
  max_source_bytes: 17179869184 # 16 GB，单条来源上限
  stale_after: 168h            # 7 天，未完成来源的过期时长
  url_allowlist: []            # CIDR 列表，空表示不启用（任务 7）
```

**接口契约：**

```go
// checkDiskSpace 断言 incomingDir 所在文件系统的剩余空间够放 needBytes。
// 不足时返回的错误必须含「还缺多少字节」。
func checkDiskSpace(incomingDir string, needBytes int64) error

// cleanupStaleSources 删除 state in (pending, fetching) 且 UpdatedAt 早于
// now-staleAfter 的来源：先删目录，再删记录。返回删了几条。
func cleanupStaleSources(ctx context.Context, sources contract.ArtifactSourceStore, incomingDir string, now, staleAfter int64) (int, error)
```

**水位按 2 倍预留**（设计 §9 风险 6）：upload 来源在拼接期间分片与 `data` 同时存在，需要
`2 * TotalBytes`。download 来源只需 `1 * TotalBytes`。两者的差异写进注释。

**上限校验的两个时机**：`kind=url` 在拿到 `Content-Length` 时（拿不到就在下载过程中累计
超限时中止并置 `failed`）；`kind=upload` 在登记时（前端已给 `TotalBytes`）。

**清理循环**：启动时跑一次 + 每 6 小时一轮。用注入的 `contract.Clock` 取时刻，不用
`time.Now()`（架构约束 3）。`complete` 与 `failed` 的来源**不清理**——前者要留给
`reimport` 与重新校验（设计 §6.4），后者要留给运维看 `FailReason`。

- [ ] **红：** 覆盖 `cleanupStaleSources`：造 4 条来源（`pending` 且过期、`fetching` 且
      过期、`pending` 未过期、`complete` 且过期），断言只删前两条且**第四条的目录仍在**；
      目录删除失败时记录仍删（否则一条永远删不掉的记录会让每轮清理都报同一个错）；空库
      返回 0。覆盖 `checkDiskSpace`：needBytes 为 0 时放行；needBytes 大于剩余空间时报错
      且错误信息里的缺口数值正确。覆盖配置：`chunk_size` 为 0 或负数时 `Validate` 报错；
      `max_source_bytes` 为 0 时报错（不能是「无限制」——那让上限校验静默失效）。
      运行 `go test ./cmd/lober/ ./internal/config/ -run "TestCleanupStale|TestCheckDiskSpace|TestSourceConfig" -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。`checkDiskSpace` 在 Windows 与 Linux 上取剩余空间的系统调用
      不同（`GetDiskFreeSpaceEx` vs `statfs`）——本机是 Windows 而生产是 Linux。用
      `golang.org/x/sys` 或拆成两个 `_windows.go` / `_linux.go` 文件；**不要用
      `syscall.Statfs` 直接写**，那在 Windows 上编译不过，会让本机 `go build ./...` 直接
      失败。

- [ ] **重构 + commit：** `make check` 全绿。commit（`feat(cmd): 来源容量上限、磁盘水位与
      过期清理`）。

**风险：** 清理的顺序是**先删目录再删记录**。反过来（先删记录）会在删目录失败时留下一个
没有任何记录指向的孤儿目录，占着几 GB 磁盘而运维在控制台里看不到它的存在——那种空间只能
靠人登上服务器 `du` 才能发现。

---

# 阶段七：前端

### 任务 14：API 层与分片上传 composable

**目标：** 把 5 个来源端点接进前端，并把分片上传与重试逻辑做成独立于视图的可测单元。

**文件：**
- 修改：`web/apps/web-ele/src/api/console/artifact.ts`
- 创建：`web/apps/web-ele/src/composables/useChunkedUpload.ts`

**类型契约**（`artifact.ts`）：

```ts
export type SourceKind = 'local' | 'upload' | 'url';
export type SourceState = 'complete' | 'failed' | 'fetching' | 'pending';
export type SourceRole = 'iso' | 'netboot' | 'winpe-bcd' | 'winpe-boot-sdi'
  | 'winpe-boot-wim' | 'winpe-wimboot';

export interface ArtifactSourceItem {
  expectedSha256: string; id: string; kind: SourceKind;
  receivedBytes: number; receivedChunks?: number[]; role: SourceRole;
  state: SourceState; totalBytes: number; uri: string; failReason: string;
}
```

**顺带修两个现存缺口**（核实时发现，不是 spec C 的要求但属同一文件）：
`ArtifactSetItem` 缺 `failReason` 字段（后端 `internal/console/artifact.go:136` 有
`json:"failReason"` 且在发，前端接口没声明、视图也没用），以及 `ArtifactSetCreateParams`
缺 `netbootPath`（后端 `artifact.go:184` 支持，前端从未传过——Debian 家族的 PXE 装机需要
它，所以现在**没法从界面登记一个能装 Debian 的制品集**）。两者在本任务补上。

**composable 契约：**

```ts
export function useChunkedUpload(): {
  progress: Ref<{ received: number; total: number }>;
  upload(setId: string, sourceId: string, file: File,
         chunkSize: number, alreadyHave: number[]): Promise<void>;
  cancel(): void;
};
```

- `alreadyHave` 是 `GET .../sources` 返回的 `receivedChunks`，**跳过这些片**——这就是断点
  续传在前端的全部实现。
- 单片失败重试 3 次，指数退避。三次都失败则 reject，**不静默跳过**（跳过会让 `complete`
  端点报缺片，而用户以为传完了）。
- 并发度 3（不是 1 也不是无限）：串行传 5 GB 在千兆网下受单连接 RTT 限制，而无限并发会
  让 640 个 fetch 同时排队把浏览器打死。

- [ ] **红：** 为 composable 写失败测试（`vitest`，mock `fetch`）：全新上传 3 片按序完成、
      `progress` 最终等于文件大小；`alreadyHave=[0,2]` 时只 PUT 第 1 片；单片失败重试后
      成功；单片三次失败后整体 reject；`cancel()` 后不再发起新请求。
      运行（命令已实测可用，2026-09-29；配置在 `web/vitest.config.ts`，环境是 happy-dom）：

      ```bash
      export PATH="/c/Program Files/nodejs:$PATH"
      cd web && node node_modules/vitest/vitest.mjs run apps/web-ele/src/composables
      ```

      预期 FAIL。首次跑约 60 秒（transform 15s + import 48s），不是卡住。现成参考：
      `apps/web-ele/src/adapter/__tests__/form-schema.test.ts`（22 个用例，本机实测全绿）。

- [ ] **绿：** 实现至通过。分片切分用 `File.slice(start, end)`——它返回的 `Blob` 是惰性的，
      不会把整个 5 GB 读进内存；用 `FileReader` 读整个文件再切会让浏览器直接 OOM。

- [ ] **重构 + commit：** `export PATH="/c/Program Files/nodejs:$PATH"` 后
      `cd web/apps/web-ele && node ../../node_modules/vue-tsc/bin/vue-tsc.js --noEmit --skipLibCheck`
      通过；`cd web && node node_modules/eslint/bin/eslint.js --fix <两个文件>` 无残留错误。
      commit（`feat(web): 来源端点接入与分片上传 composable`）。

**风险：** 页面刷新后 `File` 对象**不能恢复**（浏览器安全限制，无法持久化文件句柄）。
`receivedChunks` 还在服务端，所以续传是可能的，但**必须让用户重新选同一个文件**。UI 上要
明确写这句话（任务 15），否则运维会以为刷新页面就丢了，删掉重传 5 GB。

---

### 任务 15：来源三选一表单与进度展示

**目标：** 让创建弹窗能选三种来源，列表页能看出「还在传」与「传完了正在解压」的区别。

**文件：**
- 修改：`web/apps/web-ele/src/views/artifact-sets/index.vue`

**表单形态**（设计 §8）：单选 `ElRadioGroup` 切换三种来源，各自的输入控件互斥显示；
`kind=upload` 选中文件后显示进度条（`已传/总量` 与百分比）；「期望 SHA256」与「族」
（spec B §2.1 引入）两项对三种来源都显示。

**列表页**：`state` 列在 `importing` 之前插入来源阶段。现在这里只有制品集自己的四个状态
（`registerForm` 在 `index.vue:172`，状态渲染在 `index.vue:260-266`）。取数：轮询
`GET /artifact-sets/:id/sources`，任一来源 `state=fetching` 时显示 `传输中 61%`，
全部 `complete` 而制品集还是 `importing` 时显示 `解压中`。

**轮询节奏**：现有代码已有「有 importing 行时才轮询」的判定（`index.vue:152`），来源轮询
复用同一个开关，不要新加一个独立的定时器——两个定时器会让这个页面在有 10 个制品集时每秒
发 20 个请求。

- [ ] **红：** 前端此处无单元测试基础设施覆盖视图层（现有 `views/` 下无 `.test.ts`），
      本任务的红灯是**手工验证清单**，逐条在浏览器里走：三种来源各能提交成功；upload 的
      进度条随传输前进；传输中途刷新页面后重新选同一文件能接着传（只传缺的片）；列表页
      在传输期间显示「传输中 N%」、拼接/解压期间显示「解压中」、完成后显示 `ready`；
      `failed` 的制品集显示 `failReason`（承接任务 14 补的字段）。
      每条记录实际观察到的现象，不要只打勾。

- [ ] **绿：** 实现至手工清单全部通过。

- [ ] **重构 + commit：** vue-tsc 与 eslint 同任务 14。**如果为了验证打包跑了
      `vite build`，跑完立刻 `git restore internal/console/ui/dist/`**——那个 outDir 会被
      清空，而里面两个被 git 跟踪的占位文件（`index.html`、
      `_placeholder-keeps-embed-underscore-mode.js`）少了 `go:embed` 编译不过。
      commit（`feat(web): 来源三选一表单与传输进度展示`）。

**风险：** 5 GB 上传期间用户切到别的路由，composable 的 `upload()` promise 还在跑但组件已
卸载，`progress` ref 的更新会触发 Vue 的「更新已卸载组件」警告，更糟的是用户回来时看不到
进度。两个选择：在 `onBeforeUnmount` 里 `cancel()`（简单但用户切个页面就中断了），或把上传
状态提到 `store/` 里（正确但要多一个 store）。**选前者并在 UI 上提示「上传期间请勿离开
本页」**——后者要正确处理多个并发上传的状态隔离，超出本任务范围；而断了也能续传，代价可
接受。

---

# 阶段八：文档

### 任务 16：契约与 SPEC 同步

**目标：** 让文档与代码一致，特别是「不接受上传流」这条被本设计推翻的范围决定。

**文件：**
- 修改：`docs/contracts/http-console.md` —— 追加变更记录：制品集创建入参改 `sources`
  数组（旧 `sourcePath`/`netbootPath` 保留为兼容形态）、新增 5 个来源端点
- 修改：`docs/contracts/filesystem.md` —— 追加变更记录：`/srv/lober/incoming/<sourceID>/`
  布局，**并写明它刻意不在 nginx 的 `location /artifacts/` 之下**
- 修改：`SPEC.md §2.1` —— 「不接受上传流」改为三种来源；`§9.1` 补 SSRF 与上传端点的
  认证要求
- 修改：`deploy/nginx/` 配置注释 —— 写明 `incoming/` 不暴露

- [ ] **红：** 无红灯（纯文档）。先列出要改的 4 处，逐条打勾。

- [ ] **绿：** 逐处改写。`SPEC §2.1` 那条范围决定**不要直接删掉重写**——保留原文并注明
      「2026-09-29 推翻，理由：运维的 ISO 未必已在本机，先 scp 再填路径在有 Web 界面的
      前提下是纯粹的摩擦」。气隙环境的原始约束仍然成立（不连外网），改的只是「字节怎么
      进到这台机器上」。

- [ ] **重构 + commit：** `grep -n "不接受上传流\|不实现 WebUI 直传" SPEC.md docs/contracts/*.md`
      应只剩带「已推翻」注记的那一处。commit（`docs: 同步 ISO 三种来源与 incoming 目录契约`）。

**风险：** SPEC 里代码注释引用的小节号（`SPEC §2.1`、`§9.1` 这类）散布全仓。**只改小节
内容，不重排小节号**——重排会让几十处代码注释里的引用全部失效，而那种失效没有任何工具
能检测出来。

---

## 整体验收

全部任务完成后逐条确认。

### 构建与测试

- [ ] `export LOBER_TEST_PG_DSN='postgres://lober_test:lober_test_pw@localhost:5432/lober_test?sslmode=disable'`
- [ ] `make check` 四步全绿（fmt → vet → test → race）。测试失败集合**恰好等于**计划
      开头列的 4 个已知 `internal/render` 用例。出现任何其它失败即回归。
- [ ] `go test ./internal/archtest/ -count=1` 绿 —— 依赖方向守卫（含测试 import）。
      `internal/console` 不得 import `internal/imports` 或 `net/http` 的下载实现。
- [ ] `go run ./cmd/lober -config deploy/lober.yaml.example -check` 通过启动自检，**且
      不会开始任何下载**（任务 9 的分界）。
- [ ] 前端：`vue-tsc --noEmit --skipLibCheck` 与 `eslint` 均无错误（命令见测试命令约定节）。

### 规格逐条落地

- [ ] **需求 13（三种来源）**：`POST /artifact-sets` 接受 `kind=local|url|upload` 三种，
      各有一个 console 层测试。
- [ ] **§1.1 缺口已修**：来源落库，`TriggerImport` 从 Store 读来源组装 `ImportSource`。
      验证方式：登记一个 `kind=local` 来源 → 杀进程 → 重启 → 调 `POST .../reimport`
      → 导入成功。**这是「导入不再依赖某一次 HTTP 请求上下文」的证据。**
- [ ] **断点续传（下载）**：`httptest` 层有 Range 续传测试；真机层见下。
- [ ] **断点续传（上传）**：`receivedChunks` 反映磁盘实际、缺片只重传缺的那片，有测试。
- [ ] **SSRF 五条**：scheme / loopback / 链路本地 / 解析后 IP（含 `::ffff:127.0.0.1`）/
      重定向每跳，各有一个测试。`grep -c "func Test" cmd/lober/ssrf_test.go` ≥ 9。
- [ ] **路径穿越**：`sourceId = ../etc/passwd` 与 `n = -1` 各有一个测试，且断言
      `incoming/` 之外无文件产生。
- [ ] **认证不可绕过**：分片 PUT 端点在未认证时返回 401，有测试。
- [ ] **容量**：水位不足时错误信息含缺口字节数，有测试；过期清理只删 `pending`/`fetching`
      且先删目录后删记录，有测试。

### 验收判据（spec C §1，真机层）

- [ ] **远程下载续传**：起一个本地 HTTP 服务器托一张 ≥1 GB 的 ISO，登记 `kind=url` 来源
      → 传到一半 `kill` lober 进程 → 重启 → 观察 `receivedBytes` 从中断处继续增长 →
      最终 `complete` 且 `actualSha256` 与文件真实 sha256 相符 → 导入到 `ready`。
- [ ] **浏览器上传续传**：浏览器传一张 ≥1 GB 的 ISO → 传到一半刷新页面 → 重新选同一文件
      → 观察只传缺的片 → `complete` → 导入到 `ready`。
- [ ] 两条都要记录**实际观察到的 `receivedBytes` 变化**，不要只打勾。

### 不该变的东西没变

- [ ] **七个 e2e 脚本仍能跑**：`bash scripts/e2e.sh` 与 `bash scripts/e2e-debian.sh` 九阶段
      全绿。它们用旧形态 `sourcePath` 调创建端点（任务 5 的兼容路径），这是那条兼容性
      存在的唯一理由，也是验证它有效的唯一手段。
- [ ] `git diff docs/contracts/http-machine.md` 为空 —— 本计划完全不碰机器面。
- [ ] `go test ./internal/render/ ./internal/statemachine/ -count=1` 的失败集合等于已知的
      4 个 render 用例 —— 本计划不该碰这两个包。
- [ ] `grep -rn "time.Now()" --include="*.go" internal/console/ internal/store/postgres/source/ | grep -v _test`
      为空 —— 时间仍全部来自注入的 `Clock`。
- [ ] `incoming/` 不可从制品服务器取到：部署后 `curl <artifact_base>/artifacts/../incoming/`
      与直接猜路径均 404。

### 安全面复核

- [ ] `grep -n "allowLoopback" cmd/lober/*.go | grep -v _test` —— 生产装配处该值恒为
      `false`（任务 8 的测试开关不能泄漏到生产）。
- [ ] 分片 PUT 用 `http.MaxBytesReader` 限流，`grep -n "MaxBytesReader" internal/console/source.go`
      有命中。
- [ ] `router.MaxMultipartMemory` 已显式设值，不是 gin 的 32 MB 默认值。

