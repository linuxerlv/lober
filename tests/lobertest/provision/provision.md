# VM-S provision

重建 lobertest 项目要用的服务端虚拟机（VM-S），使其达到：dnsmasq /
nginx / postgresql / lober 四个服务 `systemctl is-active` 均为
`active`，且 `lober -config /etc/lober/lober.yaml -check` 退出 0。

本文档记录的是**实际做过、实测验证过**的 provision 过程——不是照抄
`deploy/` 下的生产模板，是在这台只有一张网卡的测试 VM 上，把生产模板
按需简化到能跑起来的版本。做了什么简化、为什么，写在下面「与生产模板
的偏离」一节。

## 一键跑

```bash
cd tests/lobertest
bash provision/provision.sh
```

幂等——已经是期望状态的机器上重跑不会有副作用，每一步先检查当前状态，
已经对的就跳过。也可以在服务处于中间破损状态（比如上一轮部署到一半
失败）时重跑来收敛到健康状态。

前提：Windows 宿主已装好 QEMU / uv / Go 1.27.1；
`D:/qemu-vm/debian-cloud/` 下已有 `base.qcow2` + `disk.qcow2`（overlay）
+ `start-vm.sh` + `id_ed25519`（VM-S 自身登录密钥）；guest 磁盘上已经
`apt install` 过 `dnsmasq`/`nginx`/`postgresql`（这一层是这台 VM 磁盘
本身的历史状态，本脚本不负责从裸 cloud-init 镜像走到"已装包"这一步）。

跑完之后用 `uv run pytest -m needs_vm -v` 验证。

## 与生产模板的偏离

三处偏离都来自同一个根本原因：**这台 VM-S 只有一张网卡**（QEMU SLIRP，
`10.10.10.0/24`，guest 固定 `10.10.10.10`），而 `deploy/` 下的生产模板
假设两张网卡（装机 VLAN + 管理网）。起真实目标机、配第二张网卡是任务 9
（L2 装机层）的范围，本次 provision 只搭 VM-S，不涉及。

1. **nginx**：机器面与人面共享同一个 IP（`10.10.10.10`），靠端口区分
   （机器面 80，人面 8090），不是生产模板假设的"两张网卡各一个 IP"。
   人面改成明文 HTTP（生产模板默认 HTTPS + 自建 CA）——理由与偏离细节
   见 `provision/nginx-lober.conf` 文件头注释。

2. **dnsmasq**：不提供真实的 DHCP/TFTP/PXE 服务，只让服务本身能干净
   启动（`port=0` 关 DNS，不声明任何 `dhcp-range`）。装机 VLAN 在这台
   VM-S 上不存在，没有对象可服务——理由见 `provision/dnsmasq-lober.conf`
   文件头注释。下一个人要验证真实 PXE 链路，需要给 QEMU 加第二张网卡
   并换回生产模板渲染出的完整版本。

3. **lober 配置**（`lobertest.config`/`deploy.render_config`）：
   `gateway_base`/`artifact_base` 用 IP 而不是域名（这台 VM-S 没有对外
   DNS）；`console_base_url` 默认值改成 `http://127.0.0.1:8090/console/v1`
   （见下方"config.py 改了什么"一节的详细说明）。

## 配置文件放在哪

| 文件 | 用途 |
|---|---|
| `provision.sh` | 一键幂等 provision 脚本，本文档描述的就是它 |
| `nginx-lober.conf` | 渡自 `deploy/nginx/lober.conf` 的实测版，装到 `/etc/nginx/sites-available/lober` |
| `dnsmasq-lober.conf` | 渡自 `deploy/dnsmasq/lober.conf` 的实测版（简化为仅保活），装到 `/etc/dnsmasq.d/lober.conf` |
（目标机 root 密钥不在本目录：见下方「目标机 root 密钥」一节。）

## 目标机 root 密钥

`gateway.root_ssh_public_keys` 要填一把公钥（`config.go` 的 `Validate` 拒绝空列表）。
这把密钥对**不放仓库里**，在 `D:/qemu-vm/lobertest-keys/`：

| 文件 | 用途 |
|---|---|
| `lobertest-target-root_ed25519` | 私钥。任务 9 用它 SSH 登进装好的目标机（L2 验收证据之二） |
| `lobertest-target-root_ed25519.pub` | 公钥。内容已内联进 `config.TEST_ROOT_SSH_PUBLIC_KEY` |

私钥路径由 `config.target_root_key_path` 引用，与 `ssh_key_path` 指向 VM-S 自身
密钥是同一个惯例。

