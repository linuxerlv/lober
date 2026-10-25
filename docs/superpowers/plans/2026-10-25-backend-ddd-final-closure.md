# 后端四层结构与模块契约收尾实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 关闭总审计的三项重要问题和三项轻微问题，使规则归属、应用入口、模块契约和自动检查相互一致。

**Architecture:** 纯执行规则归领域，机器请求和日志保存流程归执行应用，接口与外部适配器只做传输。模板候选共用一份领域资格判定；仓储取得同一次读取的事实后筛选、计数和分页。所有公开能力通过方法旁中文约定或明确的唯一来源引用说明八项契约。

**Tech Stack:** 现有Go、Gin、PostgreSQL、GORM、Wire及pnpm类型检查；不增加第三方依赖、数据库表或配置字段。

**Spec:** [已确认的四层设计](../specs/2026-10-22-backend-ddd-four-layer-design.md)、[全后端总审计](../reviews/2026-10-25-ddd-final-audit.md)、三份独立[结构](../reviews/2026-10-25-ddd-final-structure-review.md)、[编写/主机/镜像](../reviews/2026-10-25-ddd-final-authoring-hosts-media-review.md)、[任务/身份/审计](../reviews/2026-10-25-ddd-final-tasks-identity-audit-review.md)审查及现有`docs/contracts/*.md`。

状态：Task1–8实施、逐项独立审查、整分支源码/契约复核和新真实提交的原完整VM验收均已完成。原六项及本轮新增问题全部关闭，严重0、重要0、轻微0待修；功能提交为`76ef20aadba11f3632e26f21dcf9acb883bfb737`。最后文档范围核对与提交条目仅随本次文档提交生效完成，在此候选冻结时没有此前已发生的最终文档提交。

[独立计划审阅](../reviews/2026-10-25-ddd-final-closure-plan-review.md)的原计划结论保留；实际实施、失败/重放、关闭证据和未验边界见[收尾证据](../reviews/2026-10-25-ddd-final-closure-evidence.md)及[最终审查](../reviews/2026-10-25-ddd-final-closure-review.md)。本轮只交付`codex/ddd-final-closure`，未推送、合入dev或晋级其他环境。

## Global Constraints

- 起点源码为dev `3de1bcdd943ce680906e0a09e8afa107e75d789d`。计划确认后，从包含本文且业务代码与该起点一致的dev文档后继提交建立`codex/ddd-final-closure`及独立工作区；不把dev工作区两项未提交UI改动带入。保留所有原分支、工作区、私有证据和VM目录。
- 本机只读源码、编辑文件和操作Git；**所有Go、gofmt、Wire、Node、构建和测试只在lober VM**。连接方式读取忽略的`docs/linux-test-environment.private.md`，不提交或输出秘密、DSN及原始私人日志。
- 沿用`GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=off`、`LOBER_TEST_BASH=/bin/bash`、`LOBER_TEST_BUSYBOX=/usr/bin/busybox`、`WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0`。完整竞态保持`go test -race -timeout=20m ./...`。不改密码强度、会话缓存、超时或既有断言。
- 保留网页和机器协议、冻结数据、错误优先级、审计原子性、通知位置、数据库版本/锁及既有不可用行为。只允许Task2纠正取消类型、Task6明确收紧单次候选读取的一致性；二者单独提交并记录变化。
- 不新增装机能力、消息必达、持久会话、跨页快照或跨实例立即撤权。不将未确认的跨实例删除闸门风险混进本计划，也不把它标成已验证通过。
- 不引入通用事务回调到应用，不向上层传GORM/SQL事务。写事务继续READ COMMITTED；Task6的只读一致读取仅限该私有查询，不改变`Store.write`。
- 四张过渡表保持逐表0，39/68/154精确删除历史及六项正式编号能力保持。不得靠加临时豁免、扩大包许可、减少扫描范围或删除反例使检查通过。
- 每项实施者只拥有本任务列明文件；共享文件按Task顺序修改。实施者不得撤销其他人改动。VM测试由根统一串行调度，不并行争用测试库和资源。
- 每项完成后，由未参与该项实施的人先核对契约，再核对代码与VM证据；严重、重要问题修复并复审后才能进入下一项。最后另做整分支审查。不得用上一阶段PASS代替本次结果。
- 本计划交付模块分支；合入dev、推送及test/preview/main晋级另按用户指令执行。计划通过不等于发布授权。

## Review Focus

1. 额外来源摘要已通过，随后打开/读取被取消：保持取消错误、原状态和关闭资源；Task2的`Test镜像额外源读取取消超时类型与状态保持`覆盖。
2. Boot已提交后渲染失败、应答渲染期间发生取消、精确重复信号：不假装回滚、不泄出被取消的应答、不改变旧重复例外；Task3/4固定这些交错。
3. File、InstallerAnswer、Signal的观察时机不同，观察验证失败也可能仍应处理Signal；Task4的`Test机器入口保留观察时机与双重验证`覆盖，不能改成统一“成功后观察”。
4. 第二批模板/组件才有不合格项，期间又发生审批、废弃或改名：不漏计数、不混用事实、不偷偷截断；Task6的批次、快照和取消测试覆盖。
5. 新文件、自建改名接口、未调用的声明、方法值与函数字段：违规必须真实类型检查成功后被拒绝，合法输出/事件流仍通过；Task7的正反例矩阵覆盖。

