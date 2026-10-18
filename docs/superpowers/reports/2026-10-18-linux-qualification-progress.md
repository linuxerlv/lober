# Linux 装机验收进度（未完成）

更新时间：2026-10-18 20:33，Asia/Shanghai。所有构建和测试均在指定测试 VM `192.168.137.21` 执行。

环境恢复更新：用户恢复 PVE 后，已重新连接并确认 wbt_lat_usec 为原值 75000；测试盘完成 ext4 日志恢复并正常挂载，PostgreSQL、原有 DHCP/nginx 正常。没有再次更改宿主机参数；其他介质下载仍暂停。首次仓库发布修复和补充审查修复的完整 make check 均通过，最终产品提交为 d3014e535f1f2a3366cc726b5f30247bd750c8a9，已按 dev → test 集成。291 个后端源码/模块文件逐一匹配 VM 已验证输入，证据见 reviewed-fix/dev-integration.json。新的 test 候选正在准备，旧候选结果不能替代新候选企业回归。

## 已冻结并集成的候选

- 产品 / dev：`0a9b327475139e2eab7292ed609c403abdc8d448`。
- test：`e4d105951ec64bf025bc804e4152616242b43c2d`，由 dev 合入，产品文件一致。
- 原始 test Git archive SHA256：`7ae41fd4b5c401caf629ae3ee7c92068ad2cba43f78195f7f5f3f4e1a68b35c3`。
- 实际候选二进制 SHA256：`970c01038496741045e6af0fee17ff0b3a180147dbb77a2a8cd664fd8b511ba3`。
- 从同一 test 源码构建的真实前端包含 213 个文件，清单 SHA256：`c43f495a3f9bbcb609e3cbdefb8b6d576f42a6e411ece2b759f510d34783f2d2`。
- VM 构建清单：`/home/dev/lober-linux-20261018/candidate.json`，包含源码、实际生成资源和二进制绑定。不得用其他候选的旧 PASS 替代。

## 验证结果与界限

| 范围 | 结果 | 证据 / 限制 |
| --- | --- | --- |
| 集成前完整后端门禁 | PASS | `logs/make-check.log`，普通测试、race、vet、格式、构建、Wire 均通过；这是 0a9b327 的结果 |
| 从实际 test 源码重跑前端 | PASS | `logs/test-frontend.log` / `.exit=0`；26 文件、91 测试，11 个构建任务通过 |
| 嵌入真实前端后的 Go 检查与构建 | PASS | `logs/test-build.log`；cmd/lober 8.192s，internal/console 213.635s，构建成功 |
| 企业门禁自检 gate | PASS | `enterprise-candidate-1/gate.log`；30 项 |
| 企业控制面 control | PASS | `enterprise-candidate-1/control.log`；88 项 |
| 企业故障恢复 recovery | PASS | `enterprise-candidate-1/recovery.log`；5 项，包含真实 TCP 超时、ENOSPC、进程崩溃、数据库连接中断 |
| 浏览器 flows | FAIL，需重跑 | 4 项中 1 项失败；截图写入/等待超时，发生于存储阻塞期间。其余成功不能替代整个套件通过 |
| 事件 events | FAIL，需重跑 | 服务启动未在 15 秒内就绪，0 项收集。不得当作 skip 或 PASS |
| 依赖专项 / 官方 npm 审计 | PASS | 16 项真实消费者回归；审计所有级别 0。braces 使用修复分支，仍需跟踪其维护风险 |
| Debian 补充仓库发布修复 | 定向 PASS；完整验证待重跑 | `logs/debian-repo-red.log` → `debian-repo-green.log`。5 个相关包通过，完整 Go 回归被连接中断打断 |
| 实际 Linux 安装 | **尚无 PASS** | Debian、Ubuntu、Leap15、Leap16、AlmaLinux、Rocky 均未完成安装验收 |

以上日志根目录均为测试 VM 的 `/home/dev/lober-linux-20261018/`；依赖日志另在 `/home/dev/lober-linux-deps-20261018/`。失联前未全部导回本地，恢复后需校验原始退出码和日志哈希。

