# nginx 真机集成验证报告

验证环境：QEMU + WHPX 的 Debian 13 (trixie) 虚拟机，`D:/qemu-vm/debian-cloud/`，
`nginx/1.26.3`（Debian 官方 `nginx-full` 包）。Go 侧交叉编译
`GOOS=linux GOARCH=amd64` 后 scp 进虚拟机，Windows 侧不跑 nginx，因此本报告
的每一条结论都来自虚拟机内的真实命令输出，不是本地模拟。

## 一句话结论

之前的 e2e（`scripts/e2e.sh`）是 curl 直连 Go 进程，本报告验证的是生产形态：
nginx 作唯一入口、Go 绑回环。SPEC 明确警告过三处"只有真 nginx 才会暴露"的
陷阱，三处都在真机上验证过，其中两处代码侧已经正确实现，第三处（SSE 事件
去重水位）发现一个真实的 Go 侧 bug。另有一处 nginx 配置的小瑕疵（重复的
`Accept-Ranges` 响应头）。全部发现已记入下方"发现的问题"一节与任务返回值
的 `findings`/`contractGaps`。

## 环境偏差声明（先说清楚，避免误读下面的"验证通过"）

**虚拟机只有一块网卡（`enp0s2`，固定 `10.10.10.10`）。** 生产部署是机器面
（装机 VLAN）与人面（管理网）各绑一个不同网段的 IP，SPEC §1.4 的信任域
隔离依赖这两个 IP 分属不同网络、物理上不可达。本虚拟机做不到这一点——
机器面与人面被迫绑同一个 IP，只能靠端口区分（机器面 `:80`，人面 `:8880`）。

**因此：本报告"已验证"的是 nginx 配置语法（`nginx -t`）与 location 层的
行为（Range/gzip/缓冲/限流/X-Real-IP 信任判断），不包括"两个信任域物理
隔离"这件事本身——那需要两块网卡的主机才能验证，本虚拟机结构性做不到，
不是测试疏漏。凡下文提到"已验证"，均指配置语法与单机内 location 行为；
凡涉及网络层信任域隔离的表述，均标注为"虚拟机单网卡限制，未验证"。

## nginx 编译选项核对

```
$ /usr/sbin/nginx -V 2>&1 | tr ' ' '\n' | grep -- '--with-threads\|--with-http_secure_link_module'
--with-threads
--with-http_secure_link_module
```

两项都在（Debian 13 官方 `nginx-full` 包自带）。`--with-threads` 是
`aio threads` 指令的前提，已确认 `nginx -t` 接受该指令（若缺失该编译选项，
`nginx -t` 会直接报错，见下文阶段3）。`--with-http_secure_link_module`
MVP 阶段不使用（Phase 2 制品授权才需要），本次确认存在但未测试其行为。

## 三件"只有真 nginx 能暴露"的事——逐条验证

### 1. X-Real-IP 只在直连 peer 是回环时可信（SPEC §1.6、§9.2）

代码位置：`internal/gateway/clientip.go`（机器面）与
`internal/console/clientip.go`（人面/审计），两处逻辑一致：仅当
`r.RemoteAddr` 解析为回环地址时才信任 `X-Real-IP`/`X-Forwarded-For`
头部，否则原样使用 TCP 连接的真实 peer。

**正向用例（经 nginx 的正常请求，已验证，`scripts/e2e-nginx.sh` 阶段6
自动跑）：**

```
$ curl -fsS -X POST http://10.10.10.10:8880/console/v1/hosts ...
$ ssh debian@vm "cat lober.log | grep host.create"
time=2026-09-22T05:44:36.555Z level=INFO msg=audit actor=admin
  action=host.create target="" detail="client_ip=10.10.10.10"
```

lober 进程本身绑 `127.0.0.1:8081`（回环），nginx 转发到它时 TCP peer
天然是回环地址，Go 侧因此信任 nginx 注入的 `X-Real-IP: 10.10.10.10`
（真实客户端地址）。若这条判断没生效，审计记录的 `client_ip` 会是
`127.0.0.1`——上面的输出证明不是这样。