---

## 1. 六项问题的关闭条件与任务依赖

| 审计编号 | 关闭条件 | 负责任务 |
| --- | --- | --- |
| I1 执行链职责 | 纯状态/选择规则归领域；机器与日志流程经应用；组装和适配器不再持有业务步骤；协议回归不变 | 3、4、5，7保护，8复核 |
| I2 自动检查漏拦 | 所有接口生产文件、声明与能力均受查；机器接口无仓储许可；所有应用模块外部操作受限；真实反例转绿且合法传输不误拒 | 7；依赖3–6接口落定 |
| I3 取消类型丢失 | 三处额外源错误分支保留包装后的取消/超时；不写failed/来源错误；非取消问题沿用旧分类 | 2 |
| M1 契约表达 | 既有公开操作逐项可追溯八项约定，新能力随实现一并写全；没有裸签名冒充完整契约 | 1及3–6，8核完整清单 |
| M2 分页文字 | 镜像游标默认100、页码默认20、上限500与代码相符，不改行为 | 1 |
| M3 双份筛选规则 | 游标与编号查询调用同一领域函数，删除手写资格SQL；分页、当前版本、精确引用和输出规则保留 | 6 |

顺序：`1 → 2 → 3 → 4 → 5 → 6 → 7 → 8`。Task2与6的源码虽相对独立，本轮仍顺序交付，避免共享契约、守卫和VM相互干扰。Task7依赖新入口完成，不能提前封禁现有生产路径后再给它开例外。

三种取舍已按原设计落定：不重写执行协议，只迁移规则和编排；不以“登记永久例外”关闭M3，采用领域统一判定和有界扫描；不再给旧文件名单打补丁，改为全文件与真实类型能力检查。这些是本文提出的实现安排，尚非已实施事实。

## 2. 文件与职责地图

| 位置 | 职责 | 所有者 |
| --- | --- | --- |
| `docs/contracts/application-capabilities.md`（新） | 操作到唯一契约来源的导航清单，不再复制一套行为规范 | 1，后续任务补新增项 |
| `docs/contracts/machine-execution.md`（新） | 冻结现有机器协议、执行和日志的八项约定及变化边界 | 1，3–5顺序补充 |
| 既有应用`*_contract.go`、`port/*.go`与共用`ports` | 方法旁约定或精确委托，类型和实现保持对应 | 1及新增能力所属任务 |
| `internal/application/media/import.go`及取消测试 | 取消类型纠正，不改普通介质错误 | 2 |
| `internal/domain/execution/`（新） | 纯执行、控制、交付选择和日志有效性规则 | 3 |
| `internal/application/execution/` | 读事实、调用领域、条件保存、凭据技术机制、机器入口和日志接收 | 3–5 |
| `internal/interfaces/machine/http.go` | HTTP解析、状态映射和输出 | 4 |
| `internal/infrastructure/events/syslog.go` | IP租约、网络字节处理，委托应用收日志 | 5 |
| `internal/domain/selection/template.go`（新）与模板候选读取 | 一份领域资格判定；仓储批读事实、计数和分页 | 6 |
| `internal/archtest/` | 四层、全文件/能力、外部操作、规则归属和历史保护 | 各任务最小同步，7整体收紧 |
| `cmd/lober/machine.go`与相关组装测试 | 连接新能力，只做普通字段转换和启停 | 4、5顺序修改 |

## Task 1：补齐现有能力契约与分页说明（M1、M2）

**Files:** 创建`docs/contracts/application-capabilities.md`、`docs/contracts/machine-execution.md`；修改`docs/contracts/{backend-modules,authoring-preview,hosts,media,identity-audit,task-read-control-events}.md`中确有缺项或来源未连接的部分。

方法旁核对范围精确为：

- `internal/application/tasks/{contract,read_contract,control_contract,event_contract}.go`、`tasks/port/{repository,task_read,control}.go`。
- `internal/application/authoring/{hooks_contract,templates_contract,editor_contract,preview_contract,bootstrap_contract,schema_contract,machine_render}.go`、`authoring/port/{repository,bootconfig}.go`。
- `internal/application/hosts/contract.go`及`hosts/port/repository.go`；`internal/application/media/contract.go`及`media/port/{repository,control,transfer,import,deletion,coordination}.go`。
- `internal/application/identity/contract.go`、`identity/port/{repository,credentials}.go`；`internal/application/audit/contract.go`、`audit/port/repository.go`。
- `internal/application/ports/{actor,audit,clock,notification,preview,selection}.go`；`internal/domain/repository/{users,role_menus,templates,hooks,tasks,current_tasks,events,audit}.go`及`doc.go`的保存约定。已有完整说明只登记，不为统一格式重写。
- `internal/application/execution/{service,token,control_intent,observation}.go`及`execution/port/observation.go`的现有公开能力；3–5新增或改变的入口由其所属任务更新清单。