**为什么不提交私钥**（最初的版本提交了，已改）：这把公钥会被注入为**每一台**经本
测试床装出来的机器的 root 授权。lober 本身就是裸机装机系统，测试配置有变成生产
配置的倾向；私钥一旦进 git 历史极难清除，而这个做法会被后来者照抄。「这只是临时
测试机」不是把私钥写进版本库的理由——放仓库外一样能用，代价是零。
`tests/lobertest/.gitignore` 里加了 `*_ed25519`、`*.pem`、`*.key` 等兜底规则。

密钥丢了就重新生成一对，把新公钥更新进 `config.TEST_ROOT_SSH_PUBLIC_KEY`：

```bash
ssh-keygen -t ed25519 -N '' -C lobertest-target-root \
  -f /d/qemu-vm/lobertest-keys/lobertest-target-root_ed25519
```

## VM-S 网络布局实测值

| 项目 | 值 | 来源 |
|---|---|---|
| Windows 宿主 → VM-S SSH | `127.0.0.1:2222` → guest `10.10.10.10:22` | `start-vm.sh` 既有 hostfwd |
| Windows 宿主 → VM-S 人面（console） | `127.0.0.1:8090` → guest `10.10.10.10:8090` | `start-vm.sh` 第二条 hostfwd |
| Windows 宿主 → VM-S 机器面（gateway） | `127.0.0.1:8080` → guest `10.10.10.10:80` | `start-vm.sh` **第三条 hostfwd，2026-09-26 任务 8 新增** |
| guest 内 nginx 机器面 | `10.10.10.10:80` → 反代 `127.0.0.1:8180`（lober gateway listener） | `nginx-lober.conf` |
| guest 内 nginx 人面 | `10.10.10.10:8090` → 反代 `127.0.0.1:8190`（lober console listener） | `nginx-lober.conf` |

**机器面那条 hostfwd 补的经过（值得记，因为症状会误导）：** 最初只有前两条转发，机器面
从 Windows 宿主完全不可达 —— 直连 `127.0.0.1:80` 在 TCP 层就没人监听，`curl` 表现为
**纯挂起到超时、零字节**，很容易被当成「服务响应慢」或「超时参数不够长」去调。任务 8
用二分法一步定位：SSH 进 guest 直连 `127.0.0.1:8180`（lober 自己的 gateway listener）
立刻拿到 200 与正确的 `#!ipxe` → 后端正常 → 问题 100% 在转发。

注意 hostfwd 的目标地址必须写 `10.10.10.10:80` 而不是 `127.0.0.1:80`：nginx 按 SPEC
「listen 写具体 IP 不写 0.0.0.0」绑的是 `10.10.10.10:80`，**guest 自己的 loopback 上
没人听**（在 guest 内部打 `127.0.0.1:80` 实测 HTTP 000）。宿主侧用 8080 而非 80 是因为
Windows 上 80 常被 IIS/System 占。

`hostfwd` 是 QEMU 启动参数，不能热加载 —— 加这条要完整重启 VM-S。重启时若报
`monitor telnet:127.0.0.1:4444: address in use`，是旧 QEMU 进程没退干净还占着 monitor
端口，不是配置错。
| Postgres 连接 | `127.0.0.1:5432`，db `lober_test`，角色 `lober_test` | 见下方 Postgres 一节 |

## config.py 改了什么、为什么

三个默认值被订正，理由是**实测发现原值从未真正可达**：

- **`ssh_host`**：`10.20.20.1` → `127.0.0.1`。`10.20.20.1` 是装机 VLAN
  （一张独立的 socket 网卡）那一侧的地址，这台 VM-S 从未配过第二张
  网卡，这个地址不对应任何真实接口，从 Windows 宿主永远连不上。
  `127.0.0.1:2222` 是 `start-vm.sh` 现有 SLIRP hostfwd 转发到 guest
  `10.10.10.10:22` 的地址，一直是唯一真正可达的 SSH 入口——旧默认值
  从第一天就是错的，只是任务 4/5 的 `needs_vm` 用例从未真正连过 VM-S，
  没人发现。

- **`console_base_url`**：`https://console.vmlab.internal/console/v1`
  → `http://127.0.0.1:8090/console/v1`。同时错在两处：
  (1) 上一轮 vmlab 时代签的证书把人面 listen 写成了 guest 内部回环
  `127.0.0.1:443`——QEMU 的 SLIRP hostfwd 没法转发到 guest 自己的
  loopback，这个地址从未真正可达；(2) `console.vmlab.internal` 在
  Windows 宿主没有任何 DNS 记录，即使地址可达也解析不出去。改为明文
  HTTP + 新增的 8090 hostfwd 直连，不恢复 HTTPS——测试客户端验证自签
  CA 证书链的额外信任步骤，对一次性测试 VM 收益不成比例。