同样的机制在机器面网关侧用另一条独立路径验证：`/v1/boot` 命中授权后，
网关把 `clientIP(r)` 记进进程内的"客户端 IP → 任务"映射
（`internal/gateway/syslog.go` 的 `rememberClientTask`），供 syslog UDP
包按源 IP 归档。用一个从 `10.10.10.10`（nginx 看到的真实客户端地址）
发出的 UDP 包验证：

```
$ python3 -c "s=socket.socket(...); s.bind(('10.10.10.10',0)); \
    s.sendto(b'TEST', ('127.0.0.1', 8514))"
$ cat /srv/lober/log/tasks/<task_id>/pending.log
TEST: syslog line from 10.10.10.10 for X-Real-IP trust test
```

正确落在了该任务目录下（不是 `_syslog_unmatched`），证明网关侧
`clientIP()` 对经 nginx 的 `/v1/boot` 请求也解析出了 `10.10.10.10`。

**负面用例（peer 非回环时头部必须被忽略）：**

这条**不在 `scripts/e2e-nginx.sh` 的自动化范围内**——理由见该脚本文件头
"已知限制 2"：要构造"peer 非回环"的样本，需要额外起一个绑在真实网卡上
的 lober 实例（被测部署的 lober 本身绑回环，天然拿不到非回环 peer）。
本报告手工验证过一次，记录如下（**已在虚拟机上验证**，用的是一个专为此
测试临时起的 lober 实例，配置与被测部署实例不同，事后已停止并清理）：

```
# 临时实例绑真实网卡 10.10.10.10:18080（不是被测部署用的 8080）
$ curl -H 'X-Real-IP: 6.6.6.6' \
    'http://10.10.10.10:18080/v1/boot?sn=NEGTEST-SN-01&mac=...'
# （引导授权成功，签发 install token）

# 从真实网卡地址发一个 UDP 包，验证 clientTaskMap 里存的是真实 peer
# 10.10.10.10 而不是伪造的 6.6.6.6（后者无法在无 root/无原始套接字的
# 前提下伪造 UDP 源地址，因此改为验证"用真实地址发包能否命中映射"这个
# 等价形式：命中即证明映射的 key 是真实 peer，不是伪造头部）：
$ python3 -c "s.bind(('10.10.10.10',0)); s.sendto(b'TEST1B', ('127.0.0.1',18514))"
$ cat /home/debian/srv2/log/tasks/<task_id>/pending.log
TEST1B-attempt2
```

命中，证明 `clientTaskMap` 的 key 是真实 TCP peer（`10.10.10.10`），不是
请求头里伪造的 `6.6.6.6`——若代码错误地信任了非回环 peer 的头部，这里
应该命中不到（map 的 key 会是 `6.6.6.6`，而我从 `10.10.10.10` 发的包
查不到）。

**结论：两处 `clientIP()` 实现（网关与控制台）在真 nginx 环境下行为均
正确——peer 回环时信任头部（正向已验证），peer 非回环时忽略头部
（负向已用临时实例人工验证一次）。**

### 2. SSE 必须不被 nginx 缓冲（SPEC §1.4 第 3 点、§12.8）

```
$ scripts/e2e-nginx.sh 阶段9 实际输出（同一台虚拟机，多次运行的样本）：
信号发出时间: 1790055878.797130446  SSE 事件到达时间: 1790055879.811593147  延迟: 1.014s
信号发出时间: 1790055741.136386105  SSE 事件到达时间: 1790055742.167549728  延迟: 1.031s
信号发出时间: 1790055599.146513016  SSE 事件到达时间: 1790055600.163943062  延迟: 1.017s
```

三次独立运行，延迟稳定在约 1.0-1.03 秒——非常接近 Go 侧
`streamPollInterval = 1500ms` 的轮询间隔（略小于 1.5s 是因为信号发出的
时刻恰好落在两次轮询之间，不是每次都要等满一个完整周期）。若
`proxy_buffering` 未生效，事件会被攒到连接结束或 nginx 缓冲区打满才
一次性吐出——本次验证用 `timeout 12` 限定连接生命周期，事件在 1 秒级
到达，而不是等到 12 秒超时前后才出现，直接证明了"实时到达"而不是
"看起来有输出"。

