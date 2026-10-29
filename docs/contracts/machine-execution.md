# 机器执行与日志约定

本文连接执行模块方法旁的约定，固定当前行为。它不增加装机格式、可靠消息、持久会话或跨实例立即撤权。方法与测试的导航见[能力清单](application-capabilities.md)；清单不另定规则。

## 当前职责与共同保存边界

可信机器协议适配器调用 execution.Gateway；Gateway 在本模块调用 execution.Service；用户控制经 tasks.Controller 与 AuditedControl 调用。仓储不认证机器令牌，执行应用负责凭据检查。Boot/Preseed 是现有按主机查找的特例；并非每个入口都先收机器令牌。公开网页权限不套在机器协议上。

输入使用已有 Go 类型，不额外裁剪编号、改大小写、限制大小或把无效值替成示例；各方法明确列出的规范化与检查除外。Signal、执行推进和控制纯规则已归 `domain/execution`，应用保留 Signal 类型别名及同一个 ErrProtocol 哨兵。冻结文件选择由应用 Gateway 调用领域入口；HTTP只解析请求及输出，cmd以普通字段转换连接冻结渲染端口。日志收录由执行应用 `installer_logs.go` 读取事实、调用领域资格规则、追加事件并在成功后通知；`infrastructure/events/syslog.go` 只管地址租约与字节。

每次 commit 通过 repository.Tasks.UpdateTaskExecution 同事务保存任务状态、Execution、事件及可选审计。它按最新行的 State/Rev 比期望值；冲突不覆盖并发结果，应用是否重读重试依各方法。仓储只复查版本及存储约束，不在锁内再次校验机器令牌或重跑整个纯状态机；需要新尝试或状态复查时由应用循环完成。

非零审计在同一事务写入；Boot、Answer、Signal、Sweep 不写用户审计。仓储成功返回后，执行服务才 Publish 一次提示；保存失败不发。通知可合并，消费者重读持久化事件，不承诺每条通知必达。没有独立写审计入口，也没有跨文件渲染与数据库的共同事务。

原上下文传给仓储；没有新增计时器、提交后取消检查或自动回滚已提交结果。纯内存计算、Token与诊断不接收取消。服务方法共享只读密钥，调用方不能在执行中修改传入任务、Execution或Signal。不同调用可能读取新事实，不承诺多次读取或跨页快照。

## 构造、凭据与授权

| 方法 | 目的、输入、结果及错误顺序 | 重复、并发与副作用 |
| --- | --- | --- |
| New | 可信组装传Repository、密钥、秒时钟和可空Notifier。密钥不足32字节或nil时钟返回配置错误；不预查Repository是否nil。成功复制密钥并返回服务。 | 不访问存储或时钟，无审计/通知/事务/提交复查/取消；重复构造独立服务。依赖并发安全由注入实现保证。 |
| Token | 可信调用者传冻结任务与Install/Finalize范围；无Execution或未知范围返回空串。用任务ID、Attempt、IssuedAt及范围计算64位小写十六进制签名。 | 不校验当前状态或有效期、不查库或时钟，无错误；同任务与密钥结果相同。只读，无事务、冲突、提交复查、审计、通知或取消。 |
| Credentials | 原任务，先Install后Finalize返回两项Token；无Execution时两项为空。 | 完整约定保留在service.go方法旁；不验证调用者凭据或状态。 |
| Authorize | 先检查Execution、令牌长度64、范围；然后时钟早于IssuedAt或有效期已到、签名不符均ErrCredential；Install再调用InstallAccessNeedsTime决定是否读原额外时钟，并交InstallAccessAllowed检查Deadline及状态。否则传不使用的0；nil执行记录不得授权。Install寿命5400秒，Finalize寿命86400秒，边界即失效。 | 只查传入快照与当前时钟，无仓储、写入、审计或通知；重复可能因时钟变化失效，无版本重试和提交复查，不接收上下文。 |
| GetAuthorized | 先GetTask，再按finalize布尔值选范围调用Authorize；不存在/查询错误先于凭据错误。成功返回读取的原任务。 | 只读，无事务、审计/通知或提交复查；重复可能看到新状态，原上下文只交仓储。 |