- **新增 `TEST_PG_PASSWORD`**：`render_config` 原来给 `store.postgres.password`
  填的是空字符串，这台 VM-S 的 `pg_hba.conf` 对 TCP 连接要求
  `scram-sha-256`，空密码在 `fe_sendauth` 阶段就被拒绝。加了这个常量
  并给 `lober_test` 角色设了对应密码。

- **新增 `TEST_ROOT_SSH_PUBLIC_KEY`**：`render_config` 原来给
  `gateway.root_ssh_public_keys` 填 `[]`，而 `internal/config/config.go`
  的 `Validate` 明确拒绝空列表（"空列表会静默产出一台装完后没人能登录
  的机器"）——这条校验是启动时硬性拒绝，`lober -check` 会直接非零
  退出。加了一把专门生成的、与 VM-S 自身登录无关的测试密钥。

## deploy.py 改了什么、为什么

全部是**第一次真连 VM-S 才暴露的 bug**，任务 5 完成时这些函数只跑过
本机可验的类别（1、2、4），从未连过真实主机——这正是本任务存在的
理由。逐条列在下面，含实测报错原文，方便下一个人核对是否是同一个坑：

1. **`create_schema`/`teardown_schema`**：命令改成 `sudo -u postgres
   psql -d lober_test -c "CREATE SCHEMA IF NOT EXISTS ... AUTHORIZATION
   lober_test"`。原实现是 `sudo psql -U postgres ...`，在这台 VM-S 的
   `peer` 认证下，`sudo` 默认切到的 OS 用户是 root 而不是 postgres，
   `-U postgres` 只是声明"以 postgres 角色连"，两者不是一回事——报错
   是 `FATAL: Peer authentication failed for user "postgres"`。修好
   认证之后还踩了第二层：裸 `CREATE SCHEMA IF NOT EXISTS` 建出的
   schema 属主是 postgres，lober 进程实际连接用的角色 `lober_test`
   对它连 USAGE 都没有，AutoMigrate 阶段报
   `ERROR: no schema has been selected to create in (SQLSTATE 3F000)`
   ——加 `AUTHORIZATION lober_test` 让它直接成为属主才解决。另外，
   连接的数据库名必须固定是 `lober_test`（不是 schema 名本身，两者是
   独立概念，第一版误以为相同，实测报
   `FATAL: database "lobertest" does not exist` 才发现）。

2. **`deploy()` 推送二进制/配置**：改成先推到 `/tmp` 再 `sudo mv` 到
   `/opt/lober/bin/lober`、`/etc/lober/lober.yaml`。原实现直接
   `ssh.push` 到这两个路径，而它们是 `root:root 755`，SSH 登录用户
   `debian` 既无写权限也不在能写的组里——报 `PermissionError: [Errno
   13] Permission denied`。没有改成"给 debian 加权限"：那会让"测试
   环境能直接写系统路径"这件事本身变得不真实。

3. **`deploy()` 新增 `systemctl reset-failed lober`**：在 `restart`
   之前无条件跑一次（幂等，没有失败计数时是空操作）。`lober.service`
   配了 `StartLimitBurst=5`/`StartLimitIntervalSec=300`，若上一轮部署
   的配置有问题导致服务短时间内失败重启超过 5 次，systemd 会记下这个
   状态并拒绝再次启动（`Start request repeated too quickly`），即使
   这一轮推的新配置已经修好根因也不会自动清除——实测正是这样撞上的：
   `-check` 手动跑已经 exit 0，`systemctl restart` 仍然失败，直到手动
   `reset-failed` 才恢复。

4. **`_check_readiness` 判据 1 改成 `ssh.sudo(...)`**：原实现是
   `ssh.run(...)`（非特权身份）。`/etc/lober/lober.yaml` 是
   `root:lober 640`，`debian` 不在 `lober` 组里，非 sudo 身份跑
   `-check` 会在读配置这一步就被拒绝——`wait_ready` 因此永远等不到
   判据 1 通过，但报的 `rc!=0` 看起来像"配置有问题"，实际是"这个身份
   读不到配置文件"，两种失败原因不该混在一起却混在了一起。

5. **`cross_compile` 的 `out` 参数改成先 `.resolve()`**：`go build`
   子进程的 `cwd` 是 `repo_root`，不是调用方 Python 进程自己的当前
   工作目录。若 `out` 是相对路径，`-o` 参数会相对 `repo_root` 解析，
   产物落在调用方没预料到的位置——本任务写 `provision.sh` 第一次传
   相对路径 `.tmp/lober-linux-amd64` 进来时才发现：函数不报错、返回值
   正常，产物却出现在仓库根而不是 `tests/lobertest/.tmp/` 下。这是
   最隐蔽的一类 bug：看起来成功了，只是结果去了别的地方。