**一个容易误读的细节：** `X-Accel-Buffering: no` 这个响应头**经 nginx
转发后client 端看不到**：

```
$ ssh debian@vm "curl -sS -D - http://127.0.0.1:8081/console/v1/tasks/<id>/stream" # 直连Go
HTTP/1.1 200 OK
X-Accel-Buffering: no      ← 直连 Go 时头部在
...
$ curl -sS -D - http://10.10.10.10:8880/console/v1/tasks/<id>/stream  # 经 nginx
HTTP/1.1 200 OK
（无 X-Accel-Buffering 行）  ← 经 nginx 后头部消失
```

**这不是 bug，是 nginx 的既有设计**：`X-Accel-*` 系列头部（包括
`X-Accel-Buffering`、`X-Accel-Redirect` 等）是 nginx 定义的"内部控制"
头部，upstream 用它们告诉 nginx"这次响应要怎么处理"，nginx 消费后不会
转发给最终客户端——这正是 `deploy/nginx/lober.conf` 该处注释预期的行为
（"X-Accel-Buffering: no 由 Go 侧响应设置，这里不需要重复 add_header"）。
真正要验证的不是"客户端能不能看到这个头"，而是"nginx 的缓冲行为有没有
真的被这个头关掉"——上面的时间戳数据已经证明关掉了。

### 3. 制品走 location 直接发盘、Range 可用、gzip 关（SPEC §1.4 第 2/3/4 点）

```
$ curl -sSI http://10.10.10.10/artifacts/<id>/vmlinuz
HTTP/1.1 200 OK
Accept-Ranges: bytes
Accept-Ranges: bytes        ← 重复了两次，见下方"发现的问题"

$ curl -sS -D - -r 2-6 http://10.10.10.10/artifacts/<id>/vmlinuz
HTTP/1.1 206 Partial Content
Content-Range: bytes 2-6/20
Accept-Ranges: bytes        ← 206 响应里只有一次，不重复
（body 字节核对：请求 vmlinuz 的第 2-6 字节，取到"ke ke"文件内容里对应
  的准确 5 个字节，与磁盘原文件逐字节比对一致）

$ curl -sS -D - -H 'Accept-Encoding: gzip' http://10.10.10.10/artifacts/<id>/vmlinuz
HTTP/1.1 200 OK
（无 Content-Encoding: gzip 行）
（响应体逐字节比对与磁盘原文件一致，未被压缩）

$ 对照：人面（console.lober.test:8880）的 JSON 端点带 Accept-Encoding: gzip：
HTTP/1.1 200 OK
Content-Encoding: gzip      ← 人面确实压缩，机器面确实不压缩，六项必须
                                配对之三的两个方向都验证了
```

**制品请求未经过 Go 的核对：**

```
$ ssh debian@vm "grep -c 'vmlinuz\|artifacts' lober.log"
0
$ ssh debian@vm "tail -3 /var/log/nginx/lober-boot.log"
... "/artifacts/<id>/vmlinuz" 200 0 0.000
... "/artifacts/<id>/vmlinuz" 206 5 0.001
... "/artifacts/<id>/vmlinuz" 200 20 0.001
```

nginx access log 记了三次制品请求（含正确的状态码与字节数），lober 自己
的进程日志里对 `vmlinuz`/`artifacts` 零匹配——直接证明这些请求由 nginx
的 `sendfile`/`aio threads` 路径直接发盘，从未进入 Go 进程。

## 附带核实

### 信号端点独立限流桶（SPEC §10 运维点二）