**Interfaces:** 不改方法集、类型、行为或生成代码。能力清单为`包.接口.方法 → 方法定义位置 → 唯一约定及章节 → 八项各自来源/不适用原因 → 调用方与验证位置`。共享说明可引用，不能只写“见文档”或把多个规范都当唯一来源。

- [x] 按总设计第6节盘点目的/权限、输入、结果/顺序/空值、错误/优先级、写入原子性、重复/冲突、并发复查、审计/通知/取消八项；逐方法补中文说明或精确引用及自身差异。只读操作明确不涉及写入与提交复查的原因。
- [x] `machine-execution.md`先固定当前行为，再列本文3–5的目标入口，不把尚未实现的入口写成现状。对I3标清当前缺陷与预期纠正；对Task6标清单次查询事实一致性的细化，不承诺跨页一致。
- [x] 修正`media.md`为游标默认100、页码默认20、最多500；保持JSON、权限、错误顺序和当前所有分页代码。已批准的历史阶段和历史测试数字不改写。
- [x] 独立人工核对完整清单与真实调用；Git差异中Go仅允许普通注释变化，不改构建/嵌入等指令。纯说明不新增镜像实现的测试，不跑无意义全套；如果确需Go格式检查，只在VM执行。后续功能测试按各任务及Task8统一验收。
- [x] 契约独立审查通过后提交`docs(contracts): connect operation guarantees and pagination defaults`。M1只能先关闭既有能力部分；3–6新增能力必须继续满足同一清单，Task8才给最终结论。

## Task 2：保留额外镜像源取消与超时类型（I3）

**Files:** 修改`internal/application/media/import.go`、`import_cancel_order_test.go`；新建`internal/application/media/import_metadata_cancel_test.go`；核对`media/port/import.go`及`docs/contracts/media.md`取消条款。复用`import_test.go`、`import_order_test.go`的真实文件与数据库夹具，不改旧测试断言。

**Interfaces:** `(*Sources).Import(context.Context,string) error`不变。新增测试名为`Test镜像额外源读取取消超时类型与状态保持`、`Test镜像额外源打开取消类型保持`、`Test镜像额外源普通错误仍按原分类`。

- [x] 将审计6例移入正式回归：额外源Digest成功后，在两次`.disk/info`边界各注入Canceled/DeadlineExceeded（4例）；另两例用真实`context.WithCancel`在边界取消，再委托真实`MediaView.Text`。断言达到指定边界、`errors.Is`成立、ISO完整状态/Rev及来源Error不变、会话关闭一次。
- [x] 增加额外`OpenSource`返回取消/超时、包装取消错误的用例；普通坏介质/版本不符仍为原`iso_extra_source_invalid`。真实父取消后的检查使用`context.WithoutCancel`读库，不把查库被取消误当状态未变。
- [x] 在VM先运行`go test -count=1 -v -run '^Test镜像额外源(读取取消超时类型与状态保持|打开取消类型保持|普通错误仍按原分类)$' ./internal/application/media`；旧代码必须因错误类型或持久化事实断言失败，不能因数据库、依赖或夹具不合法失败。
- [x] 对额外源OpenSource及两处识别错误，先用`errors.Is`保留取消/截止错误，再走原介质映射。返回原包装错误，不把它变成新Problem；其他读取错误分类不扩改。不改变defer清理、发布、未知提交确认或后台重试政策。
- [x] VM运行`go test -count=1 ./internal/application/media ./internal/infrastructure/media ./internal/domain/media`，并核对原`Test镜像打开有效额外源时取消仍返回取消`及真实库用例均PASS、无Skip。原审计“4例合成能力写failed”在修复后也必须保持原状态，不能只修真实父取消。
- [x] 契约和代码独立审查通过后提交`fix(media): preserve cancellation across extra-source metadata reads`；记录这是明确的错误行为纠正，非目录搬迁。

## Task 3：纯执行与冻结交付规则归领域（I1第一部分）

**Files:** 创建`internal/domain/execution/{protocol,control,delivery,log}.go`及对应`*_test.go`；修改/拆分`internal/application/execution/{protocol,protocol_test,service,token,service_test,observation}.go`，凭据测试拆入`token_test.go`；修改`internal/archtest/{deps_test,machine_rules_test,execution_guard_test}.go`必要的精确域分类，补`execution_rules_test.go`。更新机器契约和能力清单。

**Interfaces:** 新域包只依赖model/catalog和必要纯标准库。`Signal`字段及顺序保持`Phase,Slot,Timing,Instance,Digest,RC,Body`，不新增JSON标签；`model.Execution`存储形态不变。应用可保留`type Signal = executionrules.Signal`及同一`ErrProtocol`哨兵别名作为正式薄入口，不保留第二份规则/错误对象。

新增域入口固定为：