6. **`api.py` 新增 `Console.unauthenticated_login_status()`**：
   `deploy.wait_ready` 判据 3 一直只有类型契约（`_ConsoleLoginProbe`
   Protocol），真实 `Console` 类上从未实现过这个方法——任务 3 完成时
   没有真的连过 VM-S，这个缺口从未被暴露，第一次跑 `wait_ready` 时
   直接 `AttributeError`。补的实现：不带凭据 POST `/auth/login`，直接
   读 `resp.status_code`，不走 `unwrap`（这里"拿到 4xx"才算成功，与
   `unwrap` 的错误处理方向相反）。

7. **`tests/test_deploy.py::test_render_config_output_accepted_by_lober_check`
   补了 `create_schema`/`teardown_schema`**：原用例直接把指向一个不
   存在的 schema 的配置推上去跑 `-check`，从未真正验证过"配置字段
   形状对不对"这个类别 3 本来要验证的东西，只是每次都死在"schema 不
   存在"这个前置条件缺失上。

8. **`tests/test_deploy.py::_FakeSSH` 补了 `.sudo()` 方法**：上面第 4
   条的修复让 `_check_readiness` 调用 `ssh.sudo(...)`，而 mock 测试用
   的 `_FakeSSH` 只实现了 `.run()`，暴露 `AttributeError`。补的实现是
   `sudo(cmd) = self.run(f"sudo {cmd}")`，与真实 `SSH.sudo` 同一个
   委托形状。

## Postgres 实测细节

VM-S 的 `pg_hba.conf`（Debian 包默认）：unix socket 连接用 `peer`，
TCP 连接（`host all all 127.0.0.1/32`）用 `scram-sha-256`。这意味着：

- 任何以 `postgres` 数据库角色身份操作（建 schema、建库）都必须走
  `sudo -u postgres psql`（对应 OS 用户 `postgres`），不能是
  `sudo psql -U postgres`（对应 OS 用户 root，与 peer 规则要求的用户名
  不一致会直接拒绝，不是密码错误）。
- lober 进程本身走 TCP 连 `127.0.0.1:5432`（`store.postgres.host` 字段
  的字面值），必须提供真实密码，不能是空字符串。

`lober_test` 角色与 `lober_test` 数据库是本次 provision 新建的（供
lobertest 项目专用，与旧 vmlab 时代留下的 `lober`/`lober` 角色和数据库
互不影响，两者在同一台 Postgres 实例上并存）。

## 曾经踩到的坑（provision 过程本身，不是代码 bug）

- **这台 VM-S 是旧 `tools/vmlab` 测试床用过的机器，不是全新机器。**
  第一次 SSH 进去时 `dnsmasq`/`nginx` 已经是 `failed` 状态，`lober`
  已经是 vmlab 时代的配置在跑（`/etc/lober/lober.yaml` 文件头写着
  "VM 实验床配置"），且在疯狂重启崩溃循环（`journalctl` 显示
  `listen udp 10.20.20.1:514: bind: cannot assign requested address`）。
  provision 的第一步实质是"把一台配错网络的旧机器收敛成新项目要的
  状态"，不是"在空白机器上从零开始"——下一个人若拿到的机器状态不同
  （比如真的是全新装的），某些步骤（比如"角色已存在，跳过创建"分支）
  可能不会被走到，但幂等设计保证走到"创建"分支也会得到同样的最终
  状态。

- **`systemctl is-active` 单独看会骗人。** 第一次检查时 `lober`
  报 `active`，但 `journalctl` 显示它在 `Restart=on-failure` 下反复
  崩溃重启——`Type=simple` 下只要进程当前处于运行态就是 `active`，
  不代表它没在崩溃循环里。这正是 `wait_ready` 要三条判据组合而不是
  只看 `is-active` 的原因，本次 provision 过程中被真实验证了一次。

- **QEMU 的 `-netdev user` 一个选项内可以接多个 `hostfwd=` 子句**
  （逗号分隔），不需要为每个转发端口开一个新的 `-netdev`。加人面的
  8090 端口转发时用的是这个形式，改完需要完整重启 VM-S（`hostfwd` 是
  启动参数，不能热加载）——用 `sudo shutdown -h now` 走 guest 内部
  清出，确认 QEMU 进程真的退出后再用新参数重新 `start-vm.sh`，不要
  直接杀 QEMU 进程（避免 qcow2 意外损坏）。

- **中文输出在这个 Windows 终端环境里经常显示成乱码**（`provision.sh`
  跑起来能看到），这是终端本身的编码问题（已知的 pwsh/GBK 类问题的
  另一种表现），不是脚本或远程命令真的输出了乱码——UTF-8 字节本身是
  对的，只是显示层解码错了。不影响脚本逻辑（脚本靠退出码与字符串
  匹配判断成败，不依赖人眼读中文）。
