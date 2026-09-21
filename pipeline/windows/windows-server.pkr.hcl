# Packer 模板骨架：Windows Server，镜像直写通道用。
#
# 部署前必跑（本 worktree 未装 packer，无法在此环境实测，见 pipeline/README.md 第 6 节）：
#   cd pipeline/windows && packer init .        # 下载 vsphere 插件
#   cd pipeline/windows && packer validate .    # 语法与变量校验
#   cd pipeline/windows && packer build .       # 实际封装（需要一个可达的 vCenter/ESXi 环境）
#
# 字段依据：VMware 官方维护的 packer-examples-for-vsphere 仓库，Windows Server 2022
# 构建定义（vmware/packer-examples-for-vsphere，builds/windows/server/2022/
# windows-server.pkr.hcl，curl 直接核对源文件字段名，非转述）。本骨架只保留字段
# 结构与含义注释，具体值全部换成占位符——真实值（vCenter 地址、凭据、ISO 路径等）
# 由部署方按目标环境填写，不应该在版本库里出现。
#
# 核实结论（对应 pipeline/README.md 第 1 节）：vsphere-iso builder 的本质是
# 「挂载真实 Windows ISO，用应答文件（autounattend.xml）驱动一次完整安装过程，
# 装完后把虚拟机导出成镜像」——这与 osbuild/mkosi「直接组装文件系统树」的方式
# 完全不同，是 SPEC §11 Phase 3 明确要求「必须引导真 installer」的唯一实现路径，
# 没有跳过安装过程直接产出 Windows 镜像的办法（来源：developer.hashicorp.com
# vsphere-iso builder 文档原文："starts from a guest operating system ISO file
# and builds a virtual machine image ... using the vSphere API"）。

packer {
  required_version = ">= 1.10.0"
  required_plugins {
    vsphere = {
      source  = "github.com/vmware/vsphere"
      version = ">= 1.2.0"
    }
  }
}

# --- vCenter 连接与目标环境：占位值，部署前必须替换 ---
# 这些字段没有「看起来能用」的默认值可给——写一个假的 IP/密码只会让人误以为
# 已经配好，实际部署时才在连接失败时发现，所以全部用显式占位形式。
variable "vcenter_server" {
  type    = string
  default = "PLACEHOLDER_VCENTER_FQDN_OR_IP"
}

variable "vcenter_username" {
  type    = string
  default = "PLACEHOLDER_VCENTER_USERNAME"
}

variable "vcenter_password" {
  type      = string
  default   = "PLACEHOLDER_VCENTER_PASSWORD"
  sensitive = true
}

variable "vsphere_datacenter" {
  type    = string
  default = "PLACEHOLDER_DATACENTER_NAME"
}

variable "vsphere_cluster" {
  type    = string
  default = "PLACEHOLDER_CLUSTER_NAME"
}

variable "vsphere_datastore" {
  type    = string
  default = "PLACEHOLDER_DATASTORE_NAME"
}

variable "vsphere_network" {
  type    = string
  default = "PLACEHOLDER_NETWORK_PORTGROUP_NAME"
}

# --- 源 ISO：气隙约束下必须是构建机本地可达的路径（datastore 或 content library）---
# 对应 pipeline/README.md 第 4 项元信息里的 source_iso_sha256——打包脚本
# （scripts/package.sh）需要读同一个 ISO 文件算 sha256 写进 meta.yaml，
# 这里的路径与那里用的必须是同一份文件，不是同名不同内容的两份。
variable "iso_datastore_path" {
  type        = string
  default     = "PLACEHOLDER_DATASTORE_ISO_PATH"
  description = "ISO 在 vSphere datastore 上的路径，如 [datastore1] ISO/windows-server-2022.iso"
}

variable "build_username" {
  type    = string
  default = "PLACEHOLDER_BUILD_ACCOUNT_USERNAME"
}

variable "build_password" {
  type      = string
  default   = "PLACEHOLDER_BUILD_ACCOUNT_PASSWORD"
  sensitive = true
}

