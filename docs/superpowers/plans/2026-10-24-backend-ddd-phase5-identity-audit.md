# 后端四层迁移第五阶段：身份权限与审计实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 登录、会话、用户管理、菜单权限和审计读取通过明确的应用操作协作，保持现有接口、口令强度、角色、保存与并发行为。

**Architecture:** 沿用已批准的四层设计。身份应用持有一份会话表和凭据协调锁；密码计算和随机令牌由独立能力提供，纯权限与菜单规则归领域。审计应用负责查询及完整导出，数据库继续保证写入与审计一起提交。

**Tech Stack:** Go、现有 Gin、PostgreSQL、Wire、现有控制台契约生成检查。不新增登录协议、数据库表、依赖或前端功能。

**Spec:** `docs/superpowers/specs/2026-10-22-backend-ddd-four-layer-design.md`（用户已确认）；阶段范围见 `2026-10-22-backend-ddd-roadmap.md`。同日其他登录设计不作为已实现事实，本阶段不顺带引入刷新令牌、Cookie 或动态角色。

**状态与起点：** 本文待用户审阅，只制定计划，未实施、未运行测试。基于已在 lober VM 完成第四阶段合并验收的本地 `dev`：`9c457799133f64a268f0c3607233feaaa78bc863`。计划和后续模块分支为 `codex/ddd-identity-audit`。默认沿用逐项实施、独立审查和最后整分支审查。

## Global Constraints

- 设计第1节：“本次重构保留现有业务行为、数据库约束、数据格式和对外接口。”角色增减、登录方案变化和口令政策升级另立功能，不混进搬迁。
- 设计第5节：“应用模块不能直接导入另一个应用模块的具体服务。”身份与审计各自的能力放在自己的 `port`；只有共同的普通审计意图进入 `application/ports`。
- 设计第6节：“不能只写方法签名就称为契约完成。”逐操作记录身份、输入、结果、错误优先级、保存范围、重复、并发、审计及取消。
- 设计第7节：“不引入可以执行任意数据库操作的通用事务回调，也不把数据库事务对象交给上层。”保留 Users、RoleMenus 和 AuditLog 的现有窄操作。
- 设计第7节：“审计与业务写入在同一事务。提交成功后才能发通知；事务失败不发通知。”身份写入只在成功返回后撤销会话；不新增通知或必达事件承诺。
- 设计第9节：“已迁移模块不得再调用旧业务流程。”不把旧入口换名保留，不移动清除阶段来凑数量；前四阶段持续清零。
- 保留五种角色和16项实际权限，`@any` 另作已登录用户标记。菜单显示不授予接口权限，只有管理员能管理用户、角色菜单和读取审计。
- 保留 `pbkdf2-sha256$600000$…`、16字节随机盐、32字节派生结果、固定600000轮校验和常量时间比较。令牌为32字节随机数的64位小写十六进制；不改为签名令牌或持久会话。
- 所有 Go、gofmt、Wire、Node 生成、构建和测试只在 lober VM，连接依用户指定私有环境文档。不得打印凭据、提交私有连接文件、重启服务或修改全局配置。
- 沿用 `GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=off`、原 Bash/Busybox 路径及 `go test -race -timeout=20m ./...`。不降低口令强度，不改测试准备缓存、超时或断言换通过。
- 本阶段只交付模块分支。合入 `dev`、推送和推进 test/preview/main 需要相应用户授权；保留已有工作区、用户文件和前阶段日志。

## Review Focus

1. 登录已读到旧散列时，管理员重置或本人改密先提交：旧口令不能随后发布有效会话；失败写入不能撤销原会话。Task2/3/5用通道控制顺序并检查真实数据库及会话。
2. 慢资料请求与重置、禁用、降级相遇，以及两位管理员同时移除资格：不能用猜测的新修订号覆盖旧安全字段，数据库必须至少保留一位启用管理员。Task3保留真实并发测试。
3. 菜单没有配置、显式空配置、必需页面和旧修订号：分别保持默认、空结果、拒绝与冲突；隐藏菜单不能改变接口权限。Task4检查两次首次保存及审计回滚。
4. 长连接中退出、到期、禁用或强制改密：仍复查原令牌及最新用户事实，不因搬迁丢失终止检查，也不改变查询令牌适用范围。Task5保留实际网络事件流测试。
5. 审计导出超过500条，后页失败、请求取消或字段含公式前缀：失败时不发成功的残缺下载；成功时顺序、全部页、公式安全及原文本均保持。Task6检查晚失败和真实导出内容。

## 1. 实际基线与范围

起点代码：`interfaces/console/{auth,sessions,password,profile,users,menus,audit,perm,routes,server,pagination,tasks,taskcontrol}.go`；保存约定在 `domain/repository/{users,role_menus,audit,types,check}.go`；实现及真实测试在 `infrastructure/persistence/pgstore`。启动账号初始化在 `cmd/lober/app.go:Initialise`。

路径简写：application/domain/infrastructure/archtest以internal/为根；console/表示internal/interfaces/console/，单写identity/表示internal/application/identity/。cmd/、docs/及已写internal/的路径从仓库根起。

