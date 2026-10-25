# 身份与审计兼容约定

本文件固定第五阶段搬迁前的行为。正式路由描述仍是对外入口的来源。
这里不新增权限、密码要求、持久会话或独立写审计能力。

## 十三条入口

路径均以 `/console/v1` 开头。所有 JSON 响应沿用 `code/data/message`。
“本人”来自当前请求认证得到的账号，不采信正文中的用户名。

| 方法与路径 | 身份与输入 | 返回与错误先后 | 保存、会话与审计 |
| --- | --- | --- | --- |
| POST `/auth/login` | 唯一公开入口；username、password 必填，不清理空白、不改大小写 | 解析→读账号→真或假散列校验→读锁内重读凭据→菜单→会话；返回 token、expiresAt、user。账号不存在、口令错、禁用、角色无效均为相同401 | 不写数据库、不写审计；最终确认到发布会话持读锁。重复成功登录产生独立会话 |
| POST `/auth/logout` | 已登录；强制改密时可用 | 200，data 为 null；已删除的令牌再请求401 | 只删除本令牌，不写审计，不引入事务 |
| POST `/auth/password` | 本人；currentPassword、newPassword 必填；强制改密时可用 | 当前口令错403，随后才检查新旧相同或新口令不可保存400；成功200 | 散列在写锁外计算；锁内复查账号、版本、散列、原令牌；仅更换密码并取消强制改密。成功保存及 `user.password.change` 审计后才撤销本用户全部会话；失败不撤销 |
| GET `/user/info` | 已登录；强制改密时可用 | 认证快照加本次有效菜单；菜单读取失败500 | 无写入、无审计 |
| PUT `/user/profile` | 本人；displayName、email、avatar、bio、rev；强制改密时403 | 先 CheckProfile，再比请求版本与认证快照版本；版本不同409；成功返回带有效菜单的用户 | 只改四项资料，资料与 `user.profile.update` 同事务。保存后菜单读取失败仍返回500，但新版本与一次审计已经提交；保留原会话，旧版本重试409 |
| GET `/users` | 仅管理员；page/page_size 或 cursor/limit | 用户名按数据库 C 排序；items/next/可选total。每人 menuIds 为非 null 空数组、menuRev 为0 | 不为每人查菜单，无写入、无审计 |
| POST `/users` | 仅管理员；username、role、password 必填，displayName、disabled 可选 | 基本账号/角色/口令检查400→散列→保存校验资料；成功返回基本用户，菜单空数组及版本0 | 创建版本1，账号及 `user.create` 同事务；CreatedAt 与审计 At 分别读时钟。重复账号冲突409 |
| PUT `/users/:username` | 仅管理员；role、rev 必填；username、displayName、password、disabled | Rev/角色/正文账号不匹配400先于自禁用409；随后读取目标。初始密码必须真正更换，否则409；原请求Rev交给保存层；成功返回基本用户 | `user.update` 与账号同事务；非空密码或禁用成功后才撤销该用户全部会话；仅改角色不主动清会话。强制改密时仅管理员更新本人可用，viewer仍403 |
| DELETE `/users/:username` | 仅管理员；实际禁用账号 | 自禁用409先于读取目标；不存在404；成功200、data 为 null | 保留账号行；`user.disable` 同事务；成功后撤销目标全部会话，失败保留 |
| GET `/roles/menus` | 仅管理员 | menus 保留目录顺序；roles 为 viewer/operator/hook-author/hook-approver/admin | 无写入、无审计 |
| PUT `/roles/:role/menus` | 仅管理员；menuIds、rev 必须给出；显式 [] 有效，null/遗漏无效 | 角色/版本/重复/可用页面/必需页面依次检查400；保存按目录排序；陈旧版本或首次保存竞争409 | 配置与 `role.menus.update` 同事务；数据库生成 before/after 审计详情；Rev=0只表示首次保存 |
| GET `/audit` | 仅管理员；target_kind、target_id、actor、action、from、to，加分页 | 先 from/to 后分页检查；时间含两端，文本精确匹配，ID降序；items/next/可选total | 只读，不为读取增加审计 |
| GET `/audit/export` | 仅管理员；同样过滤条件，不接收分页控制 | 每页500条，游标读到结束；全部成功后才设置下载头及200；后页失败返回错误，不能输出半份文件 | 只读；完整内存缓冲，无新增条数上限；不为导出增加审计 |

