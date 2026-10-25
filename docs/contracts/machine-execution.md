# 机器执行与日志约定

本文连接执行模块方法旁的约定，固定当前行为。它不增加装机格式、可靠消息、持久会话或跨实例立即撤权。方法与测试的导航见[能力清单](application-capabilities.md)；清单不另定规则。

## 当前职责与共同保存边界

可信机器协议适配器调用 execution.Service；用户控制经 tasks.Controller 与 AuditedControl 调用。仓储不认证机器令牌，执行应用负责凭据检查。Boot/Preseed 是现有按主机查找的特例；并非每个入口都先收机器令牌。公开网页权限不套在机器协议上。

输入使用已有 Go 类型，不额外裁剪编号、改大小写、限制大小或把无效值替成示例；各方法明确列出的规范化与检查除外。Signal 及其纯规则仍在 `application/execution/protocol.go`，机器文件选择和渲染调用仍在 `interfaces/machine/http.go`。日志归属及事件保存仍在 `infrastructure/events/syslog.go`。这些是当前职责位置，尚未达到后文目标。

每次 commit 通过 repository.Tasks.UpdateTaskExecution 同事务保存任务状态、Execution、事件及可选审计。它按最新行的 State/Rev 比期望值；冲突不覆盖并发结果，应用是否重读重试依各方法。仓储只复查版本及存储约束，不在锁内再次校验机器令牌或重跑整个纯状态机；需要新尝试或状态复查时由应用循环完成。

非零审计在同一事务写入；Boot、Answer、Signal、Sweep 不写用户审计。仓储成功返回后，执行服务才 Publish 一次提示；保存失败不发。通知可合并，消费者重读持久化事件，不承诺每条通知必达。没有独立写审计入口，也没有跨文件渲染与数据库的共同事务。

原上下文传给仓储；没有新增计时器、提交后取消检查或自动回滚已提交结果。纯内存计算、Token与诊断不接收取消。服务方法共享只读密钥，调用方不能在执行中修改传入任务、Execution或Signal。不同调用可能读取新事实，不承诺多次读取或跨页快照。

## 构造、凭据与授权

| 方法 | 目的、输入、结果及错误顺序 | 重复、并发与副作用 |
| --- | --- | --- |
| New | 可信组装传Repository、密钥、秒时钟和可空Notifier。密钥不足32字节或nil时钟返回配置错误；不预查Repository是否nil。成功复制密钥并返回服务。 | 不访问存储或时钟，无审计/通知/事务/提交复查/取消；重复构造独立服务。依赖并发安全由注入实现保证。 |
| Token | 可信调用者传冻结任务与Install/Finalize范围；无Execution或未知范围返回空串。用任务ID、Attempt、IssuedAt及范围计算64位小写十六进制签名。 | 不校验当前状态或有效期、不查库或时钟，无错误；同任务与密钥结果相同。只读，无事务、冲突、提交复查、审计、通知或取消。 |
| Credentials | 原任务，先Install后Finalize返回两项Token；无Execution时两项为空。 | 完整约定保留在service.go方法旁；不验证调用者凭据或状态。 |
| Authorize | 先检查Execution、令牌长度64、范围；然后时钟早于IssuedAt或有效期已到、签名不符均ErrCredential；Install再检查Deadline与booting/installing状态。Install寿命5400秒，Finalize寿命86400秒，边界即失效。 | 只查传入快照与当前时钟，无仓储、写入、审计或通知；重复可能因时钟变化失效，无版本重试和提交复查，不接收上下文。 |
| GetAuthorized | 先GetTask，再按finalize布尔值选范围调用Authorize；不存在/查询错误先于凭据错误。成功返回读取的原任务。 | 只读，无事务、审计/通知或提交复查；重复可能看到新状态，原上下文只交仓储。 |

不承诺已发令牌跨尝试有效。Retry 创建新Execution、递增Attempt，原尝试令牌不能通过随后读取新任务的授权。Finalize的授权不额外套Install阶段状态限制；Signal还有下文独立规则，不能用GetAuthorized完全替代。

## 引导和应答