现有 `infrastructure/identity` **仅是随机对象编号能力**，有六个精确使用位置。不能把密码业务直接塞进它并放宽原编号许可。新增 `infrastructure/credentials`，只实现身份模块的密码和令牌能力。

### 1.1 十三条网页入口

URL、JSON字段、状态码、路由权限和审计动作保持；正式路由描述仍是对外契约来源。下表中的“本人”指已认证快照，不采信正文中的身份。

| 方法与路径 | 身份与行为 | 顺序、保存和错误要点 |
| --- | --- | --- |
| POST `/console/v1/auth/login` | 唯一公开入口；返回 token、expiresAt、user | 必填解析→读取账号→真实或假散列校验→读锁内重读凭据→菜单读取→发布会话。账号不存在、口令错、禁用或无效角色均为相同401说明；不写审计。 |
| POST `/console/v1/auth/logout` | 已登录；只删除本令牌 | 重复请求仍先走认证；已失效令牌401。已认证后的删除不引入新事务或审计。 |
| POST `/console/v1/auth/password` | 本人改密，允许强制改密用户 | 当前口令错误403；新旧相同或新口令不可保存400；散列计算在写锁外；锁内重读账号/版本/散列及令牌，写入与审计成功后撤销该用户全部会话。动作 `user.password.change`。 |
| GET `/console/v1/user/info` | 已登录，允许强制改密用户 | 返回认证时的用户快照及本次读取的有效菜单；不泄露密码/散列，不写审计。 |
| PUT `/console/v1/user/profile` | 本人，强制改密期间不允许 | 先 CheckProfile，再要求请求Rev等于认证快照Rev；只改四项资料。动作 `user.profile.update`。保存成功后菜单读取失败，原行为是写入已提交但响应失败，不能伪装回滚。 |
| GET `/console/v1/users` | 管理员 | 页码/游标规则不变，按用户名C排序；列表返回菜单空数组/版本0，不额外查每人的菜单。 |
| POST `/console/v1/users` | 管理员 | 基础账号/角色/口令检查→散列→创建。资料细节仍按原保存校验时机处理；创建与审计同事务，动作 `user.create`；CreatedAt与审计At仍分别读时钟。 |
| PUT `/console/v1/users/:username` | 管理员 | Rev/角色/正文账号不匹配400先于自禁用409；读目标后，初始口令要求必须真正改变；原Rev交保存层，不自动采用新Rev。动作 `user.update`；非空密码或禁用成功才撤销全部会话，仅改角色不主动清会话。 |
| DELETE `/console/v1/users/:username` | 管理员，实际为禁用 | 禁止禁用本人先于读目标；不删账号行。动作 `user.disable`；保存失败不撤销会话。 |
| GET `/console/v1/roles/menus` | 管理员 | 返回原菜单顺序及 viewer/operator/hook-author/hook-approver/admin 顺序，无写入。 |
| PUT `/console/v1/roles/:role/menus` | 管理员 | 保留非nil空数组与未给数组的区别；检查角色/Rev/重复/可用页面/必需页面，再按目录顺序保存。动作 `role.menus.update`；数据库生成 before/after 审计详情。 |
| GET `/console/v1/audit` | 管理员 | 先from/to过滤，再分页；时间含两端，文本精确匹配，ID降序。查询不写新审计。 |
| GET `/console/v1/audit/export` | 管理员 | 每页500条，游标读到结束，全部成功才设置下载头并输出；查询不写新审计。 |

共同响应为原 `code/data/message`。稳定错误沿用400、401、403、404、409、422和500及现有中文原因。`ErrLastAdmin`仍409；仓储 ProblemsError 保留422及定位内容。不得靠匹配错误文字分支。

### 1.2 身份、会话和权限的细节

- 保留 Authorization 优先；格式错误时不退回查询令牌。只有被路由标记为事件流的GET允许 `access_token`，普通请求不能用查询令牌登录。
- 每次认证先查本实例会话，再读最新账号。缺失/禁用账号会删除该令牌；无效角色只拒绝、不顺带清令牌；仓储故障不改写为401。
- 默认会话43200秒；配置秒数为0取默认，负值保持构造失败。到期比较是 `expires <= now`；保留加法溢出及负时钟时饱和到MaxInt64。不存在令牌的查找不额外读时钟；发令牌时按原顺序清理已到期记录。
- 会话表、过期规则和凭据读写锁放在同一个应用实例中。密码推导仍在协调锁外；登录最终确认到发布持有读锁，重置/禁用提交到清会话持有写锁。普通资料修改继续依靠认证快照与数据库Rev，不新增锁。
- Principal只在当前请求中使用，不跨请求缓存。除原有锁内及事件流复查外，不增加额外数据库认证；取消继续经context传入原仓储调用，不在写入成功与会话撤销之间新增取消检查。
- 强制改密保留原允许清单：userinfo、logout、本人password，以及**管理员本人**旧PUT用户入口；旧PUT例外并不授予非管理员用户管理权限。其他请求先报需改密，再判断普通权限。
- 登录输入不自动trim/改大小写，也不提前用新口令的存储规则改变原401优先级。账户不存在时执行固定假散列工作；禁用或角色失效也不能跳过原有口令校验。
- CreateUser/UpdateUser/ListUsers只作原基础用户结果转换，MenuIDs是非nil空数组、MenuRev为0；只有Login/CurrentUser/UpdateProfile读取有效菜单。不能给管理员写入额外增加菜单读取故障点。
- 事件流的复查仍先查令牌，再读账号，检查禁用、强制改密、角色与task:read；账号异常只使流检查失败，不新增删令牌副作用。事件分页、5秒轮询、订阅和发送不在本阶段搬迁；不承诺数据库提交与每条网络写入组成原子操作。
- 启动初始化仍在内置内容准备之后：ListUsers(Limit=1)，已有账号则忽略seedAdmin；空库必须显式给seedAdmin，创建admin/“管理员”/强制改密。Actor=bootstrap、Action=user.create，两次时钟读取保留。并发首次初始化仍服从唯一约束，不顺带改成分布式初始化协议。