普通错误保持400、401、403、404、409、422、500及原中文说明。
最后一位启用管理员不能被禁用或降级，ErrLastAdmin 仍是409。
保存层的 ProblemsError 仍返回422及问题位置；不通过匹配错误文字作判断。

## 返回字段、菜单与权限

用户字段保持 username、displayName、email、avatar、bio、role、permissions、disabled、createdAt、rev、passwordChangeRequired、menuIds、menuRev。
不得输出 password 或散列。登录、本人信息、更新资料才读取有效菜单；创建、更新、列表只返回基本用户。
列表的 items 和用户 menuIds 保持非 null 数组；游标 next 为空字符串表示结束，total 仅在页码查询时提供。

五角色及16项实际权限不变，`@any` 只是已登录标记。
所有角色都有 host:read、iso:read、hook:read、template:read、task:read。
operator 增加 host:write、task:create、task:cancel、task:retry；hook-author 增加 hook:write、template:write；hook-approver 增加 hook:approve、template:approve。
admin 拥有全部权限，另外包括 iso:write、user:manage、audit:read。隐藏菜单不会改变接口权限。

只授予目录中的二级页面。未配置时取全部可用页面；显式空配置返回空结果，不恢复默认。
admin 的 role-menus 页面必需。读取时重新按当前权限和目录顺序过滤，保存不能授予不存在或不可用的页面。
菜单未配置时版本0；保存后版本递增。保存空数组与没有配置不同，管理员遗漏必需页面返回400。

## 认证、并发与取消

1. Authorization 优先，格式错误不回退到查询令牌。只有标记为事件流的 GET 接受 access_token，普通请求不接受。
2. 每次认证先查本实例会话，再读最新账号。账号缺失或禁用时删除该令牌；无效角色只拒绝；数据库故障保留原错误。
3. 强制改密先于普通权限检查。只允许本人信息、退出、本人改密，以及管理员本人的旧 PUT 用户入口。该例外不给普通用户管理权限。
4. 默认会话43200秒，0取默认，负值构造失败。expires <= now 即到期；负时钟和加法溢出饱和到 MaxInt64。不存在令牌不额外读时钟；发布令牌时按原顺序清理过期记录。
5. 会话与凭据协调锁共属一个实例。登录最终复查到发布持读锁；重置、禁用提交到撤销会话持写锁。密码计算在锁外。资料修改仍依赖认证快照与数据库版本。
6. 慢登录不得在重置或改密成功后发布旧密码会话；慢资料请求不得猜测新版本覆盖安全字段。并发移除两个管理员时数据库至少保留一位启用管理员。
7. 请求取消继续经 context 传给原数据库操作。保存失败不撤销会话；提交成功与撤销会话之间不增加取消检查。业务与审计同事务，不新增通知或必达事件。
8. 长连接复查先令牌后账号，再检查禁用、强制改密、角色和 task:read。账号异常只结束流检查，不增加删令牌副作用；事件分页、5秒轮询及发送顺序不变。

密码格式保留 `pbkdf2-sha256$600000$…`，16字节随机盐、32字节结果、固定600000轮校验、常量时间比较。
新密码唯一文本条件是非空且可保存；登录不提前套用新密码规则。不存在账号仍做固定假散列校验。
令牌使用32字节随机数，结果为64位小写十六进制。测试继续使用原完整强度散列缓存，不增加真实登录来替代夹具的直接发会话。

## 保存约定与初始化

