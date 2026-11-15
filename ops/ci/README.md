# Lober 内部流水线

## 云服务器宿主机上的候选镜像

在 Docker Engine 所在的云服务器宿主机运行。执行账号需能使用 Docker，并拥有一个权限为 `0700` 的共享制品目录。各阶段使用同一个目录和同一个 Docker 构建器。Docker 权限等同于宿主机管理权限，只交给受信的执行账号。

```sh
python3 -I ops/ci/worker.py image --root /srv/lober-ci/images +  --repo /srv/lober-ci/repo.git --commit "$candidate_sha"
python3 -I ops/ci/worker.py image --root /srv/lober-ci/images +  --repo /srv/lober-ci/repo.git --commit "$merged_sha" --promote-from "$candidate_sha"
```

执行前先在共享 Git 仓库拉取对应提交。镜像按完整 Git 文件树查找，晋级要求源提交是目标的祖先，且两者文件内容完全一致。原镜像缺失时，晋级会失败。镜像存在于 Docker 本地存储，`images/*.json` 只保存索引。每次运行在 `runs/<运行编号>/` 写结果和日志。发布、回滚仍会使用的镜像须保留。

如宿主机无法直连 Go 官方代理，可由管理员为执行器设置 `GOPROXY`。构建仍执行 Go 的模块校验。

默认检查只运行容器中的 `lober -h`。配置 `--check` 可指定管理员维护的检查程序，依次传入实际镜像校验值和本轮记录目录。已有企业测试和合并检查由各自的入口执行。

容器批量装机使用管理员入口 `bash ops/ci/check-candidate-image debian-batch "$@"`。
设置 `LOBER_IMAGE_DEBIAN_DVD`、`LOBER_IMAGE_DEBIAN_MINI`、
`LOBER_IMAGE_RELEASE_MANIFEST`，并给执行器传入 `--check-timeout 10800`。
批次启动一份候选容器，记录容器、实际镜像和程序身份。四台来宾各用独立空盘和网络，
按两次并行、两次串行执行，原件保存在本轮的 `kvm/` 目录。
旧容量入口只运行二进制，不能给容器候选生成长稳通过记录。

管理员可创建 `image-control`、`image-debian` 两个启动器，分别执行
`bash check-candidate-image control "$@"` 和 `bash check-candidate-image debian "$@"`，
在两次执行器调用里分别作为 `--check`，使用相同的原提交与制品目录。
控制面需要 `LOBER_IMAGE_SYNTHETIC_ISO`、`LOBER_ENTERPRISE_NODE` 和
`LOBER_ENTERPRISE_PLAYWRIGHT`。Debian 需要 `LOBER_IMAGE_DEBIAN_NETINST`、
`LOBER_IMAGE_DEBIAN_MINI`，以及宿主机的 KVM、QEMU、dnsmasq 和 tcpdump。
可设 `LOBER_IMAGE_DEBIAN_MIRROR=ustc` 选择中科大镜像。每轮启动独立数据库，
Debian 使用专用空盘和隔离网络。

晋级流程使用 `--promote-from "$candidate_sha" --verified-runs "$control_run" "$debian_run"`。
执行器检查两个运行编号是否对应原提交和同一镜像，是否通过控制面检查、完成装机且通过重复引导。
这个检查补充原有发布门槛。分支审查、完整企业验证和发布审批仍由现有流程执行。

GitHub 的 `enterprise-release` 继续由宿主机回报到试合并提交。用于回报的
`verify-as-stage test` 要求发布清单包含 `image_id`、`container_control_run`
和 `container_debian_run`，并要求所有运行服务的发布测试结果写入相同的 `image_id`。
启用前向发布校验账号提供只读证据访问，并保留 `preview` 的必过检查和人工审批。
缺证据、镜像不一致或原有发布门槛不通过时，回报失败。

在具备 Docker 的 Linux 测试宿主机执行以下命令。使用一个尚不存在的证据目录：

```sh
bash ops/ci/verify-image-reuse /srv/lober-ci/repo.git "$candidate_sha" /srv/lober-ci/reuse-verification
```

验证会检查同提交重跑是否复用镜像、仅改 Go 时前端构建是否命中缓存，以及同文件树晋级是否沿用原镜像。四轮分别保存实际运行记录。

## 原有二进制测试入口

`dev` 和 `test` 在同一台 Linux 虚拟机运行。执行器定期查询 GitHub，从对应分支下载确切提交。每个阶段有自己的系统账号、状态目录、工具缓存、测试数据库账号和数据目录。状态回报使用第三个账号。

## 安装前