### 1.3 菜单和审计导出

菜单目录只留一份，迁至 `domain/access/menus.go`，包括原ID、标题、父项、权限和Required。只允许授予二级页面。没有设置时用全部可用页面；显式空设置不等于没有设置；admin的role-menus页面必需。读取时按当前权限和目录顺序过滤，配置不能扩大接口权限。保存的Rev=0仅表示首次创建，陈旧版本/首次创建竞争409；不能把锁内版本检查搬成应用预检查。

审计列表原文不做公式转义。导出列序固定 `id,at,actor,action,target_kind,target_id,detail`，使用原encoding/csv、LF、无BOM。公式安全判定只为观察首个有效字符跳过Unicode空白/Cf，遇 `=+-@＝＋－＠` 或原首字符为tab/CR/LF时在**原文**前加tab；不得trim原文。保留一次完整内存缓冲，不擅自分页截断、增加上限或改成提前发200的流式下载。

## 2. 三个必须提前迁出的接缝

起点四清单为64/0/42/1，共107项：阶段5为35项仓储+30项规则=65项；阶段6为42项。

仅删65项会留下两种断裂：删除Deps.Users后事件流仍访问它；删除auditFrom后任务控制仍要求repository.Audit。计划同时迁出下列三个阶段6条目，**删实际旧依赖，不改其阶段编号或挪表**：

| 清单 | 原文件/函数 | 原符号 | 本阶段接替 |
| --- | --- | --- | --- |
| migrationExceptions | tasks.go / (*server).streamAuthorized | repository.Users.GetUser | 身份Registry.StreamAuthorized |
| consoleRuleMigrationExceptions | tasks.go / (*server).streamAuthorized | model.Role.Valid | 同一个身份应用复查 |
| migrationExceptions | taskcontrol.go / package-init | repository.Audit | 普通ports.AuditIntent，由execution适配层转保存类型 |

最终精确删除68项，107→39；四清单为**27/0/11/1**。阶段5及前四阶段为0，阶段6剩39项逐条与起点相同。第六阶段的任务查询、状态控制本体、事件分页/发送、错误映射和通知清理仍保留，不扩大本阶段范围。

`application/ports.AuditIntent`定义四个普通字段Actor/Action/Detail string、At int64，不能是repository.Audit别名。控制台改用auditIntentFrom从可信上下文和路由描述取得它；execution新增AuditedControl只做意图到原Audit的转换并委托现有Service。Cancel/Retry原状态规则、保存、错误及通知顺序不变，CanCancel/CanRetry仍委托原实现。

## 3. 实施接口和文件职责

以下是实施目标，不表示代码已存在。公共结果不携带仓储Page、Audit或密码散列；网页DTO只转换这些普通结果。

