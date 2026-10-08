package store_test

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// 主机与 ISO 的入参检查：pgstore 调用的就是这一份，契约测试再在 pgstore 上跑一遍。

func TestCheckHosts合规则的通过(t *testing.T) {
	full := model.Host{SN: "SN-1", Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{"rack": "A1", "hostname": "机柜 A 的 1 号"}}
	wantErr(t, store.CheckHosts([]model.Host{full}), nil)
	wantErr(t, store.CheckHosts([]model.Host{full, {SN: "SN-2"}}), nil) // 架构、固件与属性都可空
}

// 空的一批、没规范化的 SN 是调用方的错，model 的校验对它们没有问题可报：报不带问题的 ErrInvalid。
func TestCheckHosts调用方的错报不带问题的ErrInvalid(t *testing.T) {
	for _, c := range []struct {
		name string
		hs   []model.Host
	}{
		{"一台都没有", nil},
		{"空的一批", []model.Host{}},
		{"SN 是小写", []model.Host{{SN: "sn-1"}}},
		{"SN 带着首尾空白", []model.Host{{SN: "SN-1"}, {SN: " SN-2 "}}},
	} {
		err := store.CheckHosts(c.hs)
		var pe *store.ProblemsError
		if !errors.Is(err, store.ErrInvalid) || errors.As(err, &pe) {
			t.Errorf("%s：应报不带问题的 ErrInvalid，得到 %v", c.name, err)
		}
	}
}

// 能用原因代码表达的问题与 model 的校验完全相同，按入参的顺序：每台先报批内重复（只在这个 SN 第一次出现时报一条），
// 再报 NormalizeSN 与 Host.Validate 的问题。
func TestCheckHosts的问题与model的校验相同(t *testing.T) {
	bad := model.Host{SN: "SN-2", Arch: "X86_64", Attrs: map[string]string{"Rack": "A1", "ip": "\xff"}}
	placeholder := model.Host{SN: "0000000000"}
	_, err := model.NormalizeSN(placeholder.SN)
	var snProblem model.Problem
	if !errors.As(err, &snProblem) {
		t.Fatalf("反例没造对：%s 应过不了 NormalizeSN，得到 %v", placeholder.SN, err)
	}
	dup := func(sn string) model.Problem {
		return model.NewProblem("host_duplicate_in_batch", model.Subject{Kind: "host", ID: sn})
	}
	hs := []model.Host{{SN: "SN-1"}, bad, placeholder, {SN: "SN-1"}, bad}
	want := slices.Concat([]model.Problem{dup("SN-1"), dup("SN-2")}, bad.Validate(), []model.Problem{snProblem}, bad.Validate())
	wantCodesOf(t, want, "host_duplicate_in_batch", "host_duplicate_in_batch", "host_platform_invalid", "attr_key_invalid",
		"text_not_storable", "sn_placeholder", "host_platform_invalid", "attr_key_invalid", "text_not_storable")
	wantProblemsErr(t, store.CheckHosts(hs), store.ErrInvalid, want)
}

func TestCheckHostQuery(t *testing.T) {
	for _, c := range []struct {
		q    store.HostQuery
		want int
	}{
		{store.HostQuery{}, 100},
		{store.HostQuery{SNPrefix: "SN-", Limit: 5}, 5},
		{store.HostQuery{AttrKey: "rack", AttrValue: "A1", Limit: 500}, 500},
	} {
		if n, err := store.CheckHostQuery(c.q); err != nil || n != c.want {
			t.Errorf("%+v 应取 %d 条，得到 %d、%v", c.q, c.want, n, err)
		}
	}
	for _, q := range []store.HostQuery{
		{AttrKey: "rack"}, {AttrValue: "A1"}, // 属性的键与值要么都给，要么都不给
		{Limit: 501}, {Limit: -1}, {Cursor: "a\x00"},
	} {
		_, err := store.CheckHostQuery(q)
		wantInvalid(t, err, fmt.Sprintf("%+v", q))
	}
}