- 准备 Python 3.12 或更新版本、Git、Go、Make、Node、pnpm、BusyBox、debconf、`govulncheck`、PostgreSQL 客户端工具和 Chromium 运行依赖。Go 与 Node 版本按候选仓库要求。将工具放在管理员维护的路径里，给两个执行器配置各自的缓存。
- 为两个执行器创建专用 PostgreSQL 数据库和账号。该账号只访问自己的测试库，允许创建和清理测试使用的数据库模式。已运行的开发服务使用另一份数据库和账号。
- `recovery` 会模拟磁盘满和网络超时，具体权限见 [企业测试说明](../../tests/lobertest/enterprise/README.md)。为每个阶段准备专用网络命名空间，通过 systemd 的 `NetworkNamespacePath` 接入，并配置同一命名空间中的测试来源地址。执行器会核对自身命名空间。故障注入只授予受限的挂载和防火墙权限。缺少隔离或权限时，检查保持失败。
- 开发环境部署和冒烟分别接入管理员维护的两个可执行文件。两者都会收到候选程序绝对路径和完整提交号。部署入口须复核目标环境、程序校验值和正在运行的任务，并通过已有开发部署服务完成备份、切换和恢复。部署权限由部署服务持有。冒烟入口检查实际运行的版本、启动、登录和关键接口。任一入口缺失或退出码非零时，`dev-ready` 保持未通过。

`dev` 在 lobervm 上使用 `lober-ci-dev-app.service`。它只监听 `127.0.0.1:17831`，由独立账号 `lober-ci-dev-app` 运行。开发实例的 PostgreSQL 和数据目录与两个测试阶段分开。管理员在 `/etc/lober-ci-dev-app.json` 设置专用数据库、本地监听地址、独立制品目录和随机初始密码。该文件由 root 持有，权限为 `0640`，只授权开发服务组读取。部署入口复制固定提交的程序，切换后复核实际运行程序的校验值、前端入口和数据库登录。启动或冒烟失败时恢复上一个可执行文件。数据库迁移恢复须单独验证。

故障启动器为每轮创建只含回环接口的网络和独立挂载空间。测试库通过当前阶段的 Unix 套接字转接。故障操作仅接受本轮测试目录的 8 MiB 临时挂载和本轮镜像来源端口的丢包规则，测试账号不直接获得系统挂载或防火墙权限。安装时将 `dev-deploy`、`dev-smoke`、`dev-app-control`、`recovery-launch`、`recovery_session.py` 和 `recovery-bin/sudo` 放入 `/opt/lober-ci/ops/ci/`，校验并安装 `systemd/lober-ci-dev-app.sudoers`。将 `systemd/dev-environment.conf` 和 `systemd/test-recovery.conf` 分别安装到对应阶段的 `lober-ci-worker@<阶段>.service.d/`，再重新加载 systemd 配置。

## 安装执行器

用管理员账号从审查过的提交安装文件。以下命令在该源码目录运行。专用账号的家目录分别设为 `/var/lib/lober-ci-dev` 和 `/var/lib/lober-ci-test`。

```sh
for stage in dev test; do
  sudo useradd --system --user-group --no-create-home \
    --home-dir "/var/lib/lober-ci-$stage" --shell /usr/sbin/nologin "lober-ci-$stage"
done
sudo useradd --system --user-group --no-create-home --shell /usr/sbin/nologin lober-ci-report

sudo install -d -m 0755 /opt/lober-ci/ops/ci /opt/lober-ci/tests/lobertest/enterprise
sudo install -d -m 0750 -o root -g lober-ci-report /etc/lober-ci
sudo install -m 0644 ops/ci/{worker,verify,report}.py /opt/lober-ci/ops/ci/
sudo install -m 0755 ops/ci/{app-token,verify-as-stage} /opt/lober-ci/ops/ci/
sudo install -m 0644 tests/lobertest/enterprise/{gate.py,profiles.json,production_capacity.py,installation_evidence.py,kvm_install_evidence.py,cloud_install.py,cloud_packet_capture.py,cloud_capture_session.py,cloud_guest_identity.py,cloud_no_task_probe.py,upgrade_evidence.py} /opt/lober-ci/tests/lobertest/enterprise/
sudo install -m 0644 ops/ci/systemd/*.service ops/ci/systemd/*.timer /etc/systemd/system/
sudo visudo -cf ops/ci/systemd/lober-ci-report.sudoers
sudo install -m 0440 ops/ci/systemd/lober-ci-report.sudoers /etc/sudoers.d/lober-ci-report
sudo systemctl daemon-reload
```

为 `/etc/lober-ci/dev.env` 和 `/etc/lober-ci/test.env` 配置下面的变量，文件由管理员持有且权限为 `0600`。数据库连接串保存在配置文件里。