```go
func CanBoot(model.Task) bool
func Boot(model.Task, catalog.Arch, catalog.Firmware, int64) (model.Task, error)
func BootDeliverable(model.Task) bool
func PreseedAvailable(model.Task) bool
func InstallerAnswerAvailable(model.Task) bool
func InstallingNeeded(model.Task) bool
func StartInstalling(model.Task, int64) (model.Task, bool) // bool为是否改变
func Advance(model.Task, Signal, int64) (model.Task, bool, error) // bool为完全相同重报
func SignalKey(Signal) string
func FinalSignal(Signal) bool
func SignalAccessAllowed(model.Task, Signal) bool
func InstallAccessNeedsTime(model.Task) bool
func InstallAccessAllowed(model.Task, int64) bool
func CanCancel(model.Task) []model.Problem
func CanRetry(model.Task) []model.Problem
func Cancel(model.Task, int64) (model.Task, []model.Problem)
func Retry(model.Task, int64) (model.Task, []model.Problem)
func Timeout(model.Task, int64) (model.Task, bool) // bool为本次判定已超时
func SelectFile(model.Snapshot, string) (FrozenFile, error)
func CanReceiveInstallerLog(model.Task, int) bool
```

`FrozenFile`为`{Kind catalog.FileKind; Slot string; Body []byte; Hook bool}`；Hook区分冻结正文与需渲染的文件，允许空正文。`ErrFileNotFound`和`ErrFrozenContent`归此域包。`clone/runtime/stage/preComplete/fingerprint/seen/slotsComplete`为域内私有帮助函数；`attemptKey`作为持久事件键转换留应用。StartInstalling只消费已通过现有GetAuthorized的非nil执行记录；InstallingNeeded共享其幂等谓词。InstallAccessNeedsTime与InstallAccessAllowed共享同一Deadline谓词，nil执行记录不得授权。

- [x] 先固定迁移前的Signal JSON指纹、事件键、期限边界及拒绝不改输入的黄金值；在VM确认与现实现相符。补`Test纯执行规则只归领域`，基于生产AST/实际对象检查当前应用仍拥有boot/advance/状态写入时应红；不以新包缺失或装载失败冒充反例。
- [x] 迁移原protocol纯规则及测试，保持引导3次、初始900秒、pre600/packages3600/post1200、安装阶段总上限IssuedAt+5400、reboot480/initializing900；deadline到达即拒绝。精确重报检查继续早于终态/deadline，但重启后的旧安装slot仍拒绝。
- [x] 同时抽出Service的Preseed、Answer、control、Sweep及Authorize状态条件。应用保留规范化、读库、取时钟、事件转换、CAS与通知：Boot冲突立即不给引导；Answer/Signal原最多8次；用户控制不自动刷新旧版本；Sweep原四状态、每页100、只吞CAS冲突。
- [x] 保留取时钟的位置：Answer已经installing不取新时钟；control先诊断再取时钟；安装授权仅InstallAccessNeedsTime为真才进行原额外时钟读取（取值前仍先完成原凭据验证），否则传入不被使用的0。用InstallingNeeded和InstallAccessNeedsTime决定取时钟，不在应用重写状态/Deadline条件；域内调用共享谓词不复制条件。SignalAccessAllowed在原凭据验证后判断，保持收尾信号及精确重报例外。
- [x] SelectFile照搬原冻结选择和校验顺序：只允许Exec插槽；`scripts/02.sh`不等于`scripts/2.sh`；坏摘要422所对应的域错误、未知文件404所对应的域错误分开；找到一个匹配hook后仍检查后续相同摘要项，不能提前返回漏掉损坏正文。
- [x] 凭据HMAC技术机制仍在应用，密钥外部注入：`lober-v1|ID|Attempt|IssuedAt|scope`、64位十六进制、常数时间比较、install/finalize及5400/86400寿命不变。状态授权条件调用域规则；领域不取得密钥、不读当前时间。`Credentials`仍安装在前、收尾在后，无Execution双空；NewIPObserver原有效期不改。
- [x] VM运行`go test -count=1 ./internal/domain/execution ./internal/application/execution ./internal/archtest`。原纯协议测试迁域保留断言，凭据/真实保存测试留应用；补`Test执行规则拒绝不改输入`、`Test执行信号指纹与事件键兼容`、`Test执行期限等号与重启后重报`、`Test冻结组件重复摘要后续损坏仍拒绝`。
- [x] 本任务只更新让新增域包正确受查的最小规则，不加宽machine/events许可。独立审查通过后提交`refactor(execution): centralize pure execution and delivery rules`。

## Task 4：机器请求经应用入口，HTTP只解析和输出（I1第二部分）

**Files:** 创建`internal/application/execution/{gateway_contract,gateway,gateway_test}.go`、`execution/port/render.go`；修改`internal/interfaces/machine/{http,http_test}.go`、`cmd/lober/{machine,machine_test,machine_delivery_test,assembly_test}.go`及相关`execution_guard_test.go`、`task_finalize_rules_test.go`普通适配正例。补机器契约与能力清单。`machineOf`保持原provider形状，Wire通常无需变化；漂移检查必须在VM确认，不手改`wire_gen.go`。

