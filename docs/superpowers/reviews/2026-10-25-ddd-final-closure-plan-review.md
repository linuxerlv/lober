# 收尾实施计划独立审查

结论：**APPROVE**。严重0、重要0、轻微0。计划已足够开始逐项实施；本结论不代表实现通过，也不代表允许本轮开始实现。

受审计划：`E:/src/lober/.worktree/rbac-integration/docs/superpowers/plans/2026-10-25-backend-ddd-final-closure.md`。
源码起点：`3de1bcdd943ce680906e0a09e8afa107e75d789d`。只读核对HEAD与状态，确认原两项UI变化仍在，计划为唯一新增未跟踪文件。

## 审查方式

阅读已确认四层总设计、总审计、计划全文，并核对关键生产定义、调用方、测试源码和Makefile。另读私有execution-facts.md及selection-guard-facts.md作为定位材料，不把它们当成实施记录或已通过证据。

本次没有运行Go、gofmt、Node、构建、测试，没有连接VM，没有读取连接秘密，没有操作Git或修改业务文件。唯一写入是本报告。

## 为什么可以开工

| 核对项 | 计划与源码依据 | 判断 |
| --- | --- | --- |
| 六项审计覆盖与顺序 | 计划第35–48行逐项对应I1/I2/I3/M1/M2/M3，1→8；第20–29行锁定行为变化、职责和审查要求 | 0/3/3均有负责人和最终关闭条件，没有把旧套件绿当作关闭 |
| 契约唯一来源 | 第67–86行盘点真实方法文件，规定方法到唯一约定及八项各自来源；第223行最终复查新增能力 | 清单是导航，不复制第二套行为规范；不适用项也须说明 |
| 镜像取消 | 第90–101行区分两次读取边界的四个合成错误与两个真实父取消，额外覆盖OpenSource；源码import.go:101–119正是三处分支 | 返回原包装取消错误、完整状态/Rev与来源Error不变、关闭一次均能验收；不把父取消写failed作为既有事实 |
| 纯规则迁域 | 第107–142行给出完整域入口，包含Service/Authorize/control/Sweep；源码service.go与token.go的判断不止protocol.go | 不是只搬protocol；时钟位置、旧重复信号、Signal字段顺序与指纹、凭据机制和冻结选择均受保护 |
| 机器请求 | 第148–169行定义Gateway输入/结果、渲染小接口、来源错误类别及观察顺序；源码http.go的boot/installerBoot/file/signal可逐条对应 | 503/422/403/404/409来源明确；File先观察、Installer成功后观察、Signal独立观察验证和最终验证保留 |
| 日志收录 | 第175–182行定义接收器和小仓储能力；对应events/syslog.go:66起原流程 | 传输保留租约与字节处理，应用保留读事实/追加/成功通知；第180行明确不新增原子attempt承诺 |
| 模板候选接口 | 第189–198行给出新facts方法和旧ListTemplates(Selection)委托关系；repository.Templates、tasks/port.Templates、Store和wizard调用方均有明确位置 | 接口可以接上现有类型；不扩大authoring消费能力；替身不得靠nil嵌入运行 |
| 模板读取边界 | 第195–200行规定只读REPEATABLE READ、256模板批、1000精确Hook批、同一tx、只留批与页 | 不改Store.write；不调用公共GetHooks逃出事务；固定版本、最高审批不回退、游标与页码时序和代价均明确 |
| 守卫完整性 | 第207–217行覆盖所有文件和未调用声明、改名接口、函数值、字段、真实类型成功前提、合法输出正例、execution特殊layer | 对应wizard_helpers_test.go:26及:94的名单洞、machine_rules_test.go的宽许可、application_rules_test.go的模块遗漏，关闭路线具体 |
| 最终验收 | 第225–242行保留make check、类型生成检查、完整-v、前后同源证明及Skip/数据库诚实记录 | 与现Makefile和web/package.json中真实命令一致；无本机测试或既有PASS替代 |

## 可执行性与测试定位

计划列明的现存契约、应用、组装与守卫文件已抽查存在；新增域包、Gateway、日志入口、facts接口及其测试明确标新。共享文件按1→8顺序修改，避免Task7提前封死尚未迁移的生产路径。

下列点名既有测试确实存在：
- machine/http_test.go：TestCanonicalFrozenFileRouting、TestBoundedBinarySignalAndStrictQuery、TestBootServesPreseedAnswerToDebianInstallerAgent。
- cmd/lober/machine_delivery_test.go：TestLinuxMachineDeliversFrozenFilesAndCompletesProtocol。
- pgstore/pagination_test.go:179：TestNumberedTemplateEligibilityBeforePagination。
- console/selection_pagination_test.go、wizard_test.go、builtin_selection_test.go：计划点名的页码、缺失集合、冻结任务测试。
- archtest/deps_test.go:224：Test全模块依赖守卫。
- media/import_cancel_order_test.go：Test镜像打开有效额外源时取消仍返回取消。

Task6所需DBForTest在pgstore/export_test.go:15真实存在，可在测试中注册查询回调。Hook批读现GetHooks每批重新s.read，私有hookVersionsIn接收db，计划特意要求传同一tx，避免名义上事务一致、实际分批逃出。原分页Next由open.go:139的limit+1行为产生，计划保留；新测试也比较ID/Version/顺序/Next/Total。

新增测试不是只有名称：取消类断言errors.Is、状态/Rev/Error与清理；机器类断言调用顺序、状态码和正文泄出；候选类给出260/257/17等确定期望、第二批1001引用、并发屏障与取消不得半页；守卫类要求真实类型先成功，再给准确违规位置并保留合法输出。

未发现需要实施者猜定重大接口、必然导致编译失败或无法验收的缺口。局部帮助函数拆分、错误常量命名及测试夹具写法可在这些既定契约内决定，不构成阻断。

## 暂不判断的边界

- 尚无新实现，不能确认实际编译、测试通过、迁移后字节兼容或守卫没有误报。
- Task6读取量和只读快照持续时间要在实施后用真实VM结果判断；计划没有虚构性能收益或固定秒数。
- 不验证真实ISO、真实机器安装、企业测试、完整前端构建或人工验收，不评价用户原UI改动。
- 不验证跨实例删除闸门、跨页快照、消息必达、持久会话或跨实例立即撤权；计划已把这些排除，不能将它们记为通过。
- 不授权合入dev、推送或晋级test/preview/main；用户审阅计划后才可按约定启动实现。


## 局部复核附记：Task3接口精度补充

结论仍为 **APPROVE，严重0、重要0、轻微0**。本次仅复核计划第118–139行的新补充，没有重审全文或扩大授权。

- 新InstallAccessNeedsTime与InstallAccessAllowed共享Deadline谓词。应用先完成原凭据验证，只有前者为真才读取原额外时钟，否则传不会使用的0。这与token.go:34–40的原先后关系一致，也避免将Deadline>0判断留在应用层。
- StartInstalling的bool明确为是否改变，Timeout的bool明确为本次已超时。StartInstalling只消费现有GetAuthorized保证的非nil执行记录，InstallingNeeded共享同一幂等判断。接口没有新增隐含状态修复或时钟读取。
- SignalAccessAllowed在原凭据验证后执行，对应service.go:137–144的原顺序；收尾及精确重报例外继续由原域推进规则保护。

这些补充消除接口解释空间，没有改变其余任务或测试要求。本次仍未运行测试、未连接VM、未修改DEV；只追加本报告。