```go
// application/identity；context均为标准context.Context。
type Principal struct { user model.User; token string; owner *Service } // 私有值快照，不公开凭据
func (p Principal) Actor() ports.Actor
type session struct { username string; expires int64 }
type SessionBook struct {
    mu sync.Mutex
    byToken map[string]session
    ttl int64
    now ports.Clock
    tokens port.Tokens
} // 同模块运行组件，不是网页入口
func NewSessionBook(ttl int64, clock ports.Clock, tokens port.Tokens) *SessionBook
func (s *SessionBook) Put(username string) string
func (s *SessionBook) Lookup(token string) (string, bool)
func (s *SessionBook) ExpiresAt(token string) int64
func (s *SessionBook) Drop(token string)
func (s *SessionBook) DropUser(username string)
type LoginCommand struct { Username, Password string }
type SessionResult struct { Token string; ExpiresAt int64; User UserView }
type UserView struct {
    Username, DisplayName, Email, Avatar, Bio string
    Role model.Role
    Permissions, MenuIDs []string
    Disabled, MustChangePassword bool
    CreatedAt, Rev, MenuRev int64
}
type Operation = access.Operation // 普通领域值；General=0，其余为UserInfo/Logout/ChangePassword/LegacySelfUpdate
type AccessRequest struct { Permission string; Operation Operation; TargetUsername string }
type Access interface {
    Login(context.Context, LoginCommand) (SessionResult, error)
    Logout(context.Context, Principal) error
    CurrentUser(context.Context, Principal) (UserView, error)
    Authenticate(context.Context, string) (Principal, error)
    Authorize(Principal, AccessRequest) error
    StreamAuthorized(context.Context, string, string) bool // token、permission
}
type UserQuery struct { Page, Limit int; Cursor string }
type UserPage struct { Items []UserView; Next string; Total *int64 }
type CreateUserCommand struct { Username, DisplayName, Password string; Role model.Role; Disabled bool }
type UpdateUserCommand struct { Username, DisplayName, Password string; Role model.Role; Disabled bool; Rev int64 }
type PasswordCommand struct { CurrentPassword, NewPassword string }
type ProfileCommand struct { DisplayName, Email, Avatar, Bio string; Rev int64 }
type Accounts interface {
    ListUsers(context.Context, Principal, UserQuery) (UserPage, error)
    CreateUser(context.Context, Principal, CreateUserCommand) (UserView, error)
    UpdateUser(context.Context, Principal, string, UpdateUserCommand) (UserView, error)
    DisableUser(context.Context, Principal, string) error
    ChangePassword(context.Context, Principal, PasswordCommand) error
    UpdateProfile(context.Context, Principal, ProfileCommand) (UserView, error)
}
type MenuDefinition struct { ID, Title, ParentID, Permission string; Required bool }
type RoleMenuView struct { Role model.Role; AvailableMenuIDs, MenuIDs []string; Rev int64 }
type MenuList struct { Menus []MenuDefinition; Roles []RoleMenuView }
type SaveMenusCommand struct { MenuIDs []string; Rev int64 }
type Menus interface {
    ListRoleMenus(context.Context, Principal) (MenuList, error)
    SaveRoleMenus(context.Context, Principal, model.Role, SaveMenusCommand) (RoleMenuView, error)
}
type Registry interface { Access; Accounts; Menus } // Task4才组合；此前不写未实现方法占位
type Bootstrapper interface { Initialise(context.Context, string) error } // seedAdmin；只给组装点
func NewService(Deps) *Service // 同一*Service实现上述入口；不把Bootstrapper交给网页
func KnownPermission(string) bool // 仅路由描述合法性检查，权限决策仍用Authorize
```

`identity.Deps`字段固定为Users port.Users、RoleMenus port.RoleMenus、Passwords port.Passwords、Sessions *SessionBook、Clock ports.Clock。Users保留现有四个Users方法，RoleMenus保留两个现有方法，只在应用内部使用保存类型；不接受Store或事务对象。NewService要求五项均非nil。SessionBook由组装点一次创建并注入，TTL单位为秒、0取43200、负值panic，Clock/Tokens必须非nil，构造不读时钟。它是同模块的运行组件，不交给console.Deps或网页处理器；生产网页只能用Registry。`Principal`由同一实例Authenticate产生，内部保存完整值快照；Accounts/Access拒绝零值和其他实例的Principal。其他模块只从Actor()得到已有ports.Actor。

`identity/port.Passwords`：`Hash(string)(string,error)`、`Check(string,string)bool`、`CheckDummy(string)`；`Tokens`：`NewToken()(string,error)`。新口令的唯一纯文本条件为 `access.ValidPasswordText(pw) = pw != "" && model.StorableText(pw)`；Hash实现可以复查同一纯函数，非法值返回port.ErrInvalidPassword，应用散列助手转为原repository.ErrInvalid。CheckDummy原样使用起点auth.go的dummyPasswordHash，不把可选迭代数交给输入。NewToken成功必须给64位小写十六进制；出错或违反结果契约时会话发布panic，让原请求兜底返回500，不发空令牌当成功。

原harness_test.go的seedHTTPUser直接发会话，不能全部换成额外一次真实登录。测试夹具显式持有同一SessionBook并用Put准备令牌（只在测试文件保留关联，不加生产测试后门），与原来的session.put工作量相同；真实登录/口令比较测试仍走真实算法和HTTP。原seedPasswordHashes只替换能力调用路径，缓存范围/强度不变。SessionBook的Put/Lookup等方法在生产接口层受类型守卫禁止，不能作为新的登录捷径。

密码适配器构造为 `credentials.NewPasswords() port.Passwords`、`credentials.NewTokens() port.Tokens`。目录与策略的纯接口固定为 `access.MenuCatalog() []Menu`（Menu具有ID/Title/ParentID/Permission string及Required bool）、`AvailableMenuIDs(model.Role) []string`、`EffectiveMenuIDs(model.Role, []string, bool) []string`（storedIDs、found）、`NormalizeMenuIDs(model.Role, []string) ([]string,bool)`；最后一项只处理可用性/必需项与目录排序，格式和重复检查复用既有repository.CheckRoleMenus，不复制其算法。

强制改密例外只实现一次，放在 `domain/access/request.go`：`Subject{Username string; Role model.Role; Disabled,MustChangePassword bool}`、`Request{Permission string; Operation Operation; TargetUsername string}`、`CheckRequest(Subject,Request) Decision`。Decision为Allow/Unauthenticated/PasswordChangeRequired/Forbidden，先身份再改密再权限；Operation固定General=0、UserInfo=1、Logout=2、ChangePassword=3、LegacySelfUpdate=4，未知值拒绝。应用公开Operation及常量转发该普通值类型，先校验Principal归属，再转换事实调用纯判定。既有ports.Actor.Authorize保留原通用约束，不把@any的新特例扩给其他业务模块。

