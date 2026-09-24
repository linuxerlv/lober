// 本文件测试镜像直写通道的写盘脚本渲染（P3-03 任务 4），依据该计划
// 「测试要锁住的行为」五条。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// imageWriteContext 构造一份镜像直写通道的渲染上下文。
//
// 以 minimalAutoinstallCtx 为底再改：那个 helper 已经把 Facts、Bases、
// token、SSH 公钥这些必需输入都填齐了，本文件只需要覆盖通道相关的部分。
// 家族取 RHEL 而不是沿用 Ubuntu——镜像直写与家族无关（写的是块设备，不跑
// installer），取一个常见家族避免读者以为这条通道只对 Ubuntu 可用。
func imageWriteContext(t *testing.T) model.RenderContext {
	t.Helper()
	ctx := minimalAutoinstallCtx()
	ctx.Artifact.OSFamily = model.OSFamilyRHEL
	ctx.Hooks = []model.HookRef{
		{HookDigest: "dddd4444firstboot", Stage: model.HookStageFirstboot, Order: 10, Required: true},
	}
	ctx.Artifact.Channel = model.ChannelImageWrite
	ctx.Artifact.Image = &model.ImageArtifact{
		ImagePath:             "image.raw.zst",
		SHA256:                "aaaa",
		UncompressedSHA256:    "bbbb",
		SizeBytes:             1024,
		UncompressedSizeBytes: 4096,
		ManifestPath:          "manifest.yaml",
		SignaturePath:         "manifest.yaml.sig",
		PartitionLayout: model.PartitionLayout{
			TableType: "gpt",
			Partitions: []model.Partition{
				{Number: 1, Filesystem: "vfat", Role: model.PartitionRoleESP},
				{Number: 2, Filesystem: "xfs", Role: model.PartitionRoleRoot},
			},
		},
	}
	return ctx
}

// TestImageWriteScript_FiveStepsPresent 验证写盘脚本含计划要求的五步。
func TestImageWriteScript_FiveStepsPresent(t *testing.T) {
	got, err := ImageWriteScript(imageWriteContext(t))
	if err != nil {
		t.Fatalf("ImageWriteScript 失败: %v", err)
	}

	// 五步各自的标志性内容。用「必须出现的命令/字符串」而不是精确匹配整段
	// 脚本，让实现有调整空间但关键动作不能少。
	steps := map[string]string{
		"第1步 取目标盘与镜像 URL":     "LOBER_DISK_PRIMARY",
		"第2步 流式解压写盘":          "zstd",
		"第3步 回读校验":            "bbbb",
		"第4步 按布局扩展分区":         "growpart",
		"第5步 落 firstboot 脚手架": "firstboot",
	}
	for name, needle := range steps {
		if !strings.Contains(got, needle) {
			t.Errorf("%s：脚本里找不到 %q —— 五步缺一步就不是一条完整的写盘流程",
				name, needle)
		}
	}
}

// TestImageWriteScript_StreamsWithoutTempFile 验证写盘是流式的：脚本里不出现
// 任何临时文件落地。
//
// 计划任务 4 第 2 步把「不落地」列为**必须**：目标机器可能没有足够的临时空间
// 放一个几 GB 的文件，且落地会让写盘时间翻倍（先写一遍临时文件再读出来写盘）。
//
// 判据是「curl 直接管到 zstd 再管到 dd/写盘」这条管道存在，且不出现
// -o/-O/> 到某个路径再读回来的形态。
func TestImageWriteScript_StreamsWithoutTempFile(t *testing.T) {
	got, err := ImageWriteScript(imageWriteContext(t))
	if err != nil {
		t.Fatalf("ImageWriteScript 失败: %v", err)
	}

	// 管道形态必须在：下载 → 解压 → 写盘，一条管道串起来。
	if !strings.Contains(got, "curl") || !strings.Contains(got, "| zstd") {
		t.Errorf("脚本里没有「curl 管到 zstd」的流式形态 —— 目标机器可能没有"+
			"几 GB 的临时空间，且落地会让写盘时间翻倍。脚本：\n%s", got)
	}

	// 这些是「落地到临时文件」的典型痕迹。出现任一即说明实现退化成了先下载
	// 再写盘。
	for _, forbidden := range []string{"/tmp/image", "/var/tmp/image", "curl -o ", "curl -O "} {
		if strings.Contains(got, forbidden) {
			t.Errorf("脚本里出现临时文件痕迹 %q —— 计划任务 4 第 2 步要求流式写盘，"+
				"不落地到临时文件", forbidden)
		}
	}
}

