# Packer 模板骨架：ESXi 安装介质封装，镜像直写通道用。
#
# 部署前必跑（本 worktree 未装 packer/qemu，无法在此环境实测，见
# pipeline/README.md 第 6 节）：
#   cd pipeline/esxi && packer init .        # 下载 qemu 插件
#   cd pipeline/esxi && packer validate .    # 语法与变量校验
#   cd pipeline/esxi && packer build -var esxi_iso_path=/path/to/esxi.iso \
#     -var output_disk_path=/path/to/output/image.raw .
# （官方参考用环境变量 VMWARE_ESXI_ISO_PATH + env() 函数注入路径，本文件改用
# -var 命令行覆盖，与 windows/windows-server.pkr.hcl 的占位变量写法保持
# 一致，两个模板用同一套「变量 + PLACEHOLDER 默认值」约定，不混用两种
# 传参风格）
#
# 核实结论（对应 pipeline/README.md 第 1 节）：ESXi 与常规 Linux/Windows 不同——
# 它本身就是一套 hypervisor，「封装 ESXi 镜像」不是「在 vCenter 里装一台 ESXi
# 当 nested guest」，而是「产出一份能被写到物理机本地盘上的 ESXi 安装结果」。
# 因此本模板不用 vsphere-iso（那个 builder 面向的是「vCenter 里装一台 guest VM」，
# 与 ESXi 封装的目标不匹配），改用 qemu builder 在虚拟机里跑一次真实的 ESXi
# kickstart 安装，把安装结果的虚拟盘直接当作镜像产物导出。
#
# 这个模式不是本任务凭空设计的——Canonical MAAS 项目的官方 packer-maas 仓库
# 用完全相同的思路封装 ESXi 安装介质供 MAAS 装机用（qemu builder + kickstart +
# raw 格式输出 + 压缩后处理器），核对方式是 curl 直接拉取该仓库
# vmware-esxi/vmware-esxi.pkr.hcl 源文件，不是转述。这与 Lober 的目标场景高度
# 一致（都是「用 Packer 产出一份可写盘的 ESXi 镜像交给另一个装机系统用」），
# 因此参照其字段结构，具体数值仍全部替换为占位符或按 SPEC §2.1 的 ESXi 约束调整。
#
# 来源：github.com/canonical/packer-maas，vmware-esxi/vmware-esxi.pkr.hcl
# 与 vmware-esxi/KS.CFG（curl 核对，2026-09-21）。

packer {
  required_version = ">= 1.10.0"
  required_plugins {
    qemu = {
      source = "github.com/hashicorp/qemu"
      # 官方参考（canonical/packer-maas）用 ">= 1.1.0, < 1.1.2"，本文件没有
      # 照抄这个上界——查过 packer-plugin-qemu 的 CHANGELOG.md，1.1.2 附近
      # 没有找到对应的已知问题说明，无法确认那条上界对本项目是否仍然适用，
      # 与其抄一条来源不明的约束，不如只保留确认过的下界。若实测时在较新
      # 插件版本上复现出问题，再补一条有实测依据的上界。
      version = ">= 1.1.0"
    }
  }
}

variable "esxi_iso_path" {
  type        = string
  default     = "PLACEHOLDER_PATH_TO_ESXI_ISO"
  description = "ESXi 安装 ISO 的本地路径（气隙约束：构建机需能本地访问，不从网络拉取）"
}

variable "output_disk_path" {
  type        = string
  default     = "build-output/esxi/image.raw"
  description = "封装产出的原始镜像文件路径（未压缩）。压缩为 image.raw.zst 交给 scripts/package.sh 做，见下方 qemuargs 注释"
}