| 变量 | 阶段 | 用途 |
| --- | --- | --- |
| `LOBER_CI_REPOSITORY` | 两者 | `https://github.com/linuxerlv/lober.git` |
| `PATH` | 两者 | 管理员维护的工具目录，包括锁定的 Go、Node、pnpm 和漏洞扫描工具 |
| `LOBER_TEST_PG_DSN` | 两者 | 对应阶段独立测试数据库的连接串 |
| `LOBER_CI_RECOVERY_NETNS` | 两者 | 本阶段专用网络命名空间路径，例如 `/run/netns/lober-ci-dev` |
| `LOBER_CI_RECOVERY_LAUNCHER` | 两者 | 管理员持有的隔离故障启动器。启动器只接收候选提交，创建一次性的网络和挂载空间 |
| `LOBER_ENTERPRISE_VM_IP` | 两者 | 对应隔离测试环境的地址，用于故障测试的临时来源服务 |
| `LOBER_CI_DEV_DEPLOY`、`LOBER_CI_DEV_SMOKE` | dev | 管理员维护的开发环境部署和冒烟程序绝对路径 |
| `LOBER_CI_MEDIA_CHECKSUMS`、`LOBER_TEST_ESXI7_ISO`、`LOBER_TEST_ESXI8_ISO`、`LOBER_TEST_ESXI9_ISO`、`LOBER_TEST_REAL_ISO` | test | 可信来源校验值清单及四张镜像的绝对路径 |
| `LOBER_CI_CAPACITY_MANIFEST` | test | 准备完成后的容量配置文件，内容是候选清单中的 `capacity` 对象 |
| `PLAYWRIGHT_BROWSERS_PATH` | 可选 | 本阶段的浏览器缓存路径 |

真实 ISO 由内网测试机提供，在 `test` 服务中只读挂载。使用发行方校验值，或使用维护者明确指定的私有原件计算 SHA-256 后核对 VM 副本。仓库、公开制品和发布包不分发 ISO。四份镜像齐全且实际读取、识别和导入通过后，才生成 `real_media` 通过证据。

启动对应服务可以先单轮执行。确认日志和独立资源后再启动定时器。

```sh
sudo systemctl start lober-ci-worker@dev.service
sudo systemctl start lober-ci-worker@test.service
sudo systemctl enable --now lober-ci-worker@dev.timer lober-ci-worker@test.timer
journalctl -u lober-ci-worker@dev.service -u lober-ci-worker@test.service
```

每轮产物放在 `/var/lib/lober-ci-<阶段>/<完整提交号>/`。查看 `execution.json` 和 `logs/`。需要重跑时，给对应服务的 `ExecStart` 加 `--retry` 后执行单轮，再移除临时覆盖配置。

真实镜像、装机和长稳测试按 [企业测试说明](../../tests/lobertest/enterprise/README.md) 准备。先让执行器冻结程序和本轮编号。然后依据 `candidate.json` 准备原件身份文件和安装原件，将原件放入同一候选下的私有 `qualification/` 目录，由 `lober-ci-test` 持有。配置 `LOBER_CI_CAPACITY_MANIFEST` 后重跑一次，绑定容量配置并更新同一轮的自动检查。容量负载和最终证据引用冻结后的清单。使用 `installation_evidence.py --qualification-root <本轮 qualification 目录>` 检查装机和长稳原件，输出写入同一轮 `logs/`。升级使用企业测试说明中的容器入口。给发布校验账号只读访问原始证据目录的权限。缺少任一结果时完整发布检查失败。

执行器只处理已合入受保护分支的代码。阶段账号负责生成该阶段的测试证据，管理员负责审查候选执行入口和保护该账号。GitHub 回报账号使用管理员安装的检查器复核结果。

### 4 GiB 虚拟机的 Debian 装机诊断

`kvm-pxe-probe` 的 `debian` 模式在独立网络启动一台 1 GiB 来宾。使用只读挂载的 Debian 13.7 DVD-1 和同版本 mini ISO。宿主的测试网络没有默认路由，来宾的 DNS 不向外转发。需要下载的软件包通过 Unix 套接字转接，安装器仍校验 Debian 软件源签名。

管理员将 `kvm-pxe-probe` 以 `0755` 安装到 `/opt/lober-ci/ops/ci/`，将 `kvm_install_probe.py` 和 `debian_mirror.py` 以 `0644` 安装到同一目录。以下命令由管理员执行。`sha` 是已冻结候选的完整提交号，`dvd` 和 `mini` 是内网测试机上验签及校验通过的两个镜像文件的绝对路径。