不承诺已发令牌跨尝试有效。Retry 创建新Execution、递增Attempt，原尝试令牌不能通过随后读取新任务的授权。Finalize的授权不额外套Install阶段状态限制；Signal还有下文独立规则，不能用GetAuthorized完全替代。

## 引导和应答

| 方法 | 输入、检查次序与成功结果 | 写入、重复和并发 |
| --- | --- | --- |
| Boot | 原SN、架构、固件；先NormalizeSN，非法SN返回零任务/false/nil。FindOpenTask错误原样返回，无任务直接返回。只有pending/booting继续，其他状态返回不引导。纯规则检查架构/固件及状态。 | 首次建立执行记录，允许最多3次引导，平台不兼容、次数已满或booting截止到期可提交failed，返回found=false。成功提交machine.boot事件。遇保存冲突直接返回零任务/false/nil，不重试；源代码外层8轮不能被描述为8次重试。 |
| Preseed | NormalizeSN后找开放任务；非法SN、无任务或非booting/installing均不返回安装任务。成功返回原任务与true。 | 只读，无事务、审计、通知、提交复查或冲突重试；它本身不验证格式、令牌或推进状态，Debian格式检查及Answer由协议调用方负责。 |
| Answer | 每轮GetAuthorized(Install)，InstallingNeeded为假直接返回，不读推进时钟；否则读时钟交StartInstalling进入installing/packages阶段。 | 同事务保存machine.installing事件；版本冲突最多8轮重读并重新授权，耗尽ErrConflict。相同已installing请求不再提交，不额外通知。渲染不在此方法内。 |

引导阶段初始预算900秒，packages预算3600秒；pre/post等安装阶段不超过IssuedAt+5400。任务冻结快照不重新选择最新模板或镜像。状态机的其他既有规则、阶段预算和原因以domain/execution/protocol.go当前实现为准，后续迁移不得改参数。

## 信号与超时扫描

Signal 输入为任务ID、令牌与完整Signal。先读任务，再按FinalSignal选择Finalize或Install。先校验原凭据格式、寿命、签名，再调用SignalAccessAllowed拒绝Install向已结束任务上报，之后执行Advance纯推进规则。这里不会先使用Authorize的Install阶段筛选；重复信号判定及重启后旧安装信号拒绝仍由Advance负责。

Advance先拒绝缺Execution、Body超过4096字节、时钟早于IssuedAt；再用phase/slot/instance查重。同键同完整指纹返回重复成功，不写事件；同键不同内容ErrProtocol。重启后旧安装插槽信号仍拒绝。之后检查终态、最多16384信号、截止时间、插槽/实例/摘要/先后顺序。不能重排这几步，否则会改变迟到重复信号结果。

成功推进生成一条事件，顺序由仓储分配Seq，Key带Attempt。事件Kind保留machine阶段及slot/hook的现有映射；日志先由SafeLogTail遮掉机器应答/信号路径中的凭据，再保留最多4096字节尾部并以base64保存，二进制内容不改成普通文本。State/Execution/事件共同提交，冲突最多8轮重新读任务与凭据，耗尽ErrConflict；非冲突错误原样返回。系统信号不写用户审计，成功提交才提示。

Sweep 仅供可信后台循环调用，无业务输入；每次按原仓储顺序、游标每页100读取booting/installing/rebooting/initializing。每项读取一次时钟，仅有Execution、非零Deadline且已到期才把状态/Execution与machine.timeout一起提交。不写用户审计；成功才提示，冲突跳过该项，其他错误结束。重复扫描读持久事实，不缓存待办，不保证跨页快照。循环的15秒计时与异常保护仍在cmd/lober，Sweep不启动后台循环或新超时。

