# Alpine 自动发现镜像

从网络引导的 Alpine 在内存中运行。它读取本机硬件，上报后停在结果屏。采集不挂载磁盘、不写硬盘，也不需要服务器管理网或管理口凭据。电源、风扇等数据可通过 Linux 传感器和本机管理芯片接口读取。

## 构建

在对应架构的 Linux 上执行。需要 Bash、curl、GNU tar、GNU cpio、gzip 和 root 权限。构建固定使用 Alpine 3.24.4 的官方最小根文件系统，校验预先记录的 SHA-256。软件包来自 Alpine 3.24 的签名软件源，实际版本写入 `packages.txt`。

```sh
sudo bash ops/discovery/build.sh x86_64 /tmp/lober-discovery/x86_64
# 在 ARM64 Linux 上构建 ARM64 镜像
sudo bash ops/discovery/build.sh aarch64 /tmp/lober-discovery/aarch64
```

构建会把网卡驱动、依赖和固件打进 `initramfs.gz`，检查每个目标网卡驱动及其声明的固件。镜像下载后即可加载网卡，无需用尚未识别的网卡下载驱动。默认清单覆盖常见 Intel、Mellanox、Broadcom、Realtek、ENA 和虚拟机网卡。清单见 `nic-drivers.txt` 和 `firmware-files.txt`。加驱动时，用 `EXTRA_NIC_MODULES` 和 `EXTRA_FIRMWARE_PACKAGES` 提供以空格分隔的模块名、固件包名。换内核或扩展清单后，重新测试目标机器的启动网口。

如需读取部分 Broadcom 阵列控制器的成员盘和逻辑盘映射，可设置 `STORCLI_BINARY` 指向适合目标架构的本机工具，并用 `STORCLI_SHA256` 提供校验和。这个工具由使用方根据厂商条款准备，默认镜像不携带它。镜像只对该工具执行 `show` 查询。没有兼容工具时，报告仍包含控制器和系统可见盘，并标明未读取的成员盘信息。

## 接入机器网关

把目标架构的 `vmlinuz` 和 `initramfs.gz` 放到 `preview.artifacts` 指向的制品目录。下面示例启用 x86-64，ARM64 在同一映射增加 `aarch64` 项。

```yaml
machine:
  enabled: true
  discovery:
    enabled: true
    allowedNetworks: [192.0.2.0/24]
    images:
      x86_64:
        kernel: discovery/x86_64/vmlinuz
        initrd: discovery/x86_64/initramfs.gz
```

把示例网段换成实际装机网。Lober 按直连来源地址判断网段。只有代理入口已按实际来源地址限制机器请求来自装机网时，才把直接连接 Lober 的代理单个地址加入 `allowedNetworks`，IPv4 写成 `/32`，IPv6 写成 `/128`。Lober 不采用代理转发的来源地址。

在装机网的 iPXE 脚本里沿用现有引导地址，附加启动网口硬件地址。`netX` 是 iPXE 内置设置，代表最近打开的网口，`dhcp` 成功后再读取它。把示例域名换成 Lober 网关地址。

```ipxe
#!ipxe
dhcp || exit
chain http://lober.example.test/v1/boot?sn=${smbios/serial:uristring}&arch=${buildarch}&firmware=${platform}&mac=${netX/mac:hexhyp} || exit
```

采集系统优先通过启动网口获取地址。镜像找不到网卡时，结果屏会显示能识别的网卡硬件编号和固件加载错误。已经报到但始终传不上报告时，控制台仍有报到记录。现场结果屏只显示阶段和失败状态，上传凭据不会输出到日志。

## 验证

```sh
python3 -m unittest discover -s ops/discovery -p 'test_*.py'
python3 ops/discovery/smoke.py /tmp/lober-discovery/x86_64
```

第二条命令使用 QEMU 的 iPXE 网卡引导和本机测试接收端。检查虚拟机从 iPXE 下载镜像、Alpine 获取网络地址、上传带启动网口的报告并停在结果屏。用 `--direct` 可单独排查内核和内存镜像。实际部署还需验证 Lober 接收端与控制台、目标型号驱动、阵列和传感器。
