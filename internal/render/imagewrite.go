// 本文件实现镜像直写通道的写盘脚本渲染（P3-03 任务 4），依据 SPEC §11
// Phase 3。
//
// # 这条通道与 installer 驱动的关系
//
// **并存，不是替代**（SPEC §11 Phase 3）。镜像直写要先引导一个最小环境，再由
// 它把镜像写到盘上——**那个最小环境不是 installer**，它只需要网络、一个解压器
// 与一个写盘工具。
//
// 最小环境的选择：**复用某个发行版的 installer ramdisk 但不跑 installer**，
// 只传一个不同的 init 参数。代价是它比必需的大，但传输那几十 MB 不是瓶颈；
// 收益是驱动覆盖由发行版维护——自建最小 initramfs 的最大隐性成本就是驱动集合，
// 新网卡与新存储控制器的驱动是持续的负担。
//
// # 信号点的映射
//
// 镜像直写没有 pre/packages/post 三个装包阶段，但信号点**复用同一套名字**，
// 不新增信号：
//
//	pre-start / pre-done           取到写盘脚本、准备目标盘前后
//	packages-start / packages-done 写盘的前后（这一步对应装包，是最长的阶段）
//	post-start / post-done         分区扩展与 firstboot 脚手架落盘的前后
//	rebooting 及之后                与 installer 驱动通道相同
//
// 复用的理由：状态机、分阶段超时预算与控制台的时间线一行代码都不用改。
//
// **阶段超时预算已复核，不需要改**（§3.3）：写盘的耗时与装包不同量级——几 GB
// 的镜像在千兆网下几分钟，而 packages 的预算是 60 分钟，绰绰有余。这条结论
// 写在这里，免得后来者以为是忘了调整。
package render

