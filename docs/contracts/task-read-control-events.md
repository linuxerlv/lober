# 任务读取、控制和事件约定

任务列表、详情和冻结预览通过 [tasks.Reader](../../internal/application/tasks/read_contract.go) 进入应用。取消和重试通过 [tasks.Controller](../../internal/application/tasks/control_contract.go)。事件查询和订阅通过 [tasks.EventReader](../../internal/application/tasks/event_contract.go)。网页层只处理请求、响应和连接循环。

## 七个入口

| 入口 | 权限 | 顺序和结果 |
| --- | --- | --- |
| GET `/console/v1/tasks` | `task:read` | 网页先解析分页数字及混用方式。应用先验证身份，再检查分页、created_from、created_to、时间范围、host_sn、state，之后查询。每项先 CanCancel 后 CanRetry。 |
| GET `/console/v1/tasks/:id` | `task:read` | 按原编号查一次，再取诊断。只读取已保存的冻结快照。 |
| POST `/console/v1/tasks/:id/preview` | `task:read` | 网页解析JSON。应用先验证身份，再检查非空请求固件，随后查任务。请求固件为空时取快照固件；仍为空时取UEFI并追加firmware样例标签。 |
| POST `/console/v1/tasks/:id/cancel` | `task:cancel` | 查任务、生成可信审计意图、委托原控制、成功后重读、补发提示、诊断。审计动作仍为task.cancel。由tasks.Controller完成。 |
| POST `/console/v1/tasks/:id/retry` | `task:retry` | 同取消；动作仍为task.retry。原版本交原执行服务复查，不自动改用新版本。由tasks.Controller完成。 |
| GET `/console/v1/tasks/:id/events` | `task:read` | 解析after_seq、limit后直接读取。Seq大于after_seq，升序；不额外查任务。由tasks.EventReader.List完成。 |
| GET `/console/v1/tasks/:id/stream` | `task:read` | 解析after_seq，非空Last-Event-ID覆盖；查任务、订阅、首读200条后才发送流响应头。由tasks.EventReader.Open完成。 |

## 输入、错误和空值

应用的操作者只能来自可信身份入口。无身份、禁用、角色无效、强制改密均在参数和仓储之前拒绝。原五角色都能读取任务。网页继续按每请求身份快照授权。

分页保留游标和页码两种方式：游标默认100条，页码默认第1页、每页20条，最多500条。时间沿用十进制整数解析，不能为负，空文本是0；时间范围包含两端。主机编号规范化之后才验证状态。编号不存在及无效编号由原仓储决定，不增加应用格式检查。

列表保留仓储原顺序、next、total。空items和空诊断在网页输出`[]`；游标响应不凭空增加total，其他null形式不变。详情字段及冻结快照结构继续使用[原任务字段](backend-modules.md#返回字段)。

仓储错误保留原分类。参数错误仍返回400。渲染先返回错误，再由原Judge路径根据首项问题返回409或422，保留完整问题顺序；不能把普通渲染问题改成固定422的仓储问题错误。

预览只将查回任务的原快照及实际task.ID交给既有FrozenPreview。不会读取当前主机、镜像、模板或组件。样例标签先保留渲染器顺序，无错误且无问题时才追加固件标签，并按最终标签设置每个文件的sample标记。正文、文件和问题字段不变。

## 写入、审计与通知

读取和预览不写业务、审计或通知，也不启动后台工作。Tasks和Renderer必须显式注入，缺失即拒绝构造；Diagnostics缺失时保留空诊断。不会回退到网页旧依赖。重复调用可以读到新的状态和诊断；分页依赖每次仓储事实，不承诺跨多次读取的一致快照。冻结快照不重新计算。没有写入，所以没有原子写、提交时复查或版本冲突重试。请求context原样传给仓储和渲染器，由原能力处理取消和超时，不新增计时器。[读取小接口](../../internal/application/tasks/port/task_read.go)只暴露查询方法。

控制仍由原执行服务把状态、执行记录、事件及审计放在同一事务中提交。Controller显式要求任务读取、后端、时钟和通知，缺失即拒绝构造。机器未启用时注入NewUnavailableControl：不提供诊断，取消和重试仍返回422及原消息。

应用先检查各自权限，再读取原任务，用可信操作者、固定task.cancel或task.retry、空说明和一次注入时钟生成审计。不会在委托前重算诊断或提前拒绝。原任务版本原样传递；重复请求和并发条件由既有后端在锁内复查，冲突不自动换版本重试。后端失败直接返回，不重读、不补发，也不另写审计。

成功提交后执行服务发提示；应用成功重读后仍补发一次，然后先取取消诊断再取重试诊断。context原样传递，提交成功后不插入额外的取消检查。提交成功而补读失败时，不补发第二次提示，也不能声称数据库已经回滚。

## 事件流

保持text/event-stream、no-cache及X-Accel-Buffering:no。每条格式仍为id、event:task和JSON data，空页也Flush。订阅早于首读，首读失败立即退订且使用普通错误响应。每页发送前先检查连接上下文，再用最初令牌复查权限；不从新令牌续命。

每页200条，满页继续补读，短页等提示或5秒轮询；任务结束不自动断流。断开、写失败、提示关闭及晚期读取失败均结束并退订，发头后不再写JSON错误。通知继续按任务隔离、容量一、合并提示，重复退订安全；不新增持久队列或必达承诺。

EventReader必须显式注入任务读取、事件读取和共享通知实例。List在权限之后直接沿用原ListEvents校验，不增加查任务。Open先检查权限，再拒绝负after_seq，随后查任务、订阅、首读；首读失败先退订。返回的Event是普通数据，Fields独立复制，网页仍把空字段输出为空对象。

EventSubscription提供Initial、Hints、Read和Close。Initial每次返回独立副本；Read固定200条并传递原context，空结果为数组。Close可重复或并发调用，退订只执行一次。应用不新建通知实例，也不启动后台任务或计时器。网页构造必须显式传入TaskReader、TaskController和TaskEvents，缺少时立即失败。