// TestImageWriteScript_ReadBackVerifyBeforePartitionGrow 验证回读校验发生在
// 写盘之后、分区扩展之前。
//
// **顺序错了会校验到已被改动的数据**（计划任务 4 的测试要求原文）：扩展分区
// 会修改分区表与文件系统超级块，那之后再算整盘 sha256 必然与清单不符，于是
// 要么校验永远失败、要么实现者为了让它通过而把校验范围改小——两种都等于失去
// 了这道检查。
//
// 而这道检查不能省：写盘是这条通道唯一的破坏性操作，静默的写入错误（坏道、
// 控制器问题）会产出一个看起来装好但会随机崩溃的系统，回读一遍是发现它的
// 唯一方式。
func TestImageWriteScript_ReadBackVerifyBeforePartitionGrow(t *testing.T) {
	got, err := ImageWriteScript(imageWriteContext(t))
	if err != nil {
		t.Fatalf("ImageWriteScript 失败: %v", err)
	}

	verifyAt := strings.Index(got, "bbbb") // 解压后镜像的期望 sha256
	growAt := strings.Index(got, "growpart")
	if verifyAt < 0 || growAt < 0 {
		t.Fatalf("脚本里缺回读校验（%d）或分区扩展（%d）", verifyAt, growAt)
	}
	if verifyAt > growAt {
		t.Errorf("回读校验出现在分区扩展之后（校验位置 %d > 扩展位置 %d）—— "+
			"扩展分区会改动分区表与文件系统超级块，之后再算 sha256 必然与清单不符；"+
			"顺序错了等于失去这道检查，而它是发现静默写入错误的唯一方式",
			verifyAt, growAt)
	}
}

// TestImageWriteScript_SignalNamesAreInContract 验证脚本里用到的信号名都在
// M1 的信号表内。
//
// 镜像直写没有 pre/packages/post 三个装包阶段，但信号点**复用同一套名字**
// （计划任务 4 的信号映射表）：packages-start/done 对应写盘前后（这一步对应
// 装包，是最长的阶段）。复用而不是新增信号名，是为了让状态机、超时预算与
// 控制台的时间线一行代码都不用改。
func TestImageWriteScript_SignalNamesAreInContract(t *testing.T) {
	got, err := ImageWriteScript(imageWriteContext(t))
	if err != nil {
		t.Fatalf("ImageWriteScript 失败: %v", err)
	}

	// 六个必须出现的信号（三对）。
	for _, sig := range []model.SignalKind{
		model.SignalPreStart, model.SignalPreDone,
		model.SignalPackagesStart, model.SignalPackagesDone,
		model.SignalPostStart, model.SignalPostDone,
	} {
		if !strings.Contains(got, string(sig)) {
			t.Errorf("脚本里缺信号 %q —— 缺了它状态机推不动阶段，任务会卡在"+
				"上一个阶段直到超时", sig)
		}
	}

	// 反向：脚本里出现的每个 signal 调用的阶段名都必须能在 M1 的信号表里查到。
	// 这条抓的是「发明了一个新信号名」——那种错误在渲染层看不出问题，机器
	// 打点时服务端会返回错误，而机器侧的 signal 函数刻意吞掉失败（遥测失败
	// 不杀装机），所以症状是「任务静默卡住」。
	for _, line := range strings.Split(got, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "signal ") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		phase := strings.Trim(fields[1], `"'`)
		if _, ok := model.LookupSignal(model.SignalKind(phase)); !ok {
			t.Errorf("脚本里调用了信号 %q，但它不在 M1 的信号表内 —— "+
				"服务端会拒收，而机器侧的 signal 函数刻意吞掉失败（遥测失败不杀装机），"+
				"症状是任务静默卡住直到超时", phase)
		}
	}
}