## 控制与诊断

CanCancel、CanRetry 供可信任务应用读取诊断，输入为原任务，不规范化或新增大小限制，无error返回。CanCancel 非终态返回nil，否则task_control_state。CanRetry先检查ReachedReboot并返回task_retry_after_reboot，再检查failed/cancelled；可重试返回nil，其他状态task_control_state。问题保留原顺序；网页转换nil为空数组。只读纯值，无仓储、事务、审计、通知、提交复查、取消或超时；重复同值相同，不是提交成功保证。

Service.Cancel/Retry 先CheckAudit，再GetTask，存在性先于版本比较；最新Rev/State不等于传入任务报ErrConflict。随后取对应诊断，有问题报ProblemsError(ErrRejected)，再用注入时钟构建下一状态与事件。取消清Deadline；重试递增Attempt、重建执行记录并改pending。若原无Execution先建立当前执行记录，沿用原尝试计算，不改计数规则。

状态/执行记录/事件/用户审计共同提交，锁内再次比较State/Rev和开放任务约束。取消事件machine.cancelled，重试machine.retry；提交成功执行服务才提示。没有自动版本刷新或重试；重复旧输入冲突，重新读取后仍按当前状态和诊断判断。原上下文传给仓储，不新增提交后取消检查，不撤销已提交结果。