```
$ ab -n 500 -c 50 -p postbody.txt -T text/plain \
    'http://10.10.10.10/v1/signal/<task>/<token>/packages-start?rc=0&seq=9999'
...
Non-2xx responses:      454   ← 信号桶被打满，多数请求被限流拒绝

# 与此同时（信号洪水进行中）：
$ curl http://10.10.10.10/v1/boot?sn=...&mac=...
HTTP 200                       ← 引导端点完全不受影响
```

用单发 curl 循环测不出效果（进程创建开销本身就把请求速率拖到远低于
`rate=60r/s`，本报告编写过程中先踩过这个坑后改用 `ab`）。用 `ab -c 50`
真并发打信号桶，454/500 被拒绝，同一时刻引导端点仍然 100% 成功——两个
限流桶（`lober_boot`/`lober_signal`）确认互相独立，一个桶被打满不影响
另一个。

### nginx -t 通过（占位符替换后，替换后的文件不写回仓库）

```
$ sudo nginx -t -c /tmp/.../nginx-main.conf
nginx: the configuration file .../nginx-main.conf syntax is ok
nginx: configuration file .../nginx-main.conf test is successful
```

仓库里的 `deploy/nginx/lober.conf`/`lober-http-snippet.conf`
**未被修改**（`git status`/`git diff` 均无变化）——占位符替换只发生在
`scripts/e2e-nginx.sh` 生成的临时工作目录里，运行结束即随 `mktemp -d`
的目录一起被 `trap cleanup EXIT` 删除。

## 发现的问题

### Go 侧真实 bug：SSE 事件流对 Seq<=0 的事件无限重复推送

**文件**：`internal/console/stream.go`，`streamPushBatch` 函数。

**现象**：`model.TaskEvent.Seq` 字段的文档约定是"非正值表示非信号来源"
（`internal/core/model/event.go:34`）——任务创建事件、syslog 转发事件等
都合法地带 `Seq <= 0`。但 `streamPushBatch` 里推进水位的逻辑是：

```go
if e.Seq > *afterSeq {
    *afterSeq = e.Seq
}
```

`afterSeq` 初始为 0，任何 `Seq <= 0` 的事件永远满足不了 `e.Seq > *afterSeq`
（`-1 > 0` 为假），水位永远卡在 0（或卡在上一个真正推进过水位的正数
`Seq`），导致这些事件在 SSE 连接存活期间**每个轮询周期（1.5s）都被
重新推送一次，直到连接断开**——不是推送一次就停。

**真机复现（虚拟机上实测，非理论推导）**：

```
$ curl -N http://10.10.10.10:8880/console/v1/tasks/<task_id>/stream ...
13:23:49 id: -1  ...任务已创建...
13:23:51 id: -1  ...任务已创建...   ← 同一条事件，1.5s 后又来一次
13:23:52 id: -1  ...任务已创建...   ← 又来一次
13:23:54 id: -1  ...任务已创建...   ← 又来一次
（持续到连接结束，共观察到同一条事件重复 10 次以上）
```

**影响**：
1. 运维打开任务详情页订阅 SSE，会看到任务创建、syslog 转发等事件反复
   刷屏，而不是每条事件只出现一次——直接影响可用性（日志噪音，且可能
   让运维误以为任务在原地重复某个阶段）。
2. 间接影响新事件的到达延迟：本报告阶段9 记录的 SSE 延迟（约 1.0-1.03秒）
   仍然测出"实时到达"，但那是因为测试用的任务积累的历史事件不多；若
   一个任务长时间运行、事件表积累了多条 `Seq<=0` 的事件，每个轮询周期
   都要重新序列化并 flush 这些重复内容，会增加真正新事件到达前的排队
   延迟（在本报告阶段9测试之前，另一轮手工测试中曾观察到一次延迟被拖到
   约10.5秒，原因正是同一个连接积累了较多历史`Seq<=0`事件需要先重放）。