## 本轮发现及修复

使用真实 Debian 13.7 netinst + 同版 mini.iso 调用真实导入 API，结果被标为 ready，但制品只有 kernel/initrd，没有 repo。内置 preseed 引用了 `sys:repo`，因此原来的导入测试无法证明内置模板可交付。

在独立产品工作树补充了主介质仓库发布、Debian 必需制品 repo，以及包文件/Release 文件保留回归。先在 VM 上得到失败：`ready Debian ISO has no HTTP repository for the shipped preseed`；修复后 iso、catalog、admission、cmd/lober、machine 共 5 包通过，再完成完整回归。最终补充修复已提交并推进 dev/test。

真实 netinst 缺少内置安装需要的 curl 包，故只用来复现导入问题，已通过产品 API 删除该独占探针。实际 Debian 装机使用官方 DVD-1 + 同版 mini，下载后必须校验 SHA256 并确认仓库包可用。

补充独立审查指出 4 项 Important，已逐项核对并作一次修复：旧 ready Debian 记录缺 repo 时拒绝新任务；从真实目录发现 suite 并要求 Release、匹配架构的包索引和 pool 包；负例核对冻结 hook 的 RC41，retry 校验实际 HTTP422 及 task_retry_after_reboot；重复引导强制 net0 优先，并捕获真实网关 local boot 响应、比较根文件系统 UUID。新增 Go 回归已观察 RED→GREEN，最终 make check 退出码为 0。

`tests/lobertest/enterprise/linux_install.py` 是本轮新增的装机脚本：独占 VM/磁盘/MAC 校验、两人审批派生模板与必需 hook、真实向导建任务、PVE 启动、服务端终态、SSH、guest agent 的 root 检查、凭据清理及重复磁盘启动。指定 VM 的语法检查与 2 项防误判检查通过；实际装机流程尚未运行，不能作为装机通过证据。

## 环境中断与恢复记录

1. PVE 底层 `/dev/sda` 为 USB UAS 设备；内核日志出现写命令超时和 USB 设备重置。thin pool 数据约 21.93%、元数据约 1.78%，内存可用约 5.4 GiB，未发现容量耗尽。
2. 即使单连接限速下载，VM I/O pressure 仍曾接近 100%；数据库与浏览器测试受到影响。已暂停本轮相关下载，旧并发进程已退出。
3. 经用户明确批准，对 PVE `/sys/block/sda/queue/wbt_lat_usec` 从 75000 临时改为 0，预先设置 180 秒自动恢复任务 `lober-linux-wbt-restore-20261018.timer`；诊断程序也设置 finally 恢复。
4. 诊断约一分钟时 PVE SSH 连接被重置，随后宿主机与测试 VM 失联。用户恢复 PVE 后已确认参数为 75000；无法证明此前自动恢复任务是否在重启前执行。**失联原因尚未确认。** 不把暂时改善当作根因已解决。
5. 没有启动 ESXi / Windows，没有启动专用装机 VM102，也没有重启 PVE 或现有 QA VM。

## 恢复后的顺序

1. 先确认 PVE 本机状态、内核磁盘日志、原参数恢复为 75000、自动恢复任务状态；在确认存储可靠前不扩大并发。
2. 恢复测试 VM，检查 PostgreSQL、独占数据盘挂载/标记、源文件及候选哈希；保留现有 QA 服务。若有重启，重新核对挂载，刷新 PVE ticket。
3. 导回已有原始证据；重跑未完成的 Go 检查及受环境影响的 flows/events，不能复用失败日志或旧 PASS。
4. 完成 Debian 补充修复验证后，产品提交 → dev 集成验证 → test 合入，再冻结新候选。重跑受候选变更影响的企业套件。
5. 校验真实介质，应用已验证的独占 MAC DHCP/NAT 配置，配置并启动 VM102；逐个执行 Debian、Ubuntu、Leap15/16、AlmaLinux、Rocky 实际装机和重复磁盘启动。
6. 容量长稳、升级等未执行项目继续标记未完成。Linux 结果汇报后，由用户确认是否继续 ESXi7/8 与 Windows Server2022。