**Interfaces:** 删除HTTP侧Engine/Renderer/Observer/业务Request/Endpoints依赖，改为`machine.New(execution.Gateway) http.Handler`。新能力为：

```go
type Gateway interface {
    BootMenu(context.Context, BootInput) (BootResult, error)
    InstallerAnswer(context.Context, InstallerAnswerInput) (BootResult, error)
    File(context.Context, FileInput) ([]byte, error)
    Signal(context.Context, SignalInput) error
}
```

输入值分别为`BootInput{SN string; Arch catalog.Arch; Firmware catalog.Firmware}`、`InstallerAnswerInput{SN,RemoteAddr string}`、`FileInput{TaskID,Token,Name,RemoteAddr string}`、`SignalInput{TaskID,Token,RemoteAddr string; Signal executionrules.Signal}`；`BootResult{Body []byte; Found bool}`。Found=false由HTTP输出原本地盘文本`#!ipxe\nexit || sanboot --no-describe --drive 0x80\n`，这是传输表示，不在HTTP里判断任务状态。

`NewGateway(GatewayDeps) (Gateway,error)`；Deps为本模块`*Service`、`port.FrozenRenderer`、普通Endpoints四地址和可nil的`func(model.Task,string)`观察函数。缺必需服务/渲染器返回构造错误，不新增配置。`port.RenderRequest{Task model.Task; Kind catalog.FileKind; Slot string; Values map[catalog.SystemValue]string}`；`FrozenRenderer.Render(context.Context,RenderRequest)([]byte,error)`及同签名`RenderFunc`方法适配。cmd仅转换字段委托authoring.MachineRenderer，应用模块之间不互导。

- [x] 先加Gateway行为与HTTP表驱动测试，在VM对现HTTP链固定结果，再用新增职责反例证明旧HTTP仍有快照选择/摘要/推进编排；旧业务断言迁入Gateway测试，不删减成只测代理转发。
- [x] 严格保持四条顺序：Boot先提交再渲染；InstallerAnswer为Preseed→格式资格→渲染→安装凭据→Answer→观察；File为GetAuthorized→观察→SelectFile→渲染/正文→主应答Answer；Signal仅在观察函数非nil时做原GetAuthorized观察检查，失败不阻止继续调用Service.Signal。观察验证和最终Signal验证不合并。
- [x] 新`GatewayError{Kind GatewayFailureKind; Cause error}`实现Unwrap，Kind为LookupUnavailable、RenderRejected、Unauthorized、FileNotFound、AnswerConflict、SignalConflict。按来源包装，不按错误文字或底层哨兵全局猜HTTP码。HTTP映射依次503、422、403、404、409、409；未知未分类错误按原入口兜底：Boot/Installer503、File403、Signal409，正文仍用固定状态文字，不泄露Cause。
- [x] HTTP保留方法/路径/query/数字/digest语法、保守UA分流、4096字节正文上限、400/404/405/413、缓存及内容头、输出；不再接触Task/Snapshot/Execution、选择凭据scope或取得执行port。应用不依赖http/Gin。
- [x] 新`Test机器入口错误来源与状态保持`核对file初次读库错误403、Preseed读错503、最终Answer失败409；`Test机器应答渲染后取消不泄出正文`固定取消胜出；`Test机器入口保留观察时机与双重验证`固定File失败前已观察、Installer失败不观察、Signal观察验证失败仍处理及观察nil不额外查库。保留原凭据取得顺序和时机，不缓存成另一套流程。
- [x] VM运行`go test -count=1 ./internal/application/execution ./internal/interfaces/machine ./cmd/lober ./internal/archtest`及`pnpm --dir web check:console-types`。原`TestCanonicalFrozenFileRouting`、`TestBoundedBinarySignalAndStrictQuery`、`TestBootServesPreseedAnswerToDebianInstallerAgent`、`TestLinuxMachineDeliversFrozenFilesAndCompletesProtocol`等继续通过。
- [x] 更新组装普通转换正例为execution/port.RenderRequest，不放宽Token/ObserveIP/策略字段读取许可。独立审查通过后提交`refactor(machine): delegate delivery workflows to application gateway`。

## Task 5：日志收录流程归应用，基础设施只管租约和字节（I1第三部分）

**Files:** 创建`internal/application/execution/{installer_logs,installer_logs_test}.go`及`execution/port/{installer_log,repository}.go`；修改`internal/infrastructure/events/{syslog,syslog_test}.go`、`cmd/lober/{machine,machine_delivery_test}.go`、`execution/observation_test.go`及精确能力守卫。更新机器契约与能力清单。