Users 只保留 CreateUser、UpdateUser、GetUser、ListUsers；RoleMenus 只保留 GetRoleMenus、SaveRoleMenus；AuditLog 只读 ListAudit。
不开放通用事务回调，不把事务对象交给上层，不提供单独写审计的方法。
用户创建和更新先检查审计，再检查用户。更新锁定用户表后读取当前行，检查原Rev，再检查最后管理员；账号标识及创建时间不可改。
业务数据及审计一起提交或回滚；更新版本只由保存层递增，不能在上层用最新版本替换请求原版本。

角色菜单首次保存使用唯一约束，两个Rev=0请求只能一个成功；已有配置在行锁中比版本。
首次审计 before 为 null，after 为保存数组；以后 before 是之前数组；审计失败回滚配置。
重复相同内容仍按版本保存，旧版本重试冲突，不新增隐式去重。

启动初始化在内置内容准备后查询 ListUsers(Limit=1)。已有账号忽略 seedAdmin；空库必须显式提供它。
创建 admin/“管理员”/强制改密账号，Actor=bootstrap、Action=user.create；创建和审计各读一次时钟。
并发首次初始化服从唯一约束，不新增分布式初始化流程。

## 审计文件格式

列顺序固定 `id,at,actor,action,target_kind,target_id,detail`。使用原 encoding/csv、LF 换行，无字节顺序标记。
仅为判断首个有效字符跳过 Unicode 空白及 Cf 格式字符。遇 `=+-@＝＋－＠`，或原首字符为 tab/CR/LF，在原文前加 tab。
不得清理原文。普通列表保留原文。成功导出保留全部页和降序，后页故障不得发送下载头、200或残缺内容。

## 基线验证

新增 `Test身份十三入口契约与强制改密矩阵` 固定十三项路由描述、未登录、五角色、强制改密的错误先后及管理员本人更新成功。
它还检查普通请求与事件流的查询令牌区别、Authorization 优先及坏格式不回退。
新增 `Test资料已提交后菜单读取失败保留事实` 检查真实资料、新Rev、一次审计、原会话及旧版本重试。
新增 `Test审计导出后页失败不发送残缺下载` 使用真实501条审计，第一批读取500条后注入故障，检查游标、过滤条件与完整错误响应。

原登录、并发改密、慢资料、用户保存、角色菜单、最后管理员及并发管理员测试继续使用真实数据库。
原 `Test审计分页过滤完整CSV及公式作为文本`、`Test审计CSV文本转义` 保留全部页及公式安全断言。
这些测试固定已有行为，本项不修改产品代码，也不人为制造失败。

## 精确删除清单（待后续实现，当前不删除）

下面只记录起点事实，不作为代码访问许可。四张现有清单依次为 migrationExceptions、mediaMigrationExceptions、consoleRuleMigrationExceptions、notificationMigrationExceptions。
起点64/0/42/1，共107条。本阶段删除原第五阶段65条，以及原第六阶段的3条接缝，合计68条。
最终应为27/0/11/1，共39条；剩余第六阶段条目的每个字段必须与起点相同，不能移动阶段凑数。

字段依次为清单、调用包、文件、函数、目标包、符号、原清除阶段。包名全部写完整，便于逐条对照。