身份错误：新增ErrLoginFailed→401/ConsoleLoginFailed、ErrBadRequest→400/ConsoleBadRequest、ErrSelfDisable→409/ConsoleSelfDisable、ErrInitialPasswordUnchanged→409/ConsolePasswordChangeRequired。普通未认证/禁止/强制改密沿用ports三个错误及原401/403映射；仓储错误保留errors.Is/As和原映射，不抹掉错误树。

审计接口位于 `application/audit`：`Filter{TargetKind,TargetID,Actor,Action string; From,To int64}`，`Query{Filter; Page,Limit int; Cursor string}`；`Entry{ID,At int64; Actor,Action,Detail,TargetKind,TargetID string}`，`Page{Items []Entry; Next string; Total *int64}`。`Reader`两个方法为 `List(context.Context,ports.Actor,Query)(Page,error)`、`ExportCSV(context.Context,ports.Actor,Filter)([]byte,error)`；`NewReader(port.AuditLog) Reader`。只注入现有ListAudit窄操作。`domain/audit`提供纯 `ValidRange(from,to int64) bool` 和 `SpreadsheetText(string) string`，无文件、数据库或时钟。非法范围返回audit.ErrBadRequest，网页映射为原400/ConsoleBadRequest。

共同审计桥：`execution.NewAuditedControl(ControlBackend) *AuditedControl`，ControlBackend为原四方法签名（Audit仍在应用内部）；AuditedControl的Cancel/Retry第三参数改为ports.AuditIntent，其余不变。只对**精确execution包**开放共同ports依赖，不扩给机器入口/事件实现或其他应用服务。

| 文件组 | 职责与归属 |
| --- | --- |
| application/identity/{contract,service,access,login,sessions,views}.go及port/{repository,credentials}.go | 认证快照、会话和锁；登录/认证/授权；安全结果与菜单读取。 |
| application/identity/{users,password,profile,menus,bootstrap}.go | 每类写操作一个文件，复用同实例凭据协调、原保存契约与菜单解析。 |
| domain/access/{permissions,request,menus,password}.go | 原权限表、纯请求判定、唯一菜单目录、纯选择与新口令文本条件；不接仓储。 |
| infrastructure/credentials/{password,token}.go | 固定强度密码计算、严格散列解码、密码比较和令牌随机数。 |
| application/audit/{contract,reader,export}.go、audit/port/repository.go、domain/audit/export.go | 过滤/分页、完整CSV导出、安全文本纯规则；没有“独立写审计”的能力。 |
| application/ports/audit.go、application/execution/control_intent.go | 普通审计意图及原任务控制的窄转换，不搬任务状态规则。 |
| console身份相关文件、routes.go、server.go、errors.go、tasks.go、taskcontrol.go | 原解析/描述/响应及应用调用；事件流只更换身份复查，控制接口只更换审计参数。 |
| cmd/lober/{app,wire,wire_gen,taskcontrol}.go | 一次构造身份Service并复用；初始化只委托；原任务不可用分支仅适配参数。 |

## 4. 任务与独立验收

前四项先实现完整可测试能力，生产控制台暂用原实现。**不提前切登录而留下另一份写锁/会话表**；Task5在所有身份能力就绪后一次接线。每项都需可构建、有真实结果，不提交空服务或运行时“待实现”分支。

### Task 1：固定兼容契约与删除清单

**Files:** 新建 `docs/contracts/identity-audit.md`、`internal/interfaces/console/identity_contract_test.go`；按需补 `auth_concurrency_test.go`、`profile_concurrency_test.go`、`audit_test.go`；只补基线，不改产品。

**Interfaces:** 消费本计划十三入口和保存契约，产出逐条基线、65+3精确删除集合及可复用真实库夹具。

- [ ] 对照真实路由/DTO/保存代码写契约，记录空数组、菜单版本、错误顺序和审计动作；不新增独立机器契约文件。
- [ ] 补 `Test身份十三入口契约与强制改密矩阵`：13项描述、五角色、强制改密admin本人PUT允许而viewer仍403，普通请求/事件流令牌区别。
- [ ] 补 `Test资料已提交后菜单读取失败保留事实`：注入保存后的菜单故障，响应500但真实资料Rev和一次审计已提交；不撤销原会话。
- [ ] 补 `Test审计导出后页失败不发送残缺下载`：500条后返回错误；无成功下载头、不是200部分CSV；原公式与所有页断言保留。
- [ ] VM运行 `go test ./internal/interfaces/console ./internal/infrastructure/persistence/pgstore ./cmd/lober -run 'Test.*(身份十三|资料已提交|审计导出后页|登录|并发改密|慢资料|契约用户|角色菜单|最后启用管理员|并发管理员)' -count=1`。纯已有行为补测预期通过，不人为制造红灯；真实缺陷另列失败证据及修复，不能悄悄改基线。
- [ ] 提交 `test(identity): pin authentication and audit compatibility`，独立审查通过后接下一项。

### Task 2：密码能力、会话及认证应用

**Files:** 新建credentials两实现及测试、identity的contract/service/access/login/sessions/views、port两文件、domain/access/{request,menus,password}.go及纯测试、archtest/credentials_rules_test.go；修改archtest的deps/four_layer/application规则以登记**精确credentials包**，原identity_rules中的随机ID规则不动。