**Interfaces:** `port.InstallerLog{TaskID string; Attempt int; SourceIP string; At int64; Body []byte}`；Body为已截断、脱敏的字节。`port.InstallerLogReceiver.ReceiveInstallerLog(context.Context,InstallerLog) error`。新`port.InstallerLogRepository`只含`GetTask(context.Context,string)(model.Task,error)`和`AppendEvents(context.Context,string,[]repository.NewEvent)([]repository.Appended,error)`；`NewInstallerLogs(port.InstallerLogRepository,Notifier) port.InstallerLogReceiver`，Notifier复用本执行包现有的仅Publish小接口。Collector改为`events.New(port.InstallerLogReceiver,func()int64) *Collector`，ObserveIP签名不变。新能力只供可信日志适配器调用，不能接受网页伪造身份。

- [x] 先把现events混合测试分成Collector假接收器测试与应用假仓储测试，保留真实UDP/数据库接线；新职责反例应在旧Collector仍访问Repository/Notifier时红。
- [x] Collector保留IP解析/Unmap、4096地址上限、已有地址覆盖、到期等号、互斥、直接连接地址、不信Forwarded、先截前65536再SafeLogTail。有效租约才调用接收能力，At取原位置一次时钟，不把读取业务事实留在Collector。
- [x] 应用GetTask→域CanReceiveInstallerLog→AppendEvents→成功Publish。GetTask的ErrNotFound无声忽略，其他读错透传；AppendEvents任何错误都返回且不通知。事件仍`installer.syslog`、空Key、原source_ip和log_base64；无审计、不重取时钟、不改Task/Rev。接收器同步编码、不保留可变输入切片；byte处理虽移到调用前，不能引入新外部副作用。
- [x] 明确保留当前“读时判断后再追加”的窗口：读取后发生取消/重试不新增原子尝试校验，也不声称修复此窗口。重复包仍逐包写入。若另要增强这项保证，须另立契约和回归，不混进迁移。
- [x] 新`Test日志收录按事实判断并在成功后通知`覆盖终态/nilExecution/旧attempt、读错、保存错、取消和成功一次；`Test日志传输先截断再脱敏且保持捕获时间`覆盖65537字节、二进制、租约到期和mapped IPv6。ctx原样传递。
- [x] cmd按Service→InstallerLogs→Collector→Gateway→HTTP接线；Gateway只取观察口，Collector只取日志接收口，不形成构造或导入环。VM运行`go test -count=1 ./internal/application/execution ./internal/infrastructure/events ./internal/interfaces/machine ./cmd/lober ./internal/archtest`，保留真实`TestLinuxMachineDeliversFrozenFilesAndCompletesProtocol`。
- [x] 独立审查通过后提交`refactor(events): move installer log acceptance into application`。I1须等Task7保护与Task8全链复核后才最终关闭。

## Task 6：模板候选使用一份领域规则（M3）

**Files:** 创建`internal/domain/selection/{template,template_test}.go`；修改`internal/domain/repository/{types,templates}.go`、`internal/application/tasks/{port/repository,wizard,wizard_test}.go`、`internal/infrastructure/persistence/pgstore/{template_selection,templates,hooks,pagination_test}.go`；新增`template_selection_contract_test.go`；更新`docs/contracts/backend-modules.md`、能力清单及精确domain依赖检查。核对`console/harness_test.go`、`cmd/lober/app.go`调用方，不扩大authoring的消费接口。

**Interfaces:** 新`selection.TemplateEligible(model.Template,map[model.HookKey]model.Hook,model.ISO,selection.Manifest) bool`在selection包内的最后参数写`Manifest`；内部只组合现有CurrentBuiltin与TemplateUsable。两处调用同一函数，不把activation.Template的问题加入资格。

新仓储值`TemplateCandidateFacts{Template model.Template; Hooks map[model.HookKey]model.Hook}`；在`repository.Templates`及tasks/port.Templates同时声明`ListTemplateCandidates(context.Context,repository.TemplateQuery) (repository.Page[repository.TemplateCandidateFacts],error)`（repository包内省略自身限定）。Selection必填，否则ErrInvalid；其他字段和错误先后沿用原查询。原ListTemplates(Selection)保留并委托同一扫描器剥离facts，不新增第二套规则。