source "vsphere-iso" "windows-server" {
  vcenter_server      = var.vcenter_server
  username            = var.vcenter_username
  password            = var.vcenter_password
  insecure_connection = false

  datacenter = var.vsphere_datacenter
  cluster    = var.vsphere_cluster
  datastore  = var.vsphere_datastore

  vm_name       = "lober-image-windows-server"
  # guest_os_type 是 VMware VirtualMachineGuestOsIdentifier_enum 里的真实取值
  # （windows9Server64Guest，官方枚举描述为通用的 64 位 Windows Server 系）——
  # 命名带的数字与实际 Windows 版本号不对应，是 VMware 历史遗留的枚举命名，
  # 不是本文件的笔误。**需在目标 vSphere 版本上核实**：不同 guestId 对虚拟硬件
  # 版本的要求不同，选错会在某些 vm hardware version 上导致引导失败（社区已有
  # 类似 guestId 在旧硬件版本上出问题的报告），实测时如遇到引导失败，先确认
  # 这里的取值与目标 Windows 版本、目标 vm hardware version 是否匹配。
  guest_os_type = "windows9Server64Guest"
  # firmware 字段对应 SPEC §2.1 的 windows firmware 维度（uefi/bios 需各导一次）。
  # 此骨架先固定 uefi，bios 版本重复本文件并把这里改成 bios、guest_os_type 换成
  # 对应的 BIOS 兼容类型——两份制品在数据模型里是两条独立的 ArtifactSet，见计划
  # 任务 1，本文件不预先做成变量化的双输出（那是 scripts/package.sh 或上层 CI
  # 编排该做的事，不是单个 Packer source 定义该负责的）。
  firmware = "uefi"

  CPUs      = 2
  RAM       = 4096
  storage {
    disk_size             = 61440
    disk_thin_provisioned = true
  }
  network_adapters {
    network      = var.vsphere_network
    network_card = "vmxnet3"
  }

  iso_paths = [var.iso_datastore_path]

  # cd_files/cd_content：把应答文件（autounattend.xml）打进一张虚拟 CD，随
  # installer 一起挂载——这是 wimboot/WinPE 之外，Packer 侧驱动 Windows 无人
  # 值守安装的标准做法（来源同上，VMware 官方 windows-server.pkr.hcl 用同一
  # 模式）。真实的 autounattend.xml 模板不在本任务范围（那是应答文件渲染逻辑，
  # 属于 M4/M6 的职责，Packer 侧只负责把渲染好的文件挂载进去）。
  #
  # path.cwd（不是 path.root）：逐字对照官方参考的用法——该仓库里 cd_files
  # 统一用 path.cwd，path.root 只在 templatefile() 里定位模板源文件时使用，
  # 两者不是同义写法可以随便换（Packer 官方文档区分「模板所在目录」与「当前
  # 工作目录」两个不同概念，具体差异本轮未能直接核对官方页面原文——
  # developer.hashicorp.com 域名在本次核实环境下无法访问，仅按已验证的真实
  # 用例逐字对照，未独立确认两者的精确语义边界，见 openQuestions）。
  cd_files = ["${path.cwd}/data/"]
  cd_content = {
    "autounattend.xml" = "PLACEHOLDER_RENDERED_ANSWER_FILE_CONTENT"
  }

  boot_command    = ["<spacebar>"]
  boot_wait       = "5s"
  ip_wait_timeout = "40m"

  communicator   = "winrm"
  winrm_username = var.build_username
  winrm_password = var.build_password
  winrm_timeout  = "6h"

  # convert_to_template=false：本流水线要的是「导出成一份独立镜像文件」，
  # 不是「留一份 vSphere 模板供 clone」——两者是不同的交付形态，SPEC 要的
  # 是能带出气隙边界的文件，不是留在 vCenter 里的模板对象。
  convert_to_template = false

  # export：vsphere-iso builder 内置的导出配置（不是独立的 post-processor），
  # 装完后把虚拟机导出成 OVF+VMDK 落到构建机本地目录——字段名核对自 VMware
  # 官方 windows-server.pkr.hcl 的 export 块（vmware/packer-examples-for-vsphere，
  # curl 直接核对源文件，非转述）。导出的 OVF/VMDK 不是本流水线最终交付物
  # （README 第 2.1 节要的是 raw 格式），VMDK → raw 的转换与后续压缩统一交给
  # scripts/package.sh 做，保持与 Linux 侧同一个打包脚本、同一套输出结构。
  export {
    name             = "lober-image-windows-server"
    force            = true
    image_files      = true
    output_directory = "${path.cwd}/build-output/windows-server/"
  }
}

build {
  sources = ["source.vsphere-iso.windows-server"]
}