**Interfaces:** 产出Access、Principal、SessionBook、UserView、SessionResult、Deps、NewService和两类密码能力；菜单目录与有效菜单读取由本项建立，Task4复用。

- [ ] 先写能力缺失红灯，在VM点名 `Test身份登录凭据复核后才发布会话`、`Test身份会话期限与随机令牌契约`、`Test凭据实现只接受精确密码能力`；反例必须先通过类型检查。
- [ ] 实现固定密码和令牌机制；原独立向量/随机盐/坏编码/无界工作量测试逐断言迁到合适归属。应用非法新口令测试保留，不能只测可用口令；原真实强度准备缓存不改。
- [ ] 实现同实例会话表、协调锁和Access；保留实际锁/时钟/校验顺序、假散列、错误分类与不可伪造快照。CurrentUser/Login复用唯一有效菜单解析，失败时不发布会话。
- [ ] 加通道控制的登录/重置顺序、过期边界、溢出、无效角色不清令牌、仓储故障不变401、全权限/菜单副本不共享可变切片断言；保留HTTP生产入口未切换。
- [ ] VM运行 `go test ./internal/domain/access ./internal/application/identity ./internal/infrastructure/credentials ./internal/archtest -count=1` 及对应包 `-race -timeout=20m`。真实密码向量必跑；只用替身验证调用顺序不能代替真实算法。
- [ ] 提交 `refactor(identity): define credential and session contracts`，独立审查；旧控制台许可此时不预先删除。

### Task 3：本人资料、改密和管理员账号操作

**Files:** 新建identity/{users,password,profile}.go与同名/并发测试，补contract.go的Accounts；复用原pgstore契约及admin_guard_test.go，不修改其保护政策。

**Interfaces:** 消费Task2同一Service和Principal，产出Accounts六方法；UserQuery/UserPage及四种命令按第3节定义。

- [ ] VM先跑新方法缺失红灯，覆盖拒绝后无仓储、散列和审计时钟副作用。
- [ ] 实现原基础检查、读取、散列、锁和写入顺序；本人改密锁内重读，管理员更新保留请求Rev，不自动刷新。Profile只能用认证旧快照改四项资料，保存后再读菜单。
- [ ] 保留 `Test并发改密不能让旧口令登录补发会话`、`Test并发本人改密不能覆盖管理员重置`、`Test慢资料请求不能用猜测新修订号恢复旧权限或凭据` 的真实数据库事实及屏障；新增应用侧同样边界，不用固定sleep证明竞争。
- [ ] 补 `Test身份写入审计失败不撤销会话`、`Test仅改角色下次请求生效但不清令牌`、`Test身份跨实例管理员移除仍由数据库保护`，检查用户行/Rev/审计/有效令牌，而非只断言error。保留最后管理员表锁和事务内重查。
- [ ] VM运行 `go test ./internal/application/identity ./internal/infrastructure/credentials ./internal/infrastructure/persistence/pgstore -count=1` 和对应race20m；不切生产登录、不开第二份生产会话。
- [ ] 提交 `refactor(identity): move account and password operations`，独立审查。

### Task 4：菜单操作及启动账号初始化

**Files:** 新建identity/{menus,bootstrap}.go和测试，补contract.go的Menus、Registry、Bootstrapper；扩展domain/access菜单纯规则，复用repository/role_menus及真实保存测试。

**Interfaces:** 消费Task2目录及有效菜单读取、Task3账号能力；Service至此完整实现网页Registry，Bootstrapper只给组装点。

- [ ] 在VM先跑ListRoleMenus/SaveRoleMenus/Initialise缺失红灯，不给Registry加返回“待实现”的方法。
- [ ] 实现菜单缺省/显式空、required、排序、首次与更新版本、直接调用权限；仍让数据库生成before/after审计。初始角色顺序、目录和响应空值不变。
- [ ] 实现原空库初始化规则，复用同一个密码能力和时钟；已有用户时不散列seed、不改用户、不撤销会话。保留空seed错误及并发唯一约束结果，不加新全局锁协议。
- [ ] 补 `Test菜单配置不提升接口权限`、`Test身份菜单首次竞争及审计回滚`、`Test身份初始化仅空库写入且不泄露种子口令`；复用真实库，验证先前资料提交后菜单失败的兼容边界。
- [ ] VM运行 `go test ./internal/application/identity ./internal/domain/access ./internal/infrastructure/persistence/pgstore -count=1` 及对应race20m。
- [ ] 提交 `refactor(identity): centralize role menus and bootstrap`，独立审查；生产切换仍留到Task5。

### Task 5：一次接通身份入口与两个任务接缝

**Files:** 修改console/{auth,password,profile,users,menus,perm,routes,server,errors,tasks,taskcontrol}.go及harness_test.go等相关测试，移除console/sessions.go；新建ports/audit.go、execution/control_intent.go及测试；修改cmd/{app,wire,wire_gen,taskcontrol,assembly_test}.go、archtest/machine_rules_test.go；repository/users.go仅更新会话归属注释。

**Interfaces:** console.Deps增加Identity identity.Registry，移除Users、RoleMenus、SessionTTL；同一Service同时供Registry和Bootstrapper。共同AuditIntent和AuditedControl按第2/3节，TaskControl只换审计参数。