func TestCheckNewISO(t *testing.T) {
	good := model.ISO{Name: "Rocky Linux 9.6 安装盘", Family: "linux", Series: "rocky"}
	wantErr(t, store.CheckNewISO(good), nil)
	for _, c := range []struct {
		name string
		mod  func(iso *model.ISO)
	}{
		// 其余字段由仓储与导入填，空的 map 也算填了：调用方多半是把别的记录当成了新建的入参
		{"带了 ID", func(iso *model.ISO) { iso.ID = "iso-1" }},
		{"带了版本", func(iso *model.ISO) { iso.Version = "9.6" }},
		{"带了架构", func(iso *model.ISO) { iso.Arch = "x86_64" }},
		{"带了状态", func(iso *model.ISO) { iso.State = model.ISOAcquiring }},
		{"带了 SHA256", func(iso *model.ISO) { iso.SHA256 = strings.Repeat("ab", 32) }},
		{"带了空的制品", func(iso *model.ISO) { iso.Artifacts = map[string]string{} }},
		{"带了空的提供的集合", func(iso *model.ISO) { iso.Offers = map[string][]model.Option{} }},
		{"带了 Rev", func(iso *model.ISO) { iso.Rev = 1 }},
		{"系不属于族", func(iso *model.ISO) { iso.Family = "windows" }},
		{"族不是目录里的写法", func(iso *model.ISO) { iso.Family = "Linux" }},
		{"系不是目录里的写法", func(iso *model.ISO) { iso.Series = "Rocky" }},
		{"族与系都没给", func(iso *model.ISO) { iso.Family, iso.Series = "", "" }},
		{"名称含 NUL", func(iso *model.ISO) { iso.Name = "Rocky\x00" }},
		{"名称不是 UTF-8", func(iso *model.ISO) { iso.Name = "Rocky\xff" }},
	} {
		iso := good
		c.mod(&iso)
		wantInvalid(t, store.CheckNewISO(iso), c.name)
	}
}

func TestCheckISOUpdate(t *testing.T) {
	for _, st := range []model.ISOState{model.ISOAcquiring, model.ISOImporting, model.ISOReady, model.ISOFailed} {
		wantErr(t, store.CheckISOUpdate(model.ISO{ID: "iso-1", State: st}), nil)
	}
	for _, c := range []struct {
		name string
		iso  model.ISO
	}{
		{"ID 不合格式", model.ISO{ID: "ISO 1", State: model.ISOImporting}},
		{"置为已废弃：废弃要审计，走 DeprecateISO", model.ISO{ID: "iso-1", State: model.ISODeprecated}},
		{"状态为空", model.ISO{ID: "iso-1"}},
		{"状态不认识", model.ISO{ID: "iso-1", State: "Ready"}},
	} {
		wantInvalid(t, store.CheckISOUpdate(c.iso), c.name)
	}
}

// importedISO 是导入中的 ISO 在库里的样子，readyISO 是对它的一次合规则的系统更新：导入完成，记录完整。
func importedISO() model.ISO {
	return model.ISO{ID: "iso-1", Name: "Rocky 9.6", Family: "linux", Series: "rocky", State: model.ISOImporting, Rev: 2}
}

func readyISO() model.ISO {
	iso := importedISO()
	iso.Version, iso.Arch, iso.State, iso.SHA256 = "9.6", "x86_64", model.ISOReady, strings.Repeat("5a", 32)
	iso.Artifacts = map[string]string{"kernel": "rocky/9.6/x86_64/vmlinuz"}
	iso.Offers = map[string][]model.Option{"profiles": {{Value: "minimal", Label: "最小安装"}}}
	return iso
}

func TestCheckISOUpdateFrom(t *testing.T) {
	old := importedISO()
	wantErr(t, store.CheckISOUpdateFrom(old, readyISO()), nil)
	failed := old
	failed.State = model.ISOFailed // 导入失败的记录不全，不跑完整性判定
	wantErr(t, store.CheckISOUpdateFrom(old, failed), nil)

	deprecated := readyISO()
	deprecated.State = model.ISODeprecated
	if err := store.CheckISOUpdateFrom(deprecated, readyISO()); !errors.Is(err, store.ErrImmutable) {
		t.Errorf("已废弃的 ISO 应报 ErrImmutable：系统更新不能把废弃撤掉，得到 %v", err)
	}
	for _, c := range []struct {
		name string
		mod  func(iso *model.ISO)
	}{
		{"改了族", func(iso *model.ISO) { iso.Family = "windows" }},
		{"改了系", func(iso *model.ISO) { iso.Series = "almalinux" }},
		{"改了名称", func(iso *model.ISO) { iso.Name = "Rocky 9.7" }},
	} {
		iso := readyISO()
		c.mod(&iso)
		wantInvalid(t, store.CheckISOUpdateFrom(old, iso), c.name)
	}

	// C5：置为 ready 的必须通过 ISO.Validate，问题原样带出
	incomplete := readyISO()
	incomplete.Version, incomplete.Arch = "", "arm"
	want := incomplete.Validate()
	wantCodesOf(t, want, "iso_incomplete", "iso_incomplete")
	wantProblemsErr(t, store.CheckISOUpdateFrom(old, incomplete), store.ErrInvalid, want)
	incomplete.State = model.ISOImporting // 还在导入的不跑完整性判定
	wantErr(t, store.CheckISOUpdateFrom(old, incomplete), nil)

	// 存不下的文本不论置为什么状态都报：Validate 不看制品与 SHA256
	for _, st := range []model.ISOState{model.ISOReady, model.ISOImporting, model.ISOFailed} {
		iso := readyISO()
		iso.State, iso.Artifacts["initrd"] = st, "initrd\x00.img"
		wantInvalid(t, store.CheckISOUpdateFrom(old, iso), "制品存不下，置为 "+string(st))
	}
}

