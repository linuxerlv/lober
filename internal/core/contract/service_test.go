package contract

import (
	"errors"
	"strings"
	"testing"
)

// TestNoTaskScript_ContainsSanboot 锁住 §10 失败模式 1 的唯一保护：无任务时
// 返回的 iPXE 脚本必须含 `sanboot --no-describe --drive 0x80` 这一行。
//
// 为什么这条重要（SPEC §4.2、§10 失败模式 1）：BIOS/MBR 模式下没有
// efibootmgr，装完的机器完全依赖这一行 sanboot 回到本地盘——如果这行
// 丢失或拼写走样，已装好的机器每次网络优先重启都会卡在这里起不来，
// 而这个故障只会在生产环境里第一次机器重启时才会暴露，本地测不出来。
func TestNoTaskScript_ContainsSanboot(t *testing.T) {
	script := NoTaskScript("SN12345")

	const wantLine = "sanboot --no-describe --drive 0x80"
	if !strings.Contains(script, wantLine) {
		t.Fatalf("NoTaskScript 缺少保护性引导行：got %q，want 包含 %q（SPEC §4.2、§10 失败模式 1：\n"+
			"BIOS/MBR 机器装完后完全依赖这一行才能在下次重启时回到本地盘，\n"+
			"缺了它意味着这类机器会在生产环境里真实卡死）", script, wantLine)
	}
}

// TestNoTaskScript_IsValidIPXEShebang 锁住脚本以 #!ipxe shebang 开头。
//
// 为什么这条重要（SPEC §4.2）：iPXE 脚本若不以 #!ipxe 开头，固件会把整个
// 响应当作二进制内核尝试执行，机器会在无任何日志的情况下卡死或重启，
// 现场表现与本节想避免的所有故障一样难排查。
func TestNoTaskScript_IsValidIPXEShebang(t *testing.T) {
	script := NoTaskScript("SN12345")

	if !strings.HasPrefix(script, "#!ipxe\n") {
		t.Fatalf("NoTaskScript 未以 #!ipxe 开头：got %q，want 以 %q 开头（SPEC §4.2：\n"+
			"iPXE 固件按此 shebang 识别脚本，缺了它会被当作二进制内核加载失败）", script, "#!ipxe\n")
	}
}

// TestNoTaskScript_EchoesSerial 锁住 echo 行携带机器序列号。
//
// 为什么这条重要（SPEC §4.2）：no_task 决策刻意不建任务、不记事件、不告警
// （唯一留痕是 nginx access log），echo 行是运维在机器自己的控制台上唯一
// 能看到的诊断信息，若不带序列号，运维在多机并发场景下无法确认是哪台
// 机器命中了 no_task。
func TestNoTaskScript_EchoesSerial(t *testing.T) {
	script := NoTaskScript("SN-ABC-001")

	if !strings.Contains(script, "SN-ABC-001") {
		t.Fatalf("NoTaskScript 未回显序列号：got %q，want 包含 %q（SPEC §4.2：\n"+
			"no_task 决策不落任何数据库记录，echo 行是运维唯一能在机器控制台\n"+
			"上确认命中来源的诊断信息）", script, "SN-ABC-001")
	}
}

// TestPrecheckError_TypeAssertRecoversFailure 锁住计划任务 11 明确要求的
// 可测逻辑：precheck 错误能被类型断言取出「哪一项不过」。
//
// 为什么这条重要（SPEC §4.6）：运维看到的错误必须能精确到「这台机器是
// BIOS，任务要装 ESXi」这个级别，而不是一句笼统的「precheck 失败」。
// 如果 PrecheckError 不能被类型断言取出 Failure 字段，M5（网关）与
// M6（控制台）在展示失败原因时就只能退化成裸的 error.Error() 字符串
// 拼接，任何未来给 Detail 加前缀/后缀的改动都会悄悄改变运维看到的
// 诊断依据。
func TestPrecheckError_TypeAssertRecoversFailure(t *testing.T) {
	var err error = &PrecheckError{
		Failure: PrecheckFailureFirmware,
		Detail:  "机器上报 pcbios，任务要求 uefi",
	}

	var precheckErr *PrecheckError
	if !errors.As(err, &precheckErr) {
		t.Fatalf("errors.As 未能从 error 中取出 *PrecheckError：got err=%v（SPEC §4.6：\n"+
			"调用方必须能用类型断言拿到具体是哪一项校验没通过，不能只有一句\n"+
			"笼统的错误文案，否则运维无法得到「这台机器是 BIOS，任务要装 ESXi」\n"+
			"这种精确原因）", err)
	}
	if precheckErr.Failure != PrecheckFailureFirmware {
		t.Fatalf("取出的 Failure 字段不对：got %q，want %q（SPEC §4.6：\n"+
			"三项校验——arch 一致性、固件模式兼容、制品完整性——各自的失败\n"+
			"必须能被区分，混淆了就无法定位到具体是哪一层出的问题）",
			precheckErr.Failure, PrecheckFailureFirmware)
	}
	if precheckErr.Detail == "" {
		t.Fatalf("Detail 字段为空（SPEC §4.6：precheck 错误必须带可直接展示给" +
			"运维的具体说明，不能只有分类标签没有可读文案）")
	}
}

// TestPrecheckError_ErrorMessageIncludesFailureKind 锁住 Error() 输出里含
// 有失败分类，即便调用方没有做类型断言（比如只是打日志），日志里也能看出
// 是哪一类 precheck 失败。
func TestPrecheckError_ErrorMessageIncludesFailureKind(t *testing.T) {
	err := &PrecheckError{Failure: PrecheckFailureArch, Detail: "x86_64 制品发给了 arm64 机器"}

	msg := err.Error()
	if !strings.Contains(msg, string(PrecheckFailureArch)) {
		t.Fatalf("Error() 输出未包含失败分类：got %q，want 包含 %q（SPEC §4.6：\n"+
			"就算调用方偷懒直接打印 error 而不做类型断言，日志里也应该能看出\n"+
			"是哪一类校验没通过，否则排查时退化成裸文本搜索）", msg, PrecheckFailureArch)
	}
}