- [x] 新领域表测覆盖草稿/审批/废弃、系、nil/空/有值offers、旧/当前内置、钉住版本和缺失引用的既有语义；不改输入。先在VM固定旧输出；新增`Test模板候选资格只由领域实现`在旧资格SQL仍存在时应红。
- [x] 编号向导改用新facts能力，合并页内精确Hooks为同一只读map，再按原TemplateChoice形态生成Problems，不再额外GetHooks；仓储每项facts仍只保留自身引用。游标仍ListTemplates→GetHooks→TemplateEligible，保持先分候选页再筛选、空items仍可能next非空。权限→ISO→分页的错误优先级不变。
- [x] 新查询在私有只读REPEATABLE READ事务中按原`name_key COLLATE "C", id COLLATE "C"`、最高审批版本和现过滤条件，使用keyset每批256模板；精确HookRefs去重后每批1000调用传tx的私有hook读取。所有批共用tx，不调用会重新`s.read`的公共GetHooks，不改Store.write。
- [x] 资格只调用TemplateEligible，删除templateSelectionQuery及无其他用途的allowedVersionPairs/parameterElements。保留通用当前版本选取、参数校验及排序SQL；不引入规则语言。编号模式扫完合格总数，只保留请求页；直接仓储Selection游标只需limit+1合格项，仍资格先于分页。偏移复用原校验，比较窗口避免offset+limit溢出。
- [x] 内存只保留当前256项、目标页最多500项及这两部分精确引用，不能积累全库正文/全体合格键或把整批Hook map挂到每页结果。代价是读取全部候选及其引用才能计数，不声称更快、不设未经实测的秒数门槛。Items为空仍非nil，Total在页码时含0，Next与原规则一致。
- [x] 契约明写：这是单次页码查询内部事实一致性；ISO/Manifest仍为应用先取得的输入，事务不持写锁，不承诺跨请求/跨页、展示后可提交或与输入ISO同刻。旧页码二次GetHooks断言改为新facts能力，不删除相应错误覆盖；游标的二次读取断言不变。
- [x] 真实库扩展`TestNumberedTemplateEligibilityBeforePagination`，参考候选必须遍历全部页，并直接调用TemplateEligible，不再手写builtinOK。新`Test模板候选跨批精确引用与分页一致`至少两组：260模板，前256合格、后4中3不合格，合格总数257；每页20时末页17项、越界空页仍Total=257。另一组在同一模板批内含1001以上独立引用，以现TemplateHookKeys的排序选第1001个引用作为拒绝事实，验证第二组件批不能漏读。比较新facts与旧Selection接口的ID、Version、顺序、Next、Total；最高审批不合格不得回退旧审批，不为测试改生产批大小。
- [x] 新`Test模板候选读取期间变更仍使用同次事实`用两个连接和测试专用查询回调屏障，在首批后审批/改名/废弃，当前查询保持起始快照、下一查询见更新；不以sleep推断。新`Test模板候选取消不返回半页或半总数`验证后批取消、事务结束和连接可复用。回调只在测试注册、清理，不新增生产故障注入接口。
- [x] VM运行`go test -count=1 ./internal/domain/selection ./internal/application/tasks ./internal/infrastructure/persistence/pgstore ./internal/interfaces/console ./internal/archtest`。核对所有页码替身真实实现新方法，不靠nil嵌入接口运行；保留普通模板、游标、参数、提交及冻结任务的回归。独立审查通过后提交`refactor(selection): share template eligibility across pagination modes`。

## Task 7：全文件与精确能力约束，关闭自动检查漏口（I2）

**Files:** 修改`internal/archtest/{deps_test,application_rules_test,console_rules_test,console_test,machine_rules_test,execution_guard_test,transition_uses_test,wizard_helpers_test,wizard_rules_test,hosts_rules_test,authoring_rules_test,media_rules_test,identity_audit_rules_test,task_finalize_rules_test}.go`；新建`interface_capabilities_test.go`、`final_closure_rules_test.go`。不改任何生产行为或历史清单内容。

**Interfaces:** 新`interfaceCapabilityFindings(caller string,fset *token.FileSet,files []*ast.File,info *types.Info) []violation`接入原typedTransitionFindings；`consoleTransportMethod(*types.Func) bool`识别真正传输能力；`applicationExternalImport(string) bool`供所有已登记应用及其port使用。继续使用原checkModule、真实类型装载和violation，不另造只扫固定文件的检查路径。

- [x] 把审计GetHost反例转为正式`Test接口全文件与自建能力不得绕过`，先真实types检查成功，再断言违规类别与位置。扩展新文件/改名、未调用声明、匿名与嵌入接口、Lookup改名、方法值、包级函数值、函数字段、两层helper、routes/init接线；在旧守卫上先跑出准确漏拦，不接受编译失败代替拒绝。
- [x] wizardHelperFindings从全部console生产AST的函数/方法/变量/初始化出发，另外检查全部类型和能力声明；停止用migratedConsoleFile当门槛。按实际对象与方法集检查，自建业务能力不得因符号改名获准；真正已登记应用入口可用。普通DTO转换不是仓储能力。
- [x] 拆分execution、machine、events各自角色规则：machine不得仓储、应用port或领域选择/摘要/状态规则；events仅精确接收/观察port和传输库，无Repository/Notifier业务组合；execution应用不得文件、网络、数据库、自行取当前时间或随机数，保留注入密钥的纯HMAC机制。新增域执行只能model/catalog与纯标准库。
- [x] 所有应用模块及port统一拒绝os/os/exec、net/net/http、database、syscall、io/ioutil、crypto/rand、math/rand等外部操作；保留identity/audit已有更严格规则和media io/fs精确常量，不整包开口。执行模块虽原走特殊layer，也必须受此限制。作者/主机不能成为漏检例外。
- [x] 接口精确允许context、标准错误解包、HTTP服务器输出、Reader/Writer/Closer/Flusher和内置界面资源；不凭Read/Flush文字或整个net/http放行本地能力、HTTP客户端取业务事实。新增`Test接口传输输出可用但客户端取事实被拒绝`。请求格式的model.ValidID/IsSafeWord等精确值校验可留，不放行状态/准入/摘要判定。
- [x] 正例固定新文件纯DTO、真正应用调用、标准Write/Flush、5秒SSE补读/取消/退订、机器薄委托和普通组装字段转换；不得为拦仓储而封死interface、context、HTTP或合法时间类型。重构wizardTransitionFixture按用例注入所需声明，去除无关旧违规对正例的污染，不能保留旧名单或删正例。
- [x] 新`Test机器接口不得读取仓储`、`Test所有应用外部操作都受检查`、`Test执行与日志职责不能回到适配器`覆盖实际别名/函数值。原全模块发现、未知包、构建约束、缓存反例、历史清单和六项编号能力全部保留；不把old machine/store合法导入例子继续当正例。
- [x] VM运行`go test -count=1 -v ./internal/archtest`并执行`go test -count=1 -run '^Test全模块依赖守卫$' ./internal/archtest`。审计独立反例的相同生产输入此时应被明确拒绝；原断言“应拒绝”的测试转PASS。反例既要夹具测试，也要接入实际全模块路径，不能只修一个局部检查函数。
- [x] 独立审查通过后提交`test(arch): enforce final boundaries across files and capabilities`；I2只能在全部模块、machine和实际生产树均受查后关闭。