# 本 source 块的字段与结构逐条对照 canonical/packer-maas 官方仓库
# vmware-esxi/vmware-esxi.pkr.hcl（curl 直接核对源文件，2026-09-21）。
# **没有按其它 Packer 例子的常见写法自行改写**——ESXi 封装这件事的坑点
# （USB 注入 kickstart、q35 机型、按住 Shift 进 installer 命令行等）已经
# 由这份官方参考验证过，本文件只做「替换成本项目的变量名与输出路径约定」
# 这一类改动，不改动已验证过的具体取值。
source "qemu" "esxi" {
  iso_url = var.esxi_iso_path
  # ESXi 安装 ISO 通常没有对外发布的官方 sha256（Broadcom 下载页不提供，
  # 或提供但按登录会话变化），此处按官方文档允许的写法用 "none" 跳过校验——
  # **这不是本流水线放弃校验**：源 ISO 的完整性校验由本地下载渠道自行保证，
  # 而封装产物本身的 sha256（meta.yaml 里的 source_iso_sha256）在打包阶段
  # （scripts/package.sh）单独计算并写入交付物，供后续追溯，两者不是同一层
  # 校验，不能用「构建时跳过了 ISO 下载校验」推断「产物没有校验」。
  iso_checksum = "none"

  accelerator = "kvm"
  # 本机是 Windows（见任务说明「本机是 Windows，没有 nginx/dnsmasq...」），
  # kvm 加速器在此不可用——**这条约束提醒了构建机必须是 Linux/带 KVM 的环境，
  # 不能在当前 Windows worktree 里跑通**，与 README 第 6 节「交付前必跑」
  # 一致：本文件在当前环境无法实测，需要在有 KVM 的 Linux 构建机上验证。
  machine_type = "q35"
  cpus         = "4"
  memory       = 8192
  # disk_size/format 两个字段是 qemu builder 自动创建默认磁盘时用的参数，
  # **本模板不依赖它**——真正的输出磁盘由下方 qemuargs 里手工声明的
  # ide-hd 设备承担（与官方参考一致：那份参考同样只声明了 qemuargs 里的盘，
  # 没有额外依赖 builder 自动创建的默认盘）。保留 disk_size/format 只是为了
  # 让 qemu builder 内部逻辑一致，不代表两份磁盘都会被使用。
  disk_size  = "32G"
  format     = "raw"
  headless   = true
  net_device = "vmxnet3"

  # boot_command：ks=usb://ks.cfg 的写法来自 ESXi 安装器对 kickstart 位置的
  # 命令行参数约定（对应 SPEC §2.1 附录 A.7/A.8 已核实的「脚本位置靠 ks= 指定」），
  # usb:// 前缀表示从模拟 USB 设备读取，与下方 qemuargs 里挂载的 usb.img 对应。
  # <leftShift>O 是「按住 Shift 敲 O」进入 ESXi installer 的命令行编辑模式的
  # 复合按键 token，逐字抄自官方参考，不是按其它 Packer 例子里
  # <leftCtrlOn>/<leftCtrlOff> 的写法类推的（那是另一种按键表达语法，混用
  # 会导致按键序列错误）。
  #
  # Lober 生产环境里 ks= 实际指向网关渲染的应答文件 HTTP 地址（SPEC §2.1：
  # 「这份 boot.cfg 由网关渲染，不放 nginx 静态」），此处封装阶段用本地 USB
  # 镜像模拟只是为了在没有网关的构建环境里也能跑通安装，**不代表生产装机时
  # 也用 USB**——生产路径的 ks= 是 HTTP URL，由计划任务 4/M4 渲染逻辑决定，
  # 与本封装脚本的 USB 注入方式是两条独立路径，不要混淆。
  boot_command = [
    "<enter><wait>",
    "<leftShift>O",
    " ks=usb://ks.cfg",
    " cpuUniformityHardCheckPanic=FALSE",
    " systemMediaSize=min",
    " com1_Port=0x3f8 tty2Port=com1",
    "<enter>"
  ]
  boot_wait    = "3s"
  communicator = "none"

  # qemuargs：完整照抄官方参考的设备拓扑，只替换 ISO 路径变量名与输出磁盘
  # 路径。三组设备各自的作用：
  #   -usb + usb-storage + file=usb.img            承载 kickstart（ks.cfg）
  #   -cdrom                                       挂载 ESXi 安装 ISO
  #   ich9-ahci + ide-hd + file=var.output_disk_path  安装的目标盘，即最终
  #                                                     产出的 image.raw
  # usb.img 的生成（写入 KS.CFG 内容的小容量 FAT 镜像）由 scripts/package.sh
  # 或独立的准备步骤完成，不在本 Packer 模板内联生成——Packer 模板本身不
  # 适合做「生成一个文件系统镜像」这种前置准备工作。
  qemuargs = [
    ["-cpu", "host"],
    ["-serial", "stdio"],
    ["-usb"],
    ["-device", "usb-storage,drive=usb0"],
    ["-drive", "file=usb.img,if=none,id=usb0,format=raw"],
    ["-cdrom", var.esxi_iso_path],
    ["-device", "ich9-ahci,id=sata"],
    ["-device", "ide-hd,drive=sata-disk,bus=sata.0"],
    ["-drive", "file=${var.output_disk_path},if=none,id=sata-disk,cache=writeback,discard=ignore,format=raw"],
    ["-boot", "d"]
  ]

  shutdown_timeout = "1h"
  # ESXi kickstart 的 %post 阶段自己执行 halt（见 KS.CFG 里的
  # "The install reboots by default, when creating an image halt so Packer
  # can run post-processors and finish"），Packer 侧不需要额外发送
  # shutdown_command，与官方参考的做法一致。
}

build {
  sources = ["source.qemu.esxi"]
}