ControlBackend 四方法唯一继承上述Service方法；AuditedControl 的Cancel/Retry只转换四项AuditIntent字段，任务、上下文和错误原样传递。CanCancel/CanRetry原样返回诊断，无新授权或副作用。NewAuditedControl只包装注入后端，不检查nil、不查库或时钟、不失败；无事务、重试、提交复查、审计、通知或取消。任务网页补读与二次提示仍由[任务控制约定](task-read-control-events.md#写入审计与通知)负责。

## 领域纯规则

本节是下表每个领域方法的唯一八项约定来源，方法旁只连接本节。①仅供可信应用或后续传输/日志编排调用，不认证网页用户或机器令牌。②接收原model值与显式秒时间，不规范化、不读时钟；具体前提见本行。③结果、空值与顺序见本行。④未列错误的方法没有error返回；错误与诊断不混用。

⑤所有计算只在内存中，不读仓储、不保存，故不提供事务或原子提交保证。改变执行记录时复制Execution及Signals映射，保留冻结快照；拒绝不改输入，错误返回副本可能已计算部分结果，应用不得保存。⑥相同输入和时间结果相同；具体重复标志见本行，不刷新版本、不自动重试。⑦不复查数据库版本或锁内事实；调用期间不得并发修改任务、快照或信号正文。⑧不记审计、不发通知、不接收上下文、无取消或新增超时；这些副作用由应用保存入口承担。

| 方法 | 原输入、前提与返回 | 错误、顺序及重复 |
| --- | --- | --- |
| CanBoot | 任务；pending/booting为true。 | 无错误，不检查平台或执行记录。 |
| Boot | 任务、架构、固件、秒时间；返回下一任务。初始期限900秒，最多3次引导。 | 无效架构/固件/状态先ErrProtocol；复制后拒绝ReachedReboot。平台不符、次数已满、booting期限已到转failed并清期限，无error。pending且BootCount为0重建当前尝试的起点。 |
| BootDeliverable | 任务；返回!State.Finished()。 | 只保留原提交后发引导条件，不另加状态或非nil执行记录要求。 |
| PreseedAvailable | 任务；booting/installing为true。 | 不检查格式、凭据、期限或推进状态。 |
| InstallerAnswerAvailable | 任务；仅Snapshot.Format为preseed时true。 | 不替代此前Preseed资格检查，不能把二者合并。生产调用位于Gateway.InstallerAnswer。 |
| InstallingNeeded | 已通过GetAuthorized(Install)的任务；State不是installing时true。 | 与StartInstalling共用同一重复谓词，供应用决定是否读推进时钟。 |
| StartInstalling | 同上，Execution须非nil；传入秒时间，返回下一任务及是否改变。 | 已installing返回原值/false；否则复制并进入installing/packages，期限min(at+3600,IssuedAt+5400)，返回true。不重复检查凭据。 |
| Advance | 任务、Signal、秒时间；返回下一任务、是否完整相同重报、error。 | 先缺Execution/正文4096/早于IssuedAt，再按SignalKey及完整指纹查重，然后终态/16384信号/期限/冻结插槽与顺序；拒绝ErrProtocol。期限到达即拒绝新信号，精确重报早于期限及终态；重启后旧安装插槽即使精确也拒绝。pre600/packages3600/post1200，安装期限不超过IssuedAt+5400；reboot480/initializing900。 |
| SignalKey | Signal；按Phase-Slot-Instance返回文本。 | 不校验内容。Signal字段依次为Phase、Slot、Timing、Instance、Digest、RC、Body，无JSON标签；完整JSON的SHA-256小写十六进制为存储指纹。应用attemptKey再加attempt前缀。 |
| FinalSignal | Signal；TimingFirstboot或booted/init-start/init-done/ready时true。 | 不检查任务状态或信号是否可推进。 |
| SignalAccessAllowed | 已通过原凭据验证的任务与Signal；收尾信号或非终态时true。 | 保留收尾及精确重报的原后续规则；不替代Advance或凭据检查。 |
| InstallAccessNeedsTime | 任务；有Execution且Deadline>0时true。 | 与InstallAccessAllowed共用期限谓词；应用仅在凭据检查成功后按结果读原额外时钟。 |
| InstallAccessAllowed | 任务、秒时间；无Execution为false；有正期限且at>=Deadline为false；其余按PreseedAvailable。 | 不验证签名或5400/86400寿命；无需时间时忽略传入0，不增加时钟读取。 |
| CanCancel | 任务；非终态返回nil，否则一个task_control_state。 | 只诊断，不保证保存成功。 |
| CanRetry | 任务；先查ReachedReboot，再查failed/cancelled。 | 已重启先返回一个task_retry_after_reboot；可重试nil；其他状态一个task_control_state。保留问题顺序。 |
| Cancel | 任务、秒时间；返回下一任务与问题。 | 先CanCancel，拒绝返回原值及问题；成功复制，无Execution先建attempt1；转cancelled并清Deadline，问题nil。 |
| Retry | 任务、秒时间；返回下一任务与问题。 | 先CanRetry，拒绝返回原值及问题；成功复制，无Execution先建attempt1，再递增Attempt并重建记录，转pending，问题nil。 |
| Timeout | 任务、秒时间；返回下一任务及本次是否到期。 | 无Execution、Deadline为0或未到期返回原值/false；其余复制为failed、清期限并返回true。不额外筛状态；四状态扫描仍在应用。 |
| SelectFile | 原冻结Snapshot与精确文件名；返回FrozenFile的Kind、Slot、Body、Hook。Hook为true表示正文可直接交付，允许空正文；其余Kind表示待渲染文件。 | hooks/先查摘要长度64，再按快照插槽/实例原顺序，只查Exec插槽；每个同摘要正文均复核，后续损坏仍ErrFrozenContent。未找到ErrFileNotFound。其他文件依次主应答、非空辅助名、首个匹配Exec脚本；scripts/02.sh不等于scripts/2.sh。不重新选择资源、不渲染。生产调用位于Gateway.File；内部错误不可直接回传机器。 |
| CanReceiveInstallerLog | 任务与尝试号；非终态、Execution非nil且Attempt相等时true。 | 不查地址租约或当前时间，不裁剪正文。由执行应用的ReceiveInstallerLog调用；Collector不再判断任务事实。 |

ErrProtocol、ErrFileNotFound、ErrFrozenContent是本领域的三个稳定错误对象；前者由应用别名引用，不重建错误。其他普通输入输出结构不新增独立操作。领域只依赖model/catalog及所需纯标准库，不取得密钥、不访问文件、网络、数据库、时钟或随机数。

## 机器交付入口

本节是Gateway四个入口及NewGateway的唯一八项约定来源。①只供可信机器传输及组装调用，不认证网页用户；仓储不认证机器令牌，具体授权仍交本模块Service。②接收原Go结构值与原上下文，不新增规范化、大小限制、默认地址或格式能力；SN规范化沿用Service。RemoteAddr只作原连接地址，不读取转发头。

③结果和顺序见下表；BootResult.Found=false时Body为空，由HTTP写原本地盘文本，应用不返回HTTP状态。④每阶段按来源包装GatewayError，不按Cause的文字或全局哨兵猜类别；失败不返回已渲染正文。⑤不新增跨渲染与数据库事务，状态/事件的原子范围只沿用Service；读任务、观察、渲染和最终推进是独立操作。

⑥Gateway不重试、不缓存任务、文件或凭据；Service本来的冲突重读保持。重复可能读到新任务事实；相同信号的重复处理仍由Service/领域负责。⑦不在渲染后复查所有文件；只有主应答原样调用Answer，重新授权和提交复查沿用Service。调用者不得并发修改输入信号正文、冻结任务或渲染值，依赖的并发安全由实现负责。

⑧Gateway不写额外审计或提示，Service成功提交后的通知保持。原上下文传入Service及Renderer；观察同步执行且不接收上下文。无新计时器、超时或提交后取消检查；任务在渲染期间取消时，主应答的随后Answer拒绝，正文不会交付。普通菜单的已提交Boot不因随后渲染失败而回滚。

| 方法 | 输入、成功结果与精确顺序 | 错误来源与副作用 |
| --- | --- | --- |
| NewGateway | GatewayDeps含必需的本模块*Service、FrozenRenderer、四个普通Endpoints地址及可nil观察函数。服务或渲染器缺失（含有类型的nil）返回普通构造错误；地址不另验证。成功返回独立Gateway。 | 不访问仓储、渲染、时钟或观察；无审计、通知、事务、版本复查、重试、取消或超时。重复构造不共享新缓存，依赖安全由注入方保证。 |
| BootMenu | BootInput含SN/Arch/Firmware。Service.Boot先完成原状态提交；Found=false返回零BootResult。找到后，先Credentials取得安装、收尾两凭据，再以FileMenu及冻结Task渲染，成功返回Body及Found=true。 | Boot任意错误为LookupUnavailable；渲染任意错误为RenderRejected。不增加观察，Boot提交先于渲染，不撤回已提交事件/通知。 |
| InstallerAnswer | InstallerAnswerInput含SN/RemoteAddr。先Service.Preseed，随后InstallerAnswerAvailable只检查原preseed格式；任一未找到返回零BootResult。Credentials先安装后收尾供渲染；FileAnswer渲染成功后，再调用Credentials取得安装凭据，随后Answer，成功才观察原Task与原RemoteAddr，最后返回正文。 | Preseed任意错误LookupUnavailable；渲染任意错误RenderRejected；Answer任意错误AnswerConflict。失败不观察。不得缓存或合并渲染前后两次凭据取得，也不把格式检查替代Preseed资格检查。 |
| File | FileInput含TaskID/Token/Name/RemoteAddr。先GetAuthorized(Install)，成功立即观察，再SelectFile按冻结快照完整原顺序选文件并检查每个匹配摘要。Hook=true正文直接使用（允许空正文），不取得渲染凭据；其余先Credentials再按Kind/Slot渲染。仅主FileAnswer在渲染后调用Answer，成功才返回正文。 | 初次GetAuthorized任意错误Unauthorized；选择未找到FileNotFound；摘要不符及渲染错误RenderRejected；最终Answer任意错误AnswerConflict。未知文件、摘要失败或渲染失败前的观察不能撤掉，脚本/辅助/组件不推进Answer。 |
| Signal | SignalInput含TaskID/Token/RemoteAddr及完整domain/execution.Signal。仅观察函数非nil时，先按FinalSignal选原范围并GetAuthorized；成功才观察。随后始终独立调用Service.Signal。观察验证失败也继续，观察nil不增加这次读库。 | 最终Signal任意错误SignalConflict。观察验证错误不直接返回，不能和Service.Signal的凭据/状态验证合并；观察成功不保证随后Signal成功。 |

## 机器交付错误

GatewayFailureKind固定六项来源类别：LookupUnavailable、RenderRejected、Unauthorized、FileNotFound、AnswerConflict、SignalConflict。GatewayError含Kind与Cause；Error仅返回Kind固定文字，不包含Cause；Unwrap原样返回Cause，允许内部errors.Is/As诊断。Error/Unwrap是纯错误值操作：可信调用者传原错误对象，不规范化、无大小限制、无独立错误或诊断列表、同值结果相同；不认证、不读写存储、无事务、冲突、提交复查、审计、通知、上下文、取消或超时。

HTTP仅按Kind分别映射503/422/403/404/409/409。未知Kind或未分类错误按入口兜底：Boot/InstallerAnswer为503，File为403，Signal为409。HTTP正文只写固定状态文字，不输出Cause或渲染正文。构造错误由启动组装返回，不转成机器请求状态。

## 冻结渲染端口

FrozenRenderer.Render属于执行模块的出站能力，cmd用RenderFunc连接authoring.MachineRenderer，两个应用模块不互相导入。①只供已由Gateway按原顺序授权或按SN引导的冻结渲染调用，不另认证网页用户或机器令牌。②原上下文及RenderRequest{Task,Kind,Slot,Values}逐字段传入；不修改冻结Task或Values，不新增规范化、限制、默认值或资源选择。

③返回渲染正文与原错误，不改变顺序、空正文或错误；RenderFunc.Render只调用注入函数一次并原样返回。④端口不重分类错误；Gateway在渲染阶段包装RenderRejected。实际渲染规则及错误顺序唯一沿用authoring.MachineRenderer.Render的方法旁约定。

⑤渲染本身不提交任务、事件或审计，无数据库共同事务；其配置读取与正文处理按原MachineRenderer约定。⑥没有端口重试或缓存，重复调用由原渲染器决定。⑦不保证多次调用共享配置快照，不做版本或锁内复查；调用期间不得并发修改输入，函数及渲染器并发安全由注入实现保证。⑧无额外审计、通知、超时或取消检查，原上下文原样委托，取消及外部读取行为沿用原渲染器。

## 地址观察与日志现状

NewIPObserver 与 IPObservationSink.ObserveIP 保留各自方法旁的完整约定：只有原凭据成功的观察位置才调用，按原Attempt及IssuedAt+5400/86400登记原RemoteAddr；无Execution不登记。地址处理、有效期与容量由Collector决定，不读转发头。普通文件请求在凭据检查后、选择文件和渲染前观察；信号在最终Signal前尝试GetAuthorized，成功就观察，观察成功不保证随后信号推进成功。Debian应答在Answer成功后观察。普通Boot菜单不增加观察。

当前Collector.ObserveIP只在内存登记：解析host:port并把IPv4映射地址还原，非法任务/尝试/地址、已过期忽略。新地址最多4096项，重复地址覆盖；锁保护登记，过期项清除。无错误、事件、审计、通知、事务或提交复查，不接收上下文，重启丢失归属。

Collector.Receive先解析直接连接的host:port并Unmap；非法地址、无租约或到期等号都成功忽略，不读Forwarded。互斥取归属后，在原位置只取一次时钟为At；有有效租约才先截正文前65536字节，再SafeLogTail脱敏并取最多4096字节尾部，随后同步调用InstallerLogReceiver。正文、原上下文及捕获的At原样转交；接收错误原样返回。Collector不读任务、不写事件、不通知。

启用机器日志时，原 `machine.syslogListen` 同时监听 UDP 和 TCP，部署须允许两种协议到达同一地址与端口。先绑定 UDP，再用实际绑定地址打开 TCP；端口为 0 时也共用同一实际端口。任一协议绑定失败都不启动日志或恢复后台，并关闭已开的日志和 HTTP 监听。不增加配置或数据库字段，`preview.syslogAddr` 仍是客户机可到达的日志地址。

原生 TCP 由 `infrastructure/events/syslog_tcp.go` 接收，只支持以换行字节 LF 分隔的消息。最多 64 条活动连接；正文最多 65536 字节，分隔用的 LF 不计入且不保存，其他字节保留。超长消息关闭连接且整条丢弃，不按截断片段收录；断连或读错误时没有 LF 的末条也丢弃。每次实际网络读取续上 30 秒空闲期限，到期关闭连接。正常断开、超长及空闲关闭都会释放连接名额。

架构导入检查只为此网络职责精确增加 `bufio`、`time`、`errors`，分别供有界分帧、读取期限和异常保护使用。没有开放任意标准库；数据库、文件、进程、HTTP 客户端和应用业务实现仍不归日志传输。

TCP 不在连接上缓存任务归属，每条完整消息以真实 RemoteAddr 调用原 Collector.Receive，再由原应用入口检查任务状态和尝试号。无租约、到期、旧尝试和终态仍按原规则忽略；没有转发头、正文身份或认证旁路。取消、HTTP 服务退出或 TCP 接收退出时关闭监听及全部活动连接，并等接收工作结束后才返回。单条收录错误或异常只记录不含正文和凭据的提示，后续完整消息仍可接收。UDP 仍使用原有逐包接收与裁剪行为。

## 日志收录入口

NewInstallerLogs(repo, notifier)连接InstallerLogRepository及本执行包已有的仅Publish通知口，返回InstallerLogReceiver；构造不查库、不读时钟、不校验或改写输入，可信组装必须提供非nil仓储。以下八项共同适用于构造、端口ReceiveInstallerLog及具体实现；仓储小口两方法的保存细节唯一委托repository.Tasks.GetTask、repository.Events.AppendEvents及repository/doc.go，不另加规则。

①只供可信日志适配器调用，地址归属已由凭据成功后的租约观察确认；本入口不认证网页用户或机器令牌，不能作为接受网页伪造TaskID/Attempt/SourceIP的入口。

②输入为InstallerLog{TaskID,Attempt,SourceIP,At,Body}，Body已截断并脱敏。上下文与各字段不规范化、不重取时钟。调用期间不得并发修改Body；接收器在返回前同步编码为log_base64，不保留可变输入切片。

③固定顺序GetTask→domain/execution.CanReceiveInstallerLog→AppendEvents→成功Publish。非终态、Execution非nil且Attempt相同才收录；其他事实成功忽略。事件固定installer.syslog、空Key、原At，只含原source_ip与log_base64；返回error，无列表或分页顺序。

④GetTask的ErrNotFound（含包装）成功忽略，其他读取错误原样返回；资格不符不追加。AppendEvents任何错误（含ErrNotFound、取消）都原样返回，不通知；成功后仅Publish一次，通知没有可返回错误。

⑤不推进Task、不修改Rev、不记用户审计；只追加事件，保存事务沿原AppendEvents，不引入应用事务。租约读取、任务读取与追加不在共同事务内。

⑥无入口重试或去重。重复日志包仍逐包追加，Key保持空，不用TaskRev作保存条件。

⑦明确保留原有“读时判断后再追加”的窗口：GetTask之后发生取消或重试，也不新增原子Attempt复查；不承诺解决此窗口或跨实例归属。Collector只为内存租约加锁，重启丢失归属；UDP不保证必达。

⑧无审计；仅AppendEvents成功后通知，无额外取消、超时或提交后检查。ctx原样传仓储，由仓储返回取消；若仓储已成功即按原顺序通知，即使ctx已取消。UDP的ReadFrom缓冲和错误日志仍在cmd/lober；TCP传输在infrastructure/events，cmd仅装配监听与服务生命周期。TCP的读取空闲期限只约束网络连接，不增加应用收录的事务或提交后检查。

## 机器传输顺序现状

机器HTTP入口保留text/plain、no-store和nosniff。/v1/boot先解析已知单值查询，再校验/转换架构与固件，才按User-Agent分普通菜单与Debian安装器；无安装任务返回现有本地盘脚本。Boot错误503，渲染错误422。Debian路径先Preseed与格式，再渲染，再Answer，Answer失败409，成功才观察并写正文。

文件路径先做路由形状、ID、安全词、方法和查询检查；GetAuthorized失败403。凭据成功后观察，然后选择真实应答/辅助文件/脚本/组件；未知文件404，组件正文摘要不符422，渲染失败422。主应答在渲染后重新Answer，失败409，之后才写正文。取消与渲染可能已分别完成，不存在跨渲染事务。

信号先解析查询、限制正文4096（超限413、其他读取失败400），再调用Gateway.Signal；Gateway按上节尝试凭据观察，最后Service.Signal。Signal错误统一409，成功200空正文。协议传输错误与应用ErrProtocol/ErrCredential的映射保持，不能把内部错误文本直接回传。

## 尚未实现的目标入口

下面记录Task3/4/5已落地位置；最终执行链是否完整仍由Task7保护与Task8核对。

| 所属任务 | 目标能力 | 必须保留的当前约定 |
| --- | --- | --- |
| Task3（已实现本项） | 纯执行、控制、冻结选择和日志资格规则归domain/execution；应用已接入执行与控制，冻结选择已由Task4接线，日志资格已由Task5接线 | Signal查重与凭据时序、阶段预算、错误类别、State/Rev竞争、通知位置；不新增装机能力。 |
| Task4（已实现本项） | execution.Gateway承接机器引导、文件请求与信号编排，HTTP只解析及输出 | 观察位置、组件摘要、渲染后Answer、原凭据范围及每条传输错误映射；接口只剩传输。 |
| Task5（已实现本项） | 执行应用承接日志的任务规则、AppendEvents和成功后Publish；Collector继续管理IP租约/地址归属，并先截断再SafeLogTail | 归属上限/有效期/重启丢失、忽略次序、原日志字节、错误和通知位置；租约与字节处理保留在外部实现。 |

镜像I3已由Task2纠正，当前约定见[镜像第11节](media.md#11-额外来源取消类型纠正i3)。Task6只细化[单次模板候选读取](backend-modules.md#单次模板候选读取的后续细化)，不承诺跨页一致，也不改变写事务。

## 当前验证位置

纯规则：`internal/domain/execution/{protocol,control,delivery,log}_test.go`；执行服务与凭据：`internal/application/execution/{service,protocol,token,control_intent,observation,installer_logs}_test.go`。机器传输解析/状态映射：`internal/interfaces/machine/http_test.go`；真实Service交付顺序、旧文件/UA业务断言、错误来源与取消：`internal/application/execution/gateway_test.go`。日志收录事实、错误/通知与读取追加窗口：`internal/application/execution/installer_logs_test.go`；日志归属、裁剪与TCP边界：`internal/infrastructure/events/{syslog,syslog_tcp}_test.go`。组装：`cmd/lober/{machine,assembly,machine_delivery,machine_tcp_delivery,machine_syslog_lifecycle}_test.go`。事务与竞争：`internal/infrastructure/persistence/pgstore/{contract_tasks,contract_events,contract_concurrency}_test.go`及执行记录相关测试。这里只登记验证来源；Task3/4/5实际运行结果分别记在对应报告，不表示关闭Task7保护、Task8全链复核或其他后续缺口。