// TestImageWriteScript_TargetDiskFromServerDecision 验证目标盘来自服务端决策
// 的 by-id 路径，而不是脚本里猜一个 /dev/sda。
//
// SPEC §5.6 的先采集再服务端决策：盘的选择是服务端 DiskPlanner 的职责，
// 结果通过 fact 注入。脚本里写死 /dev/sda 会在「系统盘不是第一块」的机器上
// 擦掉错误的盘——而那是不可恢复的。
func TestImageWriteScript_TargetDiskFromServerDecision(t *testing.T) {
	got, err := ImageWriteScript(imageWriteContext(t))
	if err != nil {
		t.Fatalf("ImageWriteScript 失败: %v", err)
	}

	if !strings.Contains(got, string(model.FactDiskPrimary)) {
		t.Errorf("脚本没有引用 %s —— 目标盘必须来自服务端决策（§5.6），"+
			"由 fact 注入", model.FactDiskPrimary)
	}
	// 写死设备名的典型形态。
	for _, hardcoded := range []string{"of=/dev/sda", "of=/dev/nvme0n1", "of=/dev/vda"} {
		if strings.Contains(got, hardcoded) {
			t.Errorf("脚本里写死了目标盘 %q —— 在「系统盘不是第一块」的机器上"+
				"会擦掉错误的盘，且不可恢复", hardcoded)
		}
	}
}

// TestImageWriteScript_RejectsMissingImageArtifact 验证制品集不是镜像直写
// 通道、或缺 Image 三项时返回错误而不是渲染出一个残缺的脚本。
func TestImageWriteScript_RejectsMissingImageArtifact(t *testing.T) {
	t.Run("通道不是镜像直写", func(t *testing.T) {
		ctx := imageWriteContext(t)
		ctx.Artifact.Channel = model.ChannelInstaller
		if _, err := ImageWriteScript(ctx); err == nil {
			t.Error("installer 驱动通道的制品集被渲染成了写盘脚本 —— " +
				"两条通道的机制完全不同，混用会产出一个引导不起来的东西")
		}
	})

	t.Run("Image为nil", func(t *testing.T) {
		ctx := imageWriteContext(t)
		ctx.Artifact.Image = nil
		if _, err := ImageWriteScript(ctx); err == nil {
			t.Error("Image 为 nil 时仍渲染出脚本 —— 没有镜像路径与校验值，" +
				"脚本里那几个位置会是空字符串，机器会去下载一个空 URL")
		}
	})

	t.Run("没有唯一的根分区", func(t *testing.T) {
		ctx := imageWriteContext(t)
		ctx.Artifact.Image.PartitionLayout.Partitions = []model.Partition{
			{Number: 1, Filesystem: "vfat", Role: model.PartitionRoleESP},
		}
		if _, err := ImageWriteScript(ctx); err == nil {
			t.Error("布局里没有根分区时仍渲染出脚本 —— 第 4 步不知道该扩展哪个" +
				"分区，而盲目扩展一个分区是破坏性操作")
		}
	})
}

// TestImageWriteScript_FirstbootSharedWithInstallerChannel 验证 firstboot
// 脚手架与 installer 驱动通道用**同一套生成逻辑**。
//
// 这是两条通道唯一的共用部分，要保持（计划任务 4 第 5 步）：用户的 firstboot
// hook 在两条通道上行为必须一致，否则「同一个 hook 在镜像直写通道上跑不起来」
// 这类问题会让运维不敢用镜像直写通道。
func TestImageWriteScript_FirstbootSharedWithInstallerChannel(t *testing.T) {
	ctx := imageWriteContext(t)
	got, err := ImageWriteScript(ctx)
	if err != nil {
		t.Fatalf("ImageWriteScript 失败: %v", err)
	}

	// 直接比对：脚本里嵌的 firstboot 脚手架应当逐字符等于 FirstbootScaffold
	// 对同一个上下文的产出。不相等即说明镜像直写这条路径自己另写了一份，
	// 那两份迟早漂移。
	scaffold := FirstbootScaffold(ctx)
	if !strings.Contains(got, scaffold) {
		t.Errorf("写盘脚本里的 firstboot 脚手架与 FirstbootScaffold 的产出不一致 —— "+
			"两条通道的 firstboot 机制必须是同一套生成逻辑，各写一份迟早漂移，"+
			"症状是「同一个 firstboot hook 在镜像直写通道上行为不同」。\n"+
			"脚手架长度 %d，脚本长度 %d", len(scaffold), len(got))
	}
}