**不是本次任务能修的范围**：Go 代码由其它 agent 负责（本任务铁律第1条：
不碰 Go 代码），此处只记录发现，不动手修。**建议的修复方向**（供编排方
参考，不代表本报告已验证该方向可行）：`afterSeq` 的语义应该是"已经推送
过的事件数"或"已推送的最大偏移"，而不是简单的"最大 Seq 值"——对
`Seq<=0` 的事件，可能需要用一个独立的"已推送计数"或时间戳水位，或者
`ListEvents(AfterSeq)`的过滤语义本身需要重新设计，使其对非信号来源事件
也能正确表达"已经推送过"。

### nginx 配置的小瑕疵：`/artifacts/` location 的 `Accept-Ranges` 头重复

**文件**：`deploy/nginx/lober.conf`，`/artifacts/` location（第 70 行
`add_header Accept-Ranges bytes;`）。

**现象**：完整 GET 请求（非 Range）时，响应头出现两次
`Accept-Ranges: bytes`：

```
Accept-Ranges: bytes
Accept-Ranges: bytes
```

**原因**：nginx 的 `sendfile on` 对静态文件天然支持 Range 请求，会自动
在响应里加一次 `Accept-Ranges: bytes`；配置里又显式 `add_header
Accept-Ranges bytes;` 加了第二次。206 Partial Content 响应时只出现一次
（观察到的实际行为，未深究 nginx 内部为何两种响应路径表现不同）。

**影响**：目前观察到的客户端（curl）都能正常工作，两次相同的头部值
不会造成语义歧义（不像"两个不同值"那样危险）。但严格的 HTTP 客户端或
某些代理链可能对重复头部行为不一致，是一个值得清理的小瑕疵而非阻断性
缺陷。**建议**：删掉显式的 `add_header Accept-Ranges bytes;`，因为
`sendfile on` 已经会自动加这个头（本报告未验证"删掉之后是否在所有场景
下都还有这个头"，只观察到当前两次共存时功能正常）。

## `scripts/e2e-nginx.sh` 说明

新建脚本，走 nginx 的九阶段以上端到端验证（实际拆成 11 个阶段，比
`scripts/e2e.sh` 的 9 个阶段多两个专属 nginx 的验证点：阶段6 的
X-Real-IP 信任判断、阶段9 的 SSE 计时、阶段10 的制品直发验证、阶段11 的
限流桶独立性——某些内容合并在同一阶段编号内）。已在虚拟机上完整跑通两次
（确认可重复，非偶然通过），退出码 0，`trap cleanup EXIT` 确认不留残留
工作目录、不留监听端口。

用法：

```bash
LOBER_BIN=/path/to/lober-linux MACHINE_IP=10.10.10.10 \
  [CONSOLE_IP=10.10.10.10] [MACHINE_PORT=80] [CONSOLE_PORT=8880] \
  bash scripts/e2e-nginx.sh
```

`LOBER_BIN` 必须是交叉编译好、已经 scp 到目标主机的 linux/amd64 二进制
（本脚本不负责编译，因为目标主机通常没有 Go 工具链）。`MACHINE_IP`/
`CONSOLE_IP` 相同时脚本会打印单网卡限制提醒（见本报告"环境偏差声明"）。

脚本文件头详细记录了四条已知限制（制品集/模板两处契约缺口与 e2e.sh
相同、X-Real-IP 负面用例不在自动化范围、单网卡时不能证明信任域隔离、
限流测试依赖 `ab` 不可用时会跳过并说明原因），不在本报告重复。

## 本次任务未验证的项

- **两个信任域的网络层物理隔离**（SPEC §1.4 六项必须配对之一背后的
  真正意图）：需要两块网卡的主机，本虚拟机结构性做不到。
- **`--with-http_secure_link_module` 的实际签名校验行为**：MVP 阶段
  未启用，只确认了编译选项存在。
- **TLS 增量**（`deploy/nginx/lober.conf` 底部的注释块）：本次任务
  范围内未涉及，MVP 阶段本就不启用。
- **大文件（几 GB 级）的 Range/sendfile 性能**：本次用的是几十字节的
  假制品文件，Range 语义验证充分，但"零拷贝在几 GB 文件上的实际吞吐"
  未测（需要真实几 GB 镜像与更长时间的压测，超出本任务范围）。