| 方法 | 输入、检查次序与成功结果 | 写入、重复和并发 |
| --- | --- | --- |
| Boot | 原SN、架构、固件；先NormalizeSN，非法SN返回零任务/false/nil。FindOpenTask错误原样返回，无任务直接返回。只有pending/booting继续，其他状态返回不引导。纯规则检查架构/固件及状态。 | 首次建立执行记录，允许最多3次引导，平台不兼容、次数已满或booting截止到期可提交failed，返回found=false。成功提交machine.boot事件。遇保存冲突直接返回零任务/false/nil，不重试；源代码外层8轮不能被描述为8次重试。 |
| Preseed | NormalizeSN后找开放任务；非法SN、无任务或非booting/installing均不返回安装任务。成功返回原任务与true。 | 只读，无事务、审计、通知、提交复查或冲突重试；它本身不验证格式、令牌或推进状态，Debian格式检查及Answer由协议调用方负责。 |
| Answer | 每轮GetAuthorized(Install)，已installing直接返回；否则用时钟进入installing/packages阶段。 | 同事务保存machine.installing事件；版本冲突最多8轮重读并重新授权，耗尽ErrConflict。相同已installing请求不再提交，不额外通知。渲染不在此方法内。 |

引导阶段初始预算900秒，packages预算3600秒；pre/post等安装阶段不超过IssuedAt+5400。任务冻结快照不重新选择最新模板或镜像。状态机的其他既有规则、阶段预算和原因以protocol.go当前实现为准，后续迁移不得改参数。

## 信号与超时扫描

Signal 输入为任务ID、令牌与完整Signal。先读任务，再按TimingFirstboot或booted/init-start/init-done/ready选择Finalize，其余Install。先校验原凭据格式、寿命、签名，再拒绝Install向已结束任务上报，之后执行纯推进规则。这里不会先使用Authorize的Install阶段筛选；重复信号判定及重启后旧安装信号拒绝仍由advance负责。

advance先拒绝缺Execution、Body超过4096字节、时钟早于IssuedAt；再用phase/slot/instance查重。同键同完整指纹返回重复成功，不写事件；同键不同内容ErrProtocol。重启后旧安装插槽信号仍拒绝。之后检查终态、最多16384信号、截止时间、插槽/实例/摘要/先后顺序。不能重排这几步，否则会改变迟到重复信号结果。

成功推进生成一条事件，顺序由仓储分配Seq，Key带Attempt。事件Kind保留machine阶段及slot/hook的现有映射；日志先由SafeLogTail遮掉机器应答/信号路径中的凭据，再保留最多4096字节尾部并以base64保存，二进制内容不改成普通文本。State/Execution/事件共同提交，冲突最多8轮重新读任务与凭据，耗尽ErrConflict；非冲突错误原样返回。系统信号不写用户审计，成功提交才提示。

Sweep 仅供可信后台循环调用，无业务输入；每次按原仓储顺序、游标每页100读取booting/installing/rebooting/initializing。每项读取一次时钟，仅有Execution、非零Deadline且已到期才把状态/Execution与machine.timeout一起提交。不写用户审计；成功才提示，冲突跳过该项，其他错误结束。重复扫描读持久事实，不缓存待办，不保证跨页快照。循环的15秒计时与异常保护仍在cmd/lober，Sweep不启动后台循环或新超时。

## 控制与诊断

CanCancel、CanRetry 供可信任务应用读取诊断，输入为原任务，不规范化或新增大小限制，无error返回。CanCancel 非终态返回nil，否则task_control_state。CanRetry先检查ReachedReboot并返回task_retry_after_reboot，再检查failed/cancelled；可重试返回nil，其他状态task_control_state。问题保留原顺序；网页转换nil为空数组。只读纯值，无仓储、事务、审计、通知、提交复查、取消或超时；重复同值相同，不是提交成功保证。

Service.Cancel/Retry 先CheckAudit，再GetTask，存在性先于版本比较；最新Rev/State不等于传入任务报ErrConflict。随后取对应诊断，有问题报ProblemsError(ErrRejected)，再用注入时钟构建下一状态与事件。取消清Deadline；重试递增Attempt、重建执行记录并改pending。若原无Execution先建立当前执行记录，沿用原尝试计算，不改计数规则。

状态/执行记录/事件/用户审计共同提交，锁内再次比较State/Rev和开放任务约束。取消事件machine.cancelled，重试machine.retry；提交成功执行服务才提示。没有自动版本刷新或重试；重复旧输入冲突，重新读取后仍按当前状态和诊断判断。原上下文传给仓储，不新增提交后取消检查，不撤销已提交结果。