- [ ] VM先跑 `Test身份接线共用会话与凭据协调`、`Test审计意图桥接不改变任务控制` 的新接线缺失红灯；先保护真实newApp的初始化、登录、重置、旧令牌拒绝及任务控制。
- [ ] 原始Bearer提取、JSON/路径解析和响应留网页；authenticate/authorize委托Identity，actorOf从Principal取得Actor。route新增identityOperation，默认General；userinfo/logout/password分别声明对应操作，PUT用户声明LegacySelfUpdate，TargetUsername来自路径；不改变公开RouteInfo/OpenAPI字段。受保护请求缺依赖时明确失败，不回退旧Users。仅生成OpenAPI/路由的构造测试无需执行身份操作，不以仓储回退支持它。
- [ ] 一次改11条身份入口，删除旧HashPassword/CheckPassword、业务PermsOf/AllPerms和网页持有的凭据锁/会话表；权限常量仅改从应用公开契约取得。harness原直接发会话的准备改用测试持有的同一SessionBook，不多加一次口令计算；保留真实强度缓存策略和独立随机盐测试。孤立处理器测试只注入可信ports.Actor，不新增能从JSON制造Principal的接口。
- [ ] taskStream循环只委托StreamAuthorized，仍以原token复查；审计意图由路由和可信Actor/Clock生成。execution适配器转原Audit并委托既有四方法；stub只改参数类型，保持不可用错误。不修改任务状态判定、事件发送或取消重试本体。
- [ ] App.Initialise保留内置准备顺序，只把用户初始化委托Bootstrapper。组装点由同一Clock、配置秒数和Tokens构造一个SessionBook，再构造一次Service；Registry/Bootstrapper绝不能各new一份。VM生成Wire并**等待取回完成**再冻结。
- [ ] VM运行 `go test ./internal/interfaces/console ./cmd/lober ./internal/application/identity ./internal/application/execution ./internal/archtest -count=1`、相同包race20m、`pnpm --dir web check:console-types`及Wire diff；检查全部正式路由，而非只新身份路由。补退出/到期/账号改变的真实网络事件流断言，保留原三个事件流原因用例。
- [ ] 只删实际消失的身份及三个接缝许可，逐项记账，不预填中间数。提交 `refactor(identity): route console through identity contracts` 并独立审查。

### Task 6：审计查询与完整导出应用

**Files:** 新建application/audit/{contract,reader,export}.go、audit/port/repository.go、domain/audit/export.go和测试；改console/audit.go、server.go、errors.go及cmd组装/Wire；更新repository/{audit,types}.go的实际审计归属和user/role目标注释；登记新领域包与纯标准库规则。

**Interfaces:** 产出Reader.List/ExportCSV和普通Filter/Query/Entry/Page；console.Deps将Audit repository.AuditLog改为AuditReader audit.Reader。Reader只消费ports.Actor，不导入identity具体服务。

- [ ] VM先跑新Reader缺失红灯及 `Test审计应用拒绝后不读仓储`、`Test审计应用导出后页失败无部分结果`。
- [ ] 列表和导出保留身份→范围→分页→仓储顺序；网页数字仍复用现有parseQueryPagination，只做语法解析，不走旧queryPagination仓储检查。应用用原CheckPageQuery，不重写分页/游标；范围非法400，非可存筛选仍按原查询返回空页。
- [ ] 导出在应用内使用同一内存缓冲与encoding/csv，逐页读取和编码，纯公式规则只留domain/audit一份。错误时返回nil结果，网页仅在成功后设置原下载头和200；JSON列表原文不转义。
- [ ] 保留 `Test审计分页过滤完整CSV及公式作为文本`、`Test审计CSV文本转义` 和五角色权限；补第2页故障/取消、Unicode Cf/全角前缀/原首控制字符、空结果只有表头、并发新增审计不倒灌旧游标。核对真实500条以上数据与每个字段，不只断言成功码。
- [ ] VM运行 `go test ./internal/application/audit ./internal/domain/audit ./internal/interfaces/console ./internal/infrastructure/persistence/pgstore ./cmd/lober -run 'Test.*(审计|Audit|CSV|身份接线|装配初始)' -count=1`及相同命中范围race20m；Wire生成/取回/diff、契约类型检查。未改前端源码时不安装Vue依赖。
- [ ] 删除剩余本阶段审计许可，更新契约，提交 `refactor(audit): move queries and complete export behind reader`，独立审查。

### Task 7：精确边界、完整验收和整分支审查

**Files:** 新建archtest/identity_audit_rules_test.go、`docs/superpowers/reviews/2026-10-24-ddd-phase5-evidence.md`；按实际职责改deps、application_rules、machine_rules、transition_uses、wizard_helpers、migration、transition_manifest、authoring_rules、media_rules、domain_rules、four_layer测试及路线/本计划。原identity_rules中的六项随机ID许可不变。

**Interfaces:** 承接完整Registry/Bootstrapper/Reader及共同AuditIntent；阶段5为0，阶段6为39；身份/审计新接口不能变成直连仓储的后门。