import (
	"fmt"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// ImageWriteScript 渲染镜像直写通道的写盘脚本。
//
// 五步（计划任务 4）：
//
//  1. 从网关取写盘脚本（内含目标盘、镜像 URL、校验值）——即本函数的产出本身
//  2. 流式下载并解压镜像，**直接写到目标盘**，不落地到临时文件
//  3. **写完后校验**：重读写入的数据算 sha256 与清单比对
//  4. 按清单里的分区布局描述扩展分区
//  5. 挂载新系统的根分区，落进 firstboot 脚手架与凭据
//
// 第 5 步让镜像直写与 installer 驱动在 firstboot 之后汇合——两条通道的
// firstboot 机制相同（§5.8 的脚手架），所以用户的 firstboot hook 在两条通道上
// 行为一致。**这是两条通道唯一的共用部分，要保持**，因此这里直接调用
// FirstbootScaffold 而不是另写一份。
func ImageWriteScript(ctx model.RenderContext) (string, error) {
	img, root, err := imageWriteInputs(ctx)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Lober 镜像直写脚本 —— 由网关渲染，不要手工编辑。\n")
	b.WriteString("# 引导的是一个最小环境（installer ramdisk 但不跑 installer），\n")
	b.WriteString("# 本脚本负责把预先封装好的镜像写到目标盘上。\n")
	b.WriteString("set -eu\n\n")

	b.WriteString(ClientLib(ctx))
	b.WriteString("\n")

	b.WriteString(imageWriteStepPrepare(ctx, img))
	b.WriteString(imageWriteStepWrite(ctx, img))
	b.WriteString(imageWriteStepVerify(img))
	b.WriteString(imageWriteStepGrow(root))
	b.WriteString(imageWriteStepFirstboot(ctx, root))

	return b.String(), nil
}

// imageWriteInputs 校验并取出渲染所需的两项输入：镜像制品与根分区。
//
// 三种拒绝各有实在的后果，所以都是硬错误而不是降级：
//
//   - 通道不是镜像直写：两条通道的机制完全不同，混用会产出一个引导不起来的
//     东西（installer 驱动的制品集里没有镜像文件）。
//   - Image 为 nil：脚本里的镜像路径与校验值会是空字符串，机器会去下载一个
//     空 URL，然后把零字节写到盘上。
//   - 没有唯一的根分区：第 4 步不知道该扩展哪个分区，而盲目扩展一个分区是
//     破坏性操作。
func imageWriteInputs(ctx model.RenderContext) (*model.ImageArtifact, model.Partition, error) {
	if !ctx.Artifact.Channel.IsImageWrite() {
		return nil, model.Partition{}, fmt.Errorf(
			"render: 制品集 %q 的通道是 %q，不是镜像直写，不能渲染写盘脚本"+
				"（两条通道的机制完全不同：installer 驱动的制品集里没有镜像文件）",
			ctx.Artifact.ID, ctx.Artifact.Channel.Normalize())
	}
	img := ctx.Artifact.Image
	if img == nil {
		return nil, model.Partition{}, fmt.Errorf(
			"render: 制品集 %q 标为镜像直写通道但 Image 为空 —— 没有镜像路径与"+
				"校验值，脚本里那几个位置会是空串，机器会去下载一个空 URL",
			ctx.Artifact.ID)
	}
	if img.ImagePath == "" || img.UncompressedSHA256 == "" {
		return nil, model.Partition{}, fmt.Errorf(
			"render: 制品集 %q 的镜像三项不完整（ImagePath=%q UncompressedSHA256 是否为空=%v）"+
				"—— 缺镜像路径无从下载，缺解压后校验值无从回读校验",
			ctx.Artifact.ID, img.ImagePath, img.UncompressedSHA256 == "")
	}
	root, ok := img.PartitionLayout.RootPartition()
	if !ok {
		return nil, model.Partition{}, fmt.Errorf(
			"render: 制品集 %q 的分区布局里没有恰好一个根分区（共 %d 个分区）—— "+
				"扩展分区那一步不知道该扩哪个，而盲目扩展一个分区是破坏性操作",
			ctx.Artifact.ID, len(img.PartitionLayout.Partitions))
	}
	return img, root, nil
}

// imageWriteStepPrepare 是第 1 步：确定目标盘，做写盘前的准备。
//
// 目标盘取自 fact LOBER_DISK_PRIMARY，那是服务端 DiskPlanner 的决策结果
// （§5.6 先采集再服务端决策），值是 by-id 路径。**脚本里不能写死 /dev/sda**：
// 在「系统盘不是第一块」的机器上会擦掉错误的盘，且不可恢复。
func imageWriteStepPrepare(ctx model.RenderContext, img *model.ImageArtifact) string {
	return fmt.Sprintf(`signal '%s' 0 /dev/null

# 第 1 步：目标盘来自服务端决策（§5.6），值是 by-id 路径，脚本不自行猜设备名。
TARGET_DISK='%s'
if [ -z "$TARGET_DISK" ] || [ ! -b "$TARGET_DISK" ]; then
  echo "目标盘 $TARGET_DISK 不存在或不是块设备" >&2
  signal '%s' 1 /dev/null
  exit 1
fi

# 容量下限：解压后的镜像必须装得下，否则写到一半才失败、盘已被破坏。
DISK_BYTES="$(blockdev --getsize64 "$TARGET_DISK")"
IMAGE_BYTES=%d
if [ "$DISK_BYTES" -lt "$IMAGE_BYTES" ]; then
  echo "目标盘 $DISK_BYTES 字节 < 镜像 $IMAGE_BYTES 字节，装不下" >&2
  signal '%s' 1 /dev/null
  exit 1
fi

# 写盘前解除可能占用目标盘的挂载与 device-mapper 映射，否则内核会拒绝写入。
umount "$TARGET_DISK"* 2>/dev/null || true

signal '%s' 0 /dev/null
`,
		model.SignalPreStart,
		ctx.Facts[model.FactDiskPrimary],
		model.SignalPreDone,
		img.UncompressedSizeBytes,
		model.SignalPreDone,
		model.SignalPreDone,
	)
}

// imageWriteStepWrite 是第 2 步：流式下载、解压、直接写到目标盘。
//
// **不落地到临时文件**（计划任务 4 第 2 步的「必须」）：目标机器可能没有足够
// 的临时空间放一个几 GB 的文件，且落地会让写盘时间翻倍（先写一遍临时文件再读
// 出来写盘）。因此是一条 curl | zstd -d | dd 的管道，中间不碰磁盘。
//
// 用 packages-start/done 这一对信号：这一步对应装包，是最长的阶段（见文件头
// 的信号映射表）。
func imageWriteStepWrite(ctx model.RenderContext, img *model.ImageArtifact) string {
	return fmt.Sprintf(`
# 第 2 步：流式下载 → 解压 → 直接写盘。三者用管道串起来，中间不落地到临时
# 文件——目标机器可能没有几 GB 的临时空间，且落地会让写盘时间翻倍。
signal '%s' 0 /dev/null

IMAGE_URL="$ART/%s/%s"
if ! curl -fsS --retry 3 --retry-delay 5 "$IMAGE_URL" \
     | zstd -d -c \
     | dd of="$TARGET_DISK" bs=4M conv=fsync status=none; then
  echo "写盘失败（下载/解压/写入三者之一）" >&2
  signal '%s' 1 /dev/null
  exit 1
fi

# 让内核重读分区表——刚写进去的分区表内核还不知道。
partprobe "$TARGET_DISK" 2>/dev/null || true
sync

signal '%s' 0 /dev/null
`,
		model.SignalPackagesStart,
		ctx.Artifact.ID,
		img.ImagePath,
		model.SignalPackagesDone,
		model.SignalPackagesDone,
	)
}

// imageWriteStepVerify 是第 3 步：回读校验。
//
// **这一步不能省，且必须在分区扩展之前。**
//
// 不能省的理由：写盘是这条通道唯一的破坏性操作，静默的写入错误（坏道、控制器
// 问题）会产出一个看起来装好但会随机崩溃的系统——回读一遍是发现它的唯一方式。
//
// 必须在扩展之前的理由：扩展分区会改动分区表与文件系统超级块，那之后再算
// sha256 必然与清单不符；顺序错了要么校验永远失败、要么实现者为了让它通过而把
// 校验范围改小，两种都等于失去这道检查。
//
// 只读镜像本体那么长的字节（UncompressedSizeBytes），不读整盘：盘比镜像大，
// 镜像之后的区域是未定义内容，算进去校验值必然不符。
func imageWriteStepVerify(img *model.ImageArtifact) string {
	return fmt.Sprintf(`
# 第 3 步：回读校验。必须在分区扩展之前——扩展会改动分区表与超级块，之后再算
# sha256 必然与清单不符。写盘是本通道唯一的破坏性操作，静默的写入错误（坏道、
# 控制器问题）会产出一个看起来装好但随机崩溃的系统，回读是发现它的唯一方式。
EXPECTED_SHA='%s'
ACTUAL_SHA="$(dd if="$TARGET_DISK" bs=4M count=%d iflag=count_bytes status=none | sha256sum | cut -d' ' -f1)"
if [ "$ACTUAL_SHA" != "$EXPECTED_SHA" ]; then
  echo "回读校验失败：盘上内容 sha256=$ACTUAL_SHA，清单声明=$EXPECTED_SHA" >&2
  signal '%s' 1 /dev/null
  exit 1
fi
`,
		img.UncompressedSHA256,
		img.UncompressedSizeBytes,
		model.SignalPackagesDone,
	)
}

// imageWriteStepGrow 是第 4 步：按分区布局扩展根分区到整盘。
//
// 只扩根分区，其余原样保留——盲目扩展 ESP 或 swap 会破坏镜像。文件系统的增长
// 工具按类型分派（resize2fs 与 xfs_growfs 不通用），未知类型时报错而不是猜。
func imageWriteStepGrow(root model.Partition) string {
	return fmt.Sprintf(`
# 第 4 步：把根分区扩展到整盘。镜像是固定大小的，目标盘大小不一。
signal '%s' 0 /dev/null

ROOT_PARTNUM=%d
ROOT_FSTYPE='%s'
# 分区设备名的两种形态：/dev/sda2 与 /dev/nvme0n1p2。用 lsblk 问内核而不是
# 自己拼字符串——拼错会操作到另一个分区上。
ROOT_DEV="$(lsblk -lnpo NAME "$TARGET_DISK" | sed -n "$((ROOT_PARTNUM+1))p")"
if [ -z "$ROOT_DEV" ]; then
  echo "找不到第 $ROOT_PARTNUM 号分区的设备节点" >&2
  signal '%s' 1 /dev/null
  exit 1
fi

growpart "$TARGET_DISK" "$ROOT_PARTNUM" || true
case "$ROOT_FSTYPE" in
  ext2|ext3|ext4) resize2fs "$ROOT_DEV" ;;
  xfs)
    # xfs_growfs 只能对**已挂载**的文件系统操作，与 resize2fs 不同。
    mkdir -p /mnt/lober-root
    mount "$ROOT_DEV" /mnt/lober-root
    xfs_growfs /mnt/lober-root
    umount /mnt/lober-root
    ;;
  btrfs)
    mkdir -p /mnt/lober-root
    mount "$ROOT_DEV" /mnt/lober-root
    btrfs filesystem resize max /mnt/lober-root
    umount /mnt/lober-root
    ;;
  *)
    echo "不认识根分区的文件系统类型 $ROOT_FSTYPE，不猜增长工具" >&2
    signal '%s' 1 /dev/null
    exit 1
    ;;
esac
`,
		model.SignalPostStart,
		root.Number,
		root.Filesystem,
		model.SignalPostDone,
		model.SignalPostDone,
	)
}

// imageWriteStepFirstboot 是第 5 步：挂载新系统的根分区，落进 firstboot
// 脚手架与凭据。
//
// **这是两条通道唯一的共用部分**：直接嵌 FirstbootScaffold 的产出，不另写一份
// （计划任务 4 第 5 步）。两份实现迟早漂移，而漂移的症状是「同一个 firstboot
// hook 在镜像直写通道上行为不同」——那会让运维不敢用这条通道。
func imageWriteStepFirstboot(ctx model.RenderContext, root model.Partition) string {
	return fmt.Sprintf(`
# 第 5 步：挂载新系统的根分区，落进 firstboot 脚手架与凭据。
# 下面这段脚手架与 installer 驱动通道用的是**同一个生成函数**
# （render.FirstbootScaffold），不是另写一份——两条通道的 firstboot 机制必须
# 一致，否则用户的 firstboot hook 在两条通道上行为不同。
mkdir -p /mnt/lober-root
mount "$ROOT_DEV" /mnt/lober-root

cat >/mnt/lober-root/root/lober-firstboot-install.sh <<'LOBER_FIRSTBOOT_EOF'
%s
LOBER_FIRSTBOOT_EOF
chmod 0700 /mnt/lober-root/root/lober-firstboot-install.sh

sync
umount /mnt/lober-root

signal '%s' 0 /dev/null
signal '%s' 0 /dev/null
`,
		FirstbootScaffold(ctx),
		model.SignalPostDone,
		model.SignalRebooting,
	)
}