| 清单 | 调用包 | 文件 | 函数 | 目标包 | 符号 | 原阶段 |
| --- | --- | --- | --- | --- | --- | --- |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/menus.go` | `(*server).roleMenus` | `github.com/lober/lober/internal/domain/repository` | `RoleMenus.GetRoleMenus` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/menus.go` | `(*server).saveRoleMenus` | `github.com/lober/lober/internal/domain/repository` | `CheckRoleMenus` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/menus.go` | `(*server).saveRoleMenus` | `github.com/lober/lober/internal/domain/repository` | `RoleMenuSettings` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/menus.go` | `(*server).saveRoleMenus` | `github.com/lober/lober/internal/domain/repository` | `RoleMenus.SaveRoleMenus` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/password.go` | `(*server).changePassword` | `github.com/lober/lober/internal/domain/repository` | `ErrConflict` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/password.go` | `(*server).changePassword` | `github.com/lober/lober/internal/domain/repository` | `Users.GetUser` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/password.go` | `(*server).changePassword` | `github.com/lober/lober/internal/domain/repository` | `Users.UpdateUser` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/profile.go` | `(*server).updateProfile` | `github.com/lober/lober/internal/domain/repository` | `CheckProfile` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/profile.go` | `(*server).updateProfile` | `github.com/lober/lober/internal/domain/repository` | `ErrConflict` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/profile.go` | `(*server).updateProfile` | `github.com/lober/lober/internal/domain/repository` | `Users.UpdateUser` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/server.go` | `package-init` | `github.com/lober/lober/internal/domain/repository` | `RoleMenus` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/audit.go` | `(*server).exportAudit` | `github.com/lober/lober/internal/domain/repository` | `AuditEntry` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/audit.go` | `(*server).exportAudit` | `github.com/lober/lober/internal/domain/repository` | `AuditLog.ListAudit` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/audit.go` | `(*server).exportAudit` | `github.com/lober/lober/internal/domain/repository` | `Page` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/audit.go` | `(*server).listAudit` | `github.com/lober/lober/internal/domain/repository` | `AuditEntry` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/audit.go` | `(*server).listAudit` | `github.com/lober/lober/internal/domain/repository` | `AuditLog.ListAudit` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/audit.go` | `(*server).listAudit` | `github.com/lober/lober/internal/domain/repository` | `Page` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/audit.go` | `auditFilters` | `github.com/lober/lober/internal/domain/repository` | `AuditQuery` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/audit.go` | `toAuditItem` | `github.com/lober/lober/internal/domain/repository` | `AuditEntry` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/auth.go` | `(*server).login` | `github.com/lober/lober/internal/domain/repository` | `ErrNotFound` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/auth.go` | `(*server).login` | `github.com/lober/lober/internal/domain/repository` | `Users.GetUser` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/auth.go` | `HashPassword` | `github.com/lober/lober/internal/domain/repository` | `ErrInvalid` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/server.go` | `(*server).auditFrom` | `github.com/lober/lober/internal/domain/repository` | `Audit` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/server.go` | `(*server).authenticate` | `github.com/lober/lober/internal/domain/repository` | `ErrNotFound` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/server.go` | `(*server).authenticate` | `github.com/lober/lober/internal/domain/repository` | `Users.GetUser` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/server.go` | `package-init` | `github.com/lober/lober/internal/domain/repository` | `AuditLog` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/server.go` | `package-init` | `github.com/lober/lober/internal/domain/repository` | `Users` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/taskcontrol.go` | `package-init` | `github.com/lober/lober/internal/domain/repository` | `Audit` | `6` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/tasks.go` | `(*server).streamAuthorized` | `github.com/lober/lober/internal/domain/repository` | `Users.GetUser` | `6` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/users.go` | `(*server).createUser` | `github.com/lober/lober/internal/domain/repository` | `Users.CreateUser` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/users.go` | `(*server).disableUser` | `github.com/lober/lober/internal/domain/repository` | `Users.GetUser` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/users.go` | `(*server).disableUser` | `github.com/lober/lober/internal/domain/repository` | `Users.UpdateUser` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/users.go` | `(*server).listUsers` | `github.com/lober/lober/internal/domain/repository` | `Page` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/users.go` | `(*server).listUsers` | `github.com/lober/lober/internal/domain/repository` | `UserQuery` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/users.go` | `(*server).listUsers` | `github.com/lober/lober/internal/domain/repository` | `Users.ListUsers` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/users.go` | `(*server).updateUser` | `github.com/lober/lober/internal/domain/repository` | `Users.GetUser` | `5` |
| migrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/users.go` | `(*server).updateUser` | `github.com/lober/lober/internal/domain/repository` | `Users.UpdateUser` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/menus.go` | `(*server).listRoleMenus` | `github.com/lober/lober/internal/domain/model` | `Roles` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/menus.go` | `(*server).saveRoleMenus` | `github.com/lober/lober/internal/domain/model` | `Role.Valid` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/password.go` | `(*server).changePassword` | `github.com/lober/lober/internal/domain/model` | `Role.Valid` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/password.go` | `(*server).changePassword` | `github.com/lober/lober/internal/domain/model` | `StorableText` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/auth.go` | `HashPassword` | `github.com/lober/lober/internal/domain/model` | `StorableText` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/auth.go` | `(*server).login` | `github.com/lober/lober/internal/domain/model` | `Role.Valid` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermAnyUser` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermAuditRead` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermHookApprove` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermHookRead` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermHookWrite` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermHostRead` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermHostWrite` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermISORead` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermISOWrite` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermTaskCancel` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermTaskCreate` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermTaskRead` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermTaskRetry` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermTemplateApprove` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermTemplateRead` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermTemplateWrite` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `package-init` | `github.com/lober/lober/internal/domain/access` | `PermUserManage` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `AllPerms` | `github.com/lober/lober/internal/domain/access` | `AllPerms` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/perm.go` | `PermsOf` | `github.com/lober/lober/internal/domain/access` | `PermsOf` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/server.go` | `(*server).authenticate` | `github.com/lober/lober/internal/domain/model` | `Role.Valid` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/tasks.go` | `(*server).streamAuthorized` | `github.com/lober/lober/internal/domain/model` | `Role.Valid` | `6` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/users.go` | `(*server).createUser` | `github.com/lober/lober/internal/domain/model` | `Role.Valid` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/users.go` | `(*server).createUser` | `github.com/lober/lober/internal/domain/model` | `StorableText` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/users.go` | `(*server).createUser` | `github.com/lober/lober/internal/domain/model` | `ValidIdent` | `5` |
| consoleRuleMigrationExceptions | `github.com/lober/lober/internal/interfaces/console` | `internal/interfaces/console/users.go` | `(*server).updateUser` | `github.com/lober/lober/internal/domain/model` | `Role.Valid` | `5` |

## 审计读取共同边界

`audit.Reader` 先用可信 Actor 检查管理员读取权限，身份、改密、权限错误先于范围、分页和仓储错误。过滤文本不裁剪，时间含两端；应用不新增范围或条数限制。List 沿用游标默认100、页码默认20、最多500；空 Items 为非 nil 数组，next 末页为空串，total 仅页码存在。

List 和 ExportCSV 只读，不写业务或审计，不发通知。因此没有原子写入、提交复查、版本冲突或写入重试。重复读取可看到新增事实；游标后续页只读更小 ID，但不承诺跨页数据库快照。上下文交给原仓储；ExportCSV 每页读取后与最终交付前还检查取消，失败不返回部分文件，List 不另增取消检查。

## 身份纯值与凭据能力

PublicRoles 与 Principal.Actor 的各自方法说明是唯一约定：二者没有输入校验错误，不查库、不写入、不通知、不涉及事务、版本冲突、提交复查、取消或超时。PublicRoles 每次返回原顺序的独立角色切片；Actor 返回普通快照值，零值 Principal 得到零 Actor。调用方仍须逐请求 Authenticate/Authorize，不能把 Actor 当作新认证。

Passwords 的三个方法及 Tokens.NewToken 仅供可信身份应用或初始化调用。输入、错误和密码强度沿用方法旁既有说明及上文“认证、并发与取消”。不保存账号、会话或审计，不发通知，没有跨数据事务、提交复查或自动重试。Hash/NewToken 每次随机生成，故重复结果不固定；Check/CheckDummy 对同样输入按固定规则计算。调用时不修改输入，无共享账号并发事实，不接收上下文，不增加取消或超时。