ControlBackend 四方法唯一继承上述Service方法；AuditedControl 的Cancel/Retry只转换四项AuditIntent字段，任务、上下文和错误原样传递。CanCancel/CanRetry原样返回诊断，无新授权或副作用。NewAuditedControl只包装注入后端，不检查nil、不查库或时钟、不失败；无事务、重试、提交复查、审计、通知或取消。任务网页补读与二次提示仍由[任务控制约定](task-read-control-events.md#写入审计与通知)负责。

## 地址观察与日志现状

NewIPObserver 与 IPObservationSink.ObserveIP 保留各自方法旁的完整约定：只有原凭据成功的观察位置才调用，按原Attempt及IssuedAt+5400/86400登记原RemoteAddr；无Execution不登记。地址处理、有效期与容量由Collector决定，不读转发头。普通文件请求在凭据检查后、选择文件和渲染前观察；信号在最终Signal前尝试GetAuthorized，成功就观察，观察成功不保证随后信号推进成功。Debian应答在Answer成功后观察。普通Boot菜单不增加观察。

当前Collector.ObserveIP只在内存登记：解析host:port并把IPv4映射地址还原，非法任务/尝试/地址、已过期忽略。新地址最多4096项，重复地址覆盖；锁保护登记，过期项清除。无错误、事件、审计、通知、事务或提交复查，不接收上下文，重启丢失归属。

当前Collector.Receive先解析来源地址，无有效且未到期归属则成功忽略；再GetTask。任务不存在忽略，其他读取错误原样返回；终态、无Execution或Attempt不符忽略。接收正文先截到65536字节，再用SafeLogTail先遮掉机器路径凭据、后保留最多4096字节尾部；事件installer.syslog只含source_ip与log_base64。AppendEvents成功后才通知，失败不通知；它不推进任务、不记用户审计、不保证重复日志去重。

读取归属与任务、追加事件是分步操作，当前不承诺三者共同事务或锁内再次核对Attempt。Receive上下文传给仓储，UDP传输不承诺必达。监听、ReadFrom缓冲及错误日志仍在cmd/lober；这些现状不能写成已有执行应用日志入口。

## 机器传输顺序现状

机器HTTP入口保留text/plain、no-store和nosniff。/v1/boot先解析已知单值查询，再校验/转换架构与固件，才按User-Agent分普通菜单与Debian安装器；无安装任务返回现有本地盘脚本。Boot错误503，渲染错误422。Debian路径先Preseed与格式，再渲染，再Answer，Answer失败409，成功才观察并写正文。

文件路径先做路由形状、ID、安全词、方法和查询检查；GetAuthorized失败403。凭据成功后观察，然后选择真实应答/辅助文件/脚本/组件；未知文件404，组件正文摘要不符422，渲染失败422。主应答在渲染后重新Answer，失败409，之后才写正文。取消与渲染可能已分别完成，不存在跨渲染事务。

信号先解析查询、限制正文4096（超限413、其他读取失败400），再尝试凭据观察，最后Signal。Signal错误统一409，成功200空正文。协议传输错误与应用ErrProtocol/ErrCredential的映射保持，不能把内部错误文本直接回传。

## 尚未实现的目标入口

下面只记录Task3–5的职责目标，不是现有可调用方法，不虚构方法名或签名。实现后由所属任务补本文件及能力清单，最终是否完整由Task8核对。

| 所属任务 | 目标能力 | 必须保留的当前约定 |
| --- | --- | --- |
| Task3 | 把纯执行推进和控制诊断移入领域，应用保留读、凭据、保存编排 | Signal查重与凭据时序、阶段预算、错误类别、State/Rev竞争、通知位置；不新增装机能力。 |
| Task4 | 由执行应用承接机器引导、文件请求与信号编排的普通输入输出入口 | 观察位置、组件摘要、渲染后Answer、原凭据范围及每条传输错误映射；接口只剩传输。 |
| Task5 | 执行应用承接日志的任务规则、AppendEvents和成功后Publish；Collector继续管理IP租约/地址归属，并先截断再SafeLogTail | 归属上限/有效期/重启丢失、忽略次序、原日志字节、错误和通知位置；租约与字节处理保留在外部实现。 |

镜像I3的当前三处分支与预计保留取消/截止类型的纠正在[镜像第11节](media.md#11-已知取消类型缺陷与修复目标)；Task2尚未在本说明任务实现。Task6只细化[单次模板候选读取](backend-modules.md#单次模板候选读取的后续细化)，不承诺跨页一致，也不改变写事务。

## 当前验证位置

执行服务与纯规则：`internal/application/execution/{service,protocol,control_intent,observation}_test.go`。机器传输与文件顺序：`internal/interfaces/machine/http_test.go`。日志归属与裁剪：`internal/infrastructure/events/syslog_test.go`。组装：`cmd/lober/{machine,assembly}_test.go`。事务与竞争：`internal/infrastructure/persistence/pgstore/{contract_tasks,contract_events,contract_concurrency}_test.go`及执行记录相关测试。这里只登记已有验证来源，不表示本次说明修改已重跑测试或关闭后续缺口。