```sh
sudo systemd-run --unit=lober-ci-debian-mirror-diagnostic \
  -p User=lober-ci-test -p Group=lober-ci-test \
  -p RuntimeDirectory=lober-ci-debian-mirror -p RuntimeDirectoryMode=0750 \
  -p RuntimeMaxSec=70m -p MemoryMax=192M -p NoNewPrivileges=yes -p ProtectSystem=strict \
  /usr/bin/python3 -I /opt/lober-ci/ops/ci/debian_mirror.py /run/lober-ci-debian-mirror/archive.sock

sudo test -S /run/lober-ci-debian-mirror/archive.sock
evidence=$(sudo mktemp -d "/var/lib/lober-ci-test/$sha/diagnostic-pxe.XXXXXXXX")
sudo systemd-run --unit=lober-ci-kvm-debian-diagnostic \
  -p PrivateNetwork=yes -p MemoryMax=2350M -p MemorySwapMax=0 -p RuntimeMaxSec=65m \
  -p "BindReadOnlyPaths=$dvd:/run/lober-ci-verified-media/debian-dvd.iso $mini:/run/lober-ci-verified-media/debian-mini.iso" \
  /usr/bin/flock -w 90 /run/lock/lober-ci-build.lock \
  /opt/lober-ci/ops/ci/kvm-pxe-probe "$sha" "$evidence" debian
```

装机结束后停止 `lober-ci-debian-mirror-diagnostic.service`。诊断保留安装状态、抓包、串口和来宾验证日志。抓包和串口可能含临时装机凭据，只能保存在本轮私有目录。单台诊断不提供发布所需的两次并行、两次串行和重复启动证据。

正式 4 GiB 路径使用 `kvm-qualification-run`。它为四次装机分别创建空磁盘和隔离网络，先并行两次，再串行两次。四台来宾通过本机套接字连接同一个候选服务和数据库。每次安装结束后复位来宾并重新抓包，核对原系统盘、系统身份、变化后的启动编号和原装机任务。`kvm_install_evidence.py` 读取原始抓包和日志，复核每次启动与任务信号，并根据候选事件时间核对并行与串行顺序。

将 `kvm-qualification-run` 安装到 `/opt/lober-ci/ops/ci/`。使用和单台装机相同的只读镜像挂载、软件源转接、`PrivateNetwork=yes` 和共享执行锁。批次根目录由管理员在固定候选下以 `mktemp -d "$candidate/kvm-qualification.XXXXXXXX"` 创建。批次服务名为 `lober-ci-kvm-qualification`，转接服务名为 `lober-ci-debian-mirror-qualification`。批次运行在一个内存预算内，由脚本为各台来宾分配网络。

将 `capacity-qualification-run` 以 `0755` 安装到 `/opt/lober-ci/qualification/`，并将 `capacity-qualification.py`、`production_capacity.py`、`kvm_install_evidence.py`、`cloud_packet_capture.py`、`db_env.py` 以 `0644` 安装到该目录。通过 `systemd-run` 的 `PrivateNetwork=yes` 运行 `capacity-qualification-run <完整提交号> <装机批次根目录>`。它在四次装机及抓包复核通过后才启动正式容量负载。容量阶段使用独立 PostgreSQL 实例，日志保存在同一候选的 `qualification/kvm-capacity.*`。容量服务必须允许完整数据准备、常态、突发、24 小时长稳和最终复核的运行时间。

## 接上 GitHub 合并拦截

1. 把 GitHub 应用安装到该仓库。应用需有读取代码和合并请求、写入提交状态的权限。将应用私钥保存在仅状态回报账号可读的文件，权限设为 `0400`。在 `/etc/lober-ci/report.env` 配置 `LOBER_CI_GITHUB_REPOSITORY=linuxerlv/lober`、`LOBER_CI_APP_ID`、`LOBER_CI_INSTALLATION_ID`、`LOBER_CI_APP_KEY` 和 `LOBER_CI_STATUS_CREATOR_ID`。最后一项是应用机器人账号的数字 ID，可用 `gh api 'users/应用名%5Bbot%5D' --jq .id` 查询。
2. 执行 `sudo systemctl start lober-ci-report.service` 检查回报，再启用 `lober-ci-report.timer`。回报程序只处理当前同仓库的 `dev → test` 和 `test → preview` 合并请求，校验 GitHub 试合并提交的两个父提交和文件内容。它只给匹配的试合并提交回报状态。证据缺失时回报失败。
3. 在 `preview` 的保护设置中加上必过检查 `enterprise-release`，来源限定为该 GitHub 应用。接通 `dev` 部署后，在 `test` 的保护设置中同样加入 `dev-ready`。保留已有 `go`、`web`、独立审批和讨论解决要求。
4. 演练一个缺证据的候选，确认合并按钮被拦截。更换源提交或目标提交，确认旧的试合并结果无法放行新的请求。GitHub 要求来源分支包含目标分支最新提交时，首次晋级和之后的分支历史衔接需要按仓库流程处理，先验证规则配置与预期的合并方式兼容。

本地复核脚本可运行 `python3 -B -m unittest discover -s ops/ci`。实际产品测试和 GitHub 合并检查的验收在指定环境执行。

[GitHub 应用令牌](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app) · [GitHub 分支保护](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)