// 写进来的文本每一处都要存得下（spec §3 总则）：PostgreSQL 的 text 与 jsonb 都不收 NUL，jsonb 还会把非法的 UTF-8 换掉。
func TestCheckISOText(t *testing.T) {
	wantErr(t, store.CheckISOText(readyISO()), nil)
	for _, c := range []struct {
		name string
		mod  func(iso *model.ISO)
	}{
		{"版本", func(iso *model.ISO) { iso.Version = "9.6\x00" }},
		{"架构", func(iso *model.ISO) { iso.Arch = "x86\xff" }},
		{"SHA256", func(iso *model.ISO) { iso.SHA256 = "ab\x00" }},
		{"制品的角色", func(iso *model.ISO) { iso.Artifacts["\xff"] = "a" }},
		{"制品的路径", func(iso *model.ISO) { iso.Artifacts["kernel"] = "a\x00" }},
		{"提供的集合名", func(iso *model.ISO) { iso.Offers["a\x00"] = nil }},
		{"选项的值", func(iso *model.ISO) { iso.Offers["profiles"] = []model.Option{{Value: "\xff", Label: "最小安装"}} }},
		{"选项的显示名", func(iso *model.ISO) { iso.Offers["profiles"] = []model.Option{{Value: "minimal", Label: "最小\x00"}} }},
	} {
		iso := readyISO()
		c.mod(&iso)
		wantInvalid(t, store.CheckISOText(iso), c.name+"存不下")
	}
}

// C4：内容相同的只导入一次。空的 SHA256 与导入失败的 ISO 不占位，导入失败的可以记下与别的 ISO 相同的内容、指向那一个；
// 已废弃的照样占着，内容相同的不能再导入一份。
func TestHoldsSHA256(t *testing.T) {
	sum := strings.Repeat("5a", 32)
	for _, c := range []struct {
		iso  model.ISO
		want bool
	}{
		{model.ISO{SHA256: sum, State: model.ISOReady}, true},
		{model.ISO{SHA256: sum, State: model.ISOImporting}, true},
		{model.ISO{SHA256: sum, State: model.ISODeprecated}, true},
		{model.ISO{SHA256: sum, State: model.ISOFailed}, false},
		{model.ISO{State: model.ISOReady}, false},
	} {
		if got := store.HoldsSHA256(c.iso); got != c.want {
			t.Errorf("SHA256 %q、状态 %s：占不占位应为 %v，得到 %v", c.iso.SHA256, c.iso.State, c.want, got)
		}
	}
}

func TestCheckISODeprecation(t *testing.T) {
	wantErr(t, store.CheckISODeprecation("iso-1", model.ISOReady), nil)
	for _, st := range []model.ISOState{model.ISOAcquiring, model.ISOImporting, model.ISOFailed, model.ISODeprecated} {
		if err := store.CheckISODeprecation("iso-1", st); !errors.Is(err, store.ErrConflict) {
			t.Errorf("%s 的 ISO 废弃应报 ErrConflict，得到 %v", st, err)
		}
	}
}

// wantCodesOf 要求 ps 的代码依次是 codes：用来确认反例造对了。
func wantCodesOf(t *testing.T, ps []model.Problem, codes ...model.Code) {
	t.Helper()
	if got := codesOf(ps); !slices.Equal(got, codes) {
		t.Fatalf("反例没造对：问题的代码是 %v，应为 %v", got, codes)
	}
}

// wantProblemsErr 要求 err 是包着 kind 的 ProblemsError，问题依次就是 want。
func wantProblemsErr(t *testing.T, err, kind error, want []model.Problem) {
	t.Helper()
	var pe *store.ProblemsError
	if !errors.As(err, &pe) || pe.Kind != kind {
		t.Fatalf("应报 ProblemsError(%v)，得到 %v", kind, err)
	}
	if !reflect.DeepEqual(pe.Problems, want) {
		t.Errorf("问题 = %v\n期望 %v", pe.Problems, want)
	}
}