- [ ] 先写并在VM运行类型有效反例：网页新文件/本地接口/方法值/包别名反查Users或RoleMenus、构造身份Service/SessionBook或调用会话表方法、重写角色/菜单判断、直接算口令或读随机数；credentials持有Store/自己的Users能力、应用导入网页/其他应用具体服务或外部IO、领域读时钟/文件、cmd重建身份业务均拒绝。正例包含正常Registry、路由权限常量、DTO转换、Crypto能力、execution只转换意图。先真实类型检查，不能以装载失败冒充拦截。
- [ ] 核对68项删除集合：35+30阶段5及第2节三个明确阶段6条目；107→39、四表27/0/11/1。保留39项逐字段相同，零新增、零改阶段、零换名挪表；不能仅比较数量。
- [ ] 更新历史快照检查的**接续方式**：media_rules原154项删除数据和原四个起点摘要保持，用当前39项+本轮68项还原上轮107项，再加原154项还原261。原前三阶段/向导/镜像清零及新阶段5清零均保持；不能改原摘要来容忍未知变化。
- [ ] 冻结源码并在lober VM从头执行下节完整命令。逐轮保存树、环境、命令、时间、退出码、生成差异、缓存、明确skip和无匹配；不能把简略日志中的未知跳过总数写成0。
- [ ] 提交 `refactor(identity): enforce phase-five contracts` 供本项独立审查；全部七项逐项门禁闭环后，再按完整起点到最终候选做独立整分支审查，逐一覆盖十三入口和五项Review Focus。每项未判断范围由根说明处理，不默认通过。
- [ ] 两道收尾审查通过才标记模块可交付。文档收尾用Git精确差异证明执行代码仍等于被测树，不重复套件；保留模块分支、工作区、原日志和用户文件，不自动合入或推送。

## 5. lober VM执行与验收

1. 开始实施时只从指定私有文档读取必要连接资料，不输出内容。核实hostname=lober，工具版本、磁盘/内存、真实库lober_test未恢复及无竞争Go作业；不能复制第四阶段资源数当本次测量。
2. 建立新的第五阶段归属目录，例如 `/home/dev/lober-ddd-phase5-identity-audit-20261024`。只复用已有测试连接材料，不覆盖第四阶段目录、产品服务或其他人的缓存/日志；私有DSN忽略且600。
3. 传入固定候选/合并的完整Git树，逐路径核对；工作区含用户改动时从明确提交归档，不把未提交内容偷偷纳入验收。格式/Wire也只在VM，明确取回文件并等待完成后再冻结。
4. 一次只运行一个VM作业。失败先保存原日志/堆栈和安全事实，确定真实原因；保持未涉及问题的代码和测试设置，复跑原失败用例，再按原命令完成验收。真实缺陷先失败复现、单列修复及行为影响，不能因搬迁假定已修好。
5. 最终原命令如下；第一次实际执行前原样保存，后续失败复跑不删包、不改过滤、不改原20分钟竞态超时：

```sh
make check
pnpm --dir web check:console-types
go test ./internal/application/identity ./internal/application/audit ./internal/application/execution ./internal/domain/access ./internal/domain/audit ./internal/infrastructure/credentials ./internal/interfaces/console ./internal/infrastructure/persistence/pgstore ./cmd/lober -run 'Test.*(身份|登录|口令|改密|会话|用户|资料|菜单|审计|管理员|凭据|SSE|控制委托|装配初始)' -v -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
git diff --check
git diff --exit-code
```

6. make保留原全模块强制发现、普通测试、race20m、Linux amd64构建和Wire diff。额外点名命令证明新危险输入与真实数据库检查实际执行；统计实际命中、缓存和skip，不用旧阶段成功或缓存标记冒充重跑。
7. 本阶段不宣称实物镜像、实机安装、企业套件、浏览器人工验收或完整前端构建通过。后续合入目标是用户明确的dev；按仓库流程合并后仍在VM重跑，后续环境不随设计批准自动推进。

## 6. 计划自检与待确认边界

- 已逐条覆盖设计的四层方向、契约唯一来源、共享模型、审计原子性、模块接口、只读导出和依赖检查；没有引入通用事务或新登录方案。
- 已静态核对起点清单35+30+42=107及三个跨阶段接缝，目标27/0/11/1=39；这是计划清单核对，不是Go测试结果。
- 已核对接力：Task2 Access与私有会话→Task3 Accounts→Task4 Menus/完整Registry→Task5一次生产接线→Task6独立Reader→Task7门禁；没有要求实现者用未实现的方法撑编译或同时维护两份生产会话。
- 已核对所有新Go类型/接口的定义归属，Bootstrapper不交网页，Principal不暴露凭据，AuditIntent不是仓储别名；execution桥接只依赖共同ports。
- 已核对harness直接准备会话及seedPasswordHashes缓存：会话组件显式注入并只在测试夹具保留引用，不把所有准备替换成新增登录，不新增生产测试后门。
- 已将五项Review Focus和资料已提交后的晚失败落到具体任务与测试；旧并发/数据库/随机盐测试保留，不降低强度。
- 本计划的具体实施选择是：一次切身份接线、CSV仍完整缓冲、提前迁出三个接缝。用户审阅本计划后才实施；若不接受提前迁出的范围，应重排阶段，不能保留失效依赖而声称本阶段已清零。