## Task 8：全链契约复核、VM完整验收与整分支独立审查

**Files:** 创建`docs/superpowers/reviews/2026-10-25-ddd-final-closure-evidence.md`、`2026-10-25-ddd-final-closure-review.md`；更新本文状态、总路线及能力清单，保留原审计与历史数字，不把原问题提前改成已修。VM/原始日志和连接证据仍在忽略目录。

**Interfaces:** 不新增业务能力。以任务1清单加3–6新增项逐项核八项约定、调用方、实现、测试和错误分类；按I1/I2/I3/M1/M2/M3分别给关闭证据，不能用一张“全通过”代替。

- [x] 根固定最终功能提交和完整树；在新归属VM目录检出该提交，不覆盖旧目录、不叠加dev未提交UI。实际探测lober、资源/工具、无竞争Go作业、真实lober_test非恢复及DSN0600，禁止输出连接内容。
- [x] 取运行前完整提交/物理文件清单、Go实际选入的源码/测试/嵌入输入及generated.ts摘要；清测试结果缓存后依次运行原命令：

```sh
go clean -testcache
make check
pnpm --dir web check:console-types
go test -count=1 -v ./...
git diff --check
git diff --exit-code
git diff --cached --exit-code
```

- [x] 取运行后同源证明，核对HEAD、HEAD/索引树、全部文件字节、实际输入、无忽略活动源码和生成类型。记录实际包数、顶层/子项、缓存、明确Skip及每个重点新旧测试的真实PASS；不复制审计的37包/1165等旧数。普通/竞态隐式Skip未知仍如实写未知；真实数据库用例不能跳过后称通过。
- [x] 两个审计反例正式回归都须转PASS：守卫拒绝同一非法输入，镜像保留同一取消错误/状态。真实ISO两个opt-in和一次性记录用例如仍未启用，逐项说明，不宣称真实介质、实机、企业或完整前端验收。
- [x] 另派未参与最终任务的整分支审查者，重核四层职责、所有模块八项契约、Task6单次一致性代价、I1凭据/观察/错误时序、I2正反例与实际VM证据。审查列明严重/重要/轻微、不能验证或不评判事项，根逐条裁定。严重/重要未关闭不能交付为全部落地。
- [x] 失败时保留首轮日志，先在同一VM、同一代码与设置复跑失败用例定位，再以原完整命令验收；不改超时/密码/缓存/断言绕过。真实缺陷回原所属任务修正并复审，必要时使后续任务依次重验。
- [x] 文档收尾后精确核对功能提交到文档提交只含授权文档，生产、测试和全部活动输入不变；否则回VM补相应检查。独立文档核对通过后提交`docs: record final architecture and contract closure`。

## 3. 交付、回退与验收证据

每项证据包括输入契约、具体提交、VM源码身份、完整命令和退出码、实际断言、独立审查和修复轮次。Task2错误修复、Task3纯迁移、Task4/5流程迁移、Task6查询实现及Task7守卫分别提交；不把它们混成一个不可定位的变更。

本阶段交付`codex/ddd-final-closure`。Task6读放大/一致性或I1协议兼容若不满足验收，回对应任务处理，不退回旧SQL双规则或永久开放旁路再宣布完成。回退按依赖逆序保留证据，不reset用户工作区，不回退已验收的六阶段。后续本地dev集成及合并后VM完整验收另按用户授权执行。

最终“全部落地”必须同时满足：六个问题各有关闭依据、八项任务审查通过、原完整VM命令通过、所有新能力契约完整、无生产层旁路/重复业务判定/新临时许可。仅目录齐全、原套件绿或过渡表为零都不单独构成完成。
