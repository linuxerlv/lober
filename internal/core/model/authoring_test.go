package model

import (
	"reflect"
	"slices"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
)

// sampleHook 有两份实现：rocky 与 almalinux 共用一份 kickstart 片段，debian 与 ubuntu
// 共用一份装后脚本。同一个功能在不同的系可以是不同种类（spec §2.4）。
func sampleHook() Hook {
	return Hook{
		ID:          "ssh-key",
		Version:     2,
		Name:        "SSH 公钥",
		Description: "给 root 写入登录用的公钥",
		State:       VersionApproved,
		Author:      "alice",
		Approver:    "bob",
		Builtin:     true,
		Params:      []ParamDecl{{Name: "key", Label: "公钥", Type: "sshkey"}},
		TimeoutSec:  300,
		Required:    true,
		Impls: []Impl{
			{
				Family: "linux",
				Series: []catalog.Series{"rocky", "almalinux"},
				Kind:   ImplFragment,
				Body:   "sshkey --username=root \"{{param:key}}\"\n",
			},
			{
				Family:      "linux",
				Series:      []catalog.Series{"debian", "ubuntu"},
				Kind:        ImplScript,
				Body:        "#!/bin/sh\nmkdir -p /root/.ssh\necho \"$LOBER_PARAM_KEY\" >>/root/.ssh/authorized_keys\n",
				Interpreter: "sh",
				Timings:     []catalog.Timing{"post-target"},
			},
		},
	}
}

// 这些字面量会存进数据库和接口，发布后不能改名。
func TestAuthoring取值的字面量(t *testing.T) {
	if got, want := []VersionState{VersionDraft, VersionReview, VersionApproved, VersionDeprecated},
		[]VersionState{"draft", "review", "approved", "deprecated"}; !slices.Equal(got, want) {
		t.Errorf("版本状态的字面量是 %q，应为 %q", got, want)
	}
	if got, want := []ImplKind{ImplFragment, ImplScript}, []ImplKind{"fragment", "script"}; !slices.Equal(got, want) {
		t.Errorf("实现种类的字面量是 %q，应为 %q", got, want)
	}
	if got, want := []BindingSource{BindingFixed, BindingAttr, BindingDefault},
		[]BindingSource{"fixed", "attr", "default"}; !slices.Equal(got, want) {
		t.Errorf("取值来源的字面量是 %q，应为 %q", got, want)
	}
}

// 版本状态的显示名给说明与界面用（T5-1）：每个状态都有，是合格的显示名；不认识的为空串，由调用方
// 回落到标识。
func TestVersionState的显示名(t *testing.T) {
	want := map[VersionState]string{
		VersionDraft: "草稿", VersionReview: "待审批", VersionApproved: "已审批", VersionDeprecated: "已废弃",
	}
	for s, label := range want {
		if got := s.Label(); got != label || !ValidLabel(got) {
			t.Errorf("%q.Label() = %q，应为 %q", s, got, label)
		}
	}
	for _, s := range []VersionState{"", "Approved", "approve", "published", "draft "} {
		if got := s.Label(); got != "" {
			t.Errorf("不认识的状态 %q 的显示名应为空，得到 %q", s, got)
		}
	}
}

// 这几个取值类型都会进数据库，界面上选不出不认识的值，它们只会来自直接调接口或库里的坏数据
// （T7-2）。第 2 层建 CHECK 约束、第 5 层保存时拒收，都只调这里的 Valid，不另抄取值表。
func TestAuthoring取值类型的Valid只认各自的常量(t *testing.T) {
	for _, s := range []VersionState{VersionDraft, VersionReview, VersionApproved, VersionDeprecated} {
		if !s.Valid() {
			t.Errorf("VersionState %q.Valid() 应为 true", s)
		}
	}
	for _, s := range []VersionState{"", "Approved", "approve", "published", "draft "} {
		if s.Valid() {
			t.Errorf("VersionState %q.Valid() 应为 false", s)
		}
	}
	for _, k := range []ImplKind{ImplFragment, ImplScript} {
		if !k.Valid() {
			t.Errorf("ImplKind %q.Valid() 应为 true", k)
		}
	}
	for _, k := range []ImplKind{"", "Fragment", "SCRIPT", "macro", "script "} {
		if k.Valid() {
			t.Errorf("ImplKind %q.Valid() 应为 false", k)
		}
	}
	for _, s := range []BindingSource{BindingFixed, BindingAttr, BindingDefault} {
		if !s.Valid() {
			t.Errorf("BindingSource %q.Valid() 应为 true", s)
		}
	}
	// 零值也不算：JSON 里漏了 Source 的取值来源，不能被当成哪一种。
	for _, s := range []BindingSource{"", "Fixed", "attribute", "default "} {
		if s.Valid() {
			t.Errorf("BindingSource %q.Valid() 应为 false", s)
		}
	}
}

// FitsSlot 是插槽适配唯一的一份判定（L-7），激活判定和快照构建都调它：片段只进片段插槽；脚本只进
// 执行插槽，且要声明这个插槽的时机。种类不对时不再看时机：错的是种类，不连带报时机。
func TestImplFitsSlot(t *testing.T) {
	script := Impl{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: ImplScript,
		Body: "#!/bin/sh\ntrue\n", Interpreter: "sh", Timings: []catalog.Timing{"pre", "post-target"}}
	fragment := Impl{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: ImplFragment, Body: "part / --grow\n"}
	type fitCase struct {
		name   string
		impl   Impl
		exec   bool
		timing catalog.Timing
		want   Code
	}
	cases := []fitCase{
		{"脚本进声明了该时机的执行插槽", script, true, "post-target", ""},
		{"脚本进没声明该时机的执行插槽", script, true, "firstboot", "impl_timing_mismatch"},
		{"片段进执行插槽", fragment, true, "pre", "impl_kind_mismatch"},
		{"片段进片段插槽", fragment, false, "", ""},
		{"脚本进片段插槽", script, false, "", "impl_kind_mismatch"},
		// 片段插槽没有时机，传了也不看。
		{"片段插槽不看时机", fragment, false, "firstboot", ""},
	}
	for _, kind := range []ImplKind{"", "macro", "Script", "FRAGMENT"} {
		impl := script
		impl.Kind = kind
		cases = append(cases,
			fitCase{"种类「" + string(kind) + "」进执行插槽", impl, true, "firstboot", "impl_kind_mismatch"},
			fitCase{"种类「" + string(kind) + "」进片段插槽", impl, false, "", "impl_kind_mismatch"},
		)
	}
	for _, c := range cases {
		if got := c.impl.FitsSlot(c.exec, c.timing); got != c.want {
			t.Errorf("%s：FitsSlot(%v, %q) = %q，应为 %q", c.name, c.exec, c.timing, got, c.want)
		}
	}
}

func TestHookKey由ID与版本组成(t *testing.T) {
	h := sampleHook()
	if got, want := h.Key(), (HookKey{ID: "ssh-key", Version: 2}); got != want {
		t.Errorf("Key() = %+v，应为 %+v", got, want)
	}
	// 模板引用的是具体版本（C23）：同一个 hook 的新版本是另一个键。
	next := sampleHook()
	next.Version = 3
	if h.Key() == next.Key() {
		t.Errorf("v2 与 v3 得到了同一个键 %+v", h.Key())
	}
}

func TestHookImplFor按系取覆盖它的实现(t *testing.T) {
	h := sampleHook()
	for s, idx := range map[catalog.Series]int{"rocky": 0, "almalinux": 0, "debian": 1, "ubuntu": 1} {
		got, ok := h.ImplFor(s)
		if !ok || !reflect.DeepEqual(got, h.Impls[idx]) {
			t.Errorf("ImplFor(%q) = %+v, %v；应为第 %d 份实现", s, got, ok, idx+1)
		}
	}
	// 同族没覆盖的系、别族的系、大小写不同的写法、未知的系都取不到。
	for _, s := range []catalog.Series{"rhel", "opencloudos", "sles15", "windows-server", "esxi", "Rocky", "centos", ""} {
		if got, ok := h.ImplFor(s); ok || !reflect.DeepEqual(got, Impl{}) {
			t.Errorf("ImplFor(%q) 应取不到，得到 %+v, %v", s, got, ok)
		}
	}
	if _, ok := (Hook{ID: "empty", Version: 1}).ImplFor("rocky"); ok {
		t.Error("没有实现的 hook 不应取到实现")
	}
}

// 同一个系出现在两份实现里是激活判定（任务 9）要拦的错误。这里不另报错，只按实现的
// 顺序取第一份，结果是确定的。
func TestHookImplFor同一个系在两份实现里时取第一份(t *testing.T) {
	h := Hook{ID: "pkgs", Version: 1, Impls: []Impl{
		{Family: "linux", Series: []catalog.Series{"rhel"}, Kind: ImplFragment, Body: "first\n"},
		{Family: "linux", Series: []catalog.Series{"rocky", "rhel"}, Kind: ImplFragment, Body: "second\n"},
	}}
	for s, want := range map[catalog.Series]string{"rhel": "first\n", "rocky": "second\n"} {
		if got, ok := h.ImplFor(s); !ok || got.Body != want {
			t.Errorf("ImplFor(%q) 取到 %q, %v，应为 %q", s, got.Body, ok, want)
		}
	}
}

// 返回的实现是副本（全局约定）：调用方改了它的系或时机，hook 本身不变。
func TestHookImplFor返回副本(t *testing.T) {
	h := sampleHook()
	got, ok := h.ImplFor("debian")
	if !ok {
		t.Fatal("应取到覆盖 debian 的脚本")
	}
	got.Series[0] = "rhel"
	got.Timings[0] = "pre"
	if !reflect.DeepEqual(h, sampleHook()) {
		t.Errorf("改动 ImplFor 返回的实现改到了 hook 本身：%+v", h.Impls[1])
	}
}

// 期望值由 coreutils 的 sha256sum 算出（printf '…' | sha256sum）。机器侧的运行脚本用它校验
// 下载到的 hook 脚本，两边对同样的字节必须得到同样的串，所以换行一律不规范化。
func TestImplDigest与sha256sum一致(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"空正文", "", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"LF 换行且结尾有换行", "#!/bin/sh\necho ok\n", "b4d644d4279594903f1a9911956432d9473041f2984fc6014c14d7402c7d126c"},
		{"CRLF 换行", "#!/bin/sh\r\necho ok\r\n", "65c09b27287ef3597a218fc2fd7d1602265826209a6809b5064ec1821576bd13"},
		{"结尾没有换行", "#!/bin/sh\necho ok", "7dc2f3638241e16a628b268b9c3fd41e5531951d5bcf07c438039f09e2d403dd"},
		{"UTF-8 按字节算", "echo 装机\n", "333986a2bfbd658ad8b04c0ae0f34fc3d4c288fcc419563ca995643734581d2b"},
	}
	for _, c := range cases {
		if got := (Impl{Kind: ImplScript, Body: c.body}).Digest(); got != c.want {
			t.Errorf("%s：Digest() = %s，sha256sum 的结果是 %s", c.name, got, c.want)
		}
	}
}

// Format 只看所勾的系：族与系是否相符、系是否合法，由激活判定查。
func TestTemplateFormat(t *testing.T) {
	cases := []struct {
		name   string
		series []catalog.Series
		want   catalog.Format
		ok     bool
	}{
		{"没勾系", nil, "", false},
		{"勾一个", []catalog.Series{"debian"}, "preseed", true},
		{"勾同格式的多个", []catalog.Series{"rhel", "rocky", "almalinux", "openeuler", "opencloudos"}, "kickstart", true},
		{"勾同格式的两个 Windows 系", []catalog.Series{"windows-server", "windows-desktop"}, "unattend", true},
		{"勾了不同格式的", []catalog.Series{"rocky", "debian"}, "", false},
		{"同族不同格式", []catalog.Series{"sles15", "sles16"}, "", false}, // AutoYaST 与 Agama
		{"不同格式的系排在后面", []catalog.Series{"leap16", "sles16", "ubuntu"}, "", false},
		{"未知的系", []catalog.Series{"centos"}, "", false},
		{"混进大小写不同的写法", []catalog.Series{"rocky", "Rocky"}, "", false},
	}
	for _, c := range cases {
		got, ok := (Template{ID: "tpl", Version: 1, Series: c.series}).Format()
		if got != c.want || ok != c.ok {
			t.Errorf("%s：Format() = %q, %v；应为 %q, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

// tplVersion 造一个模板版本，只填 CurrentApproved 要看的版本与状态。
func tplVersion(v int, s VersionState) Template {
	return Template{ID: "baseline", Version: v, Name: "Linux 基线", State: s}
}

func TestCurrentApproved取已审批的最高版本(t *testing.T) {
	cases := []struct {
		name     string
		versions []Template
		want     int // 应取到的版本，0 表示取不到
	}{
		{"没有版本", nil, 0},
		{"全是草稿", []Template{
			tplVersion(1, VersionDraft), tplVersion(2, VersionReview), tplVersion(3, VersionDraft),
		}, 0},
		{"审批过的都废弃了", []Template{
			tplVersion(1, VersionDeprecated), tplVersion(2, VersionDeprecated), tplVersion(3, VersionDraft),
		}, 0},
		{"v5 已审批", []Template{
			tplVersion(1, VersionApproved), tplVersion(2, VersionDeprecated), tplVersion(3, VersionApproved),
			tplVersion(4, VersionApproved), tplVersion(5, VersionApproved),
		}, 5},
		// 最新版被废弃后回落到上一个已审批版本（C26）。
		{"v5 已废弃而 v4 已审批", []Template{
			tplVersion(3, VersionApproved), tplVersion(4, VersionApproved), tplVersion(5, VersionDeprecated),
		}, 4},
		// 新版本审批之前，旧版本照常可用（spec §5.2）。
		{"新版本还没审批", []Template{
			tplVersion(4, VersionApproved), tplVersion(5, VersionReview), tplVersion(6, VersionDraft),
		}, 4},
		{"版本乱序传入", []Template{
			tplVersion(2, VersionApproved), tplVersion(7, VersionDraft), tplVersion(5, VersionApproved),
			tplVersion(1, VersionApproved), tplVersion(6, VersionDeprecated), tplVersion(3, VersionApproved),
		}, 5},
	}
	for _, c := range cases {
		got, ok := CurrentApproved(c.versions)
		if c.want == 0 {
			if ok || !reflect.DeepEqual(got, Template{}) {
				t.Errorf("%s：不应取到版本，得到 v%d（%s）, %v", c.name, got.Version, got.State, ok)
			}
			continue
		}
		if !ok || got.Version != c.want || got.State != VersionApproved {
			t.Errorf("%s：取到 v%d（%s）, %v，应为 v%d", c.name, got.Version, got.State, ok, c.want)
		}
	}
}

// 派生记下的是来源的具体版本（C24），内置标记随版本走（C25）：取当前版本时两者都原样带出，
// 新建的模板没有来源。
func TestCurrentApproved原样带出派生来源与内置标记(t *testing.T) {
	builtin := tplVersion(3, VersionApproved)
	builtin.ID, builtin.Builtin = "builtin-kickstart", true
	derived := tplVersion(1, VersionApproved)
	derived.DerivedFrom = &TemplateKey{ID: "builtin-kickstart", Version: 3}
	for _, v := range []Template{builtin, derived} {
		got, ok := CurrentApproved([]Template{v})
		if !ok || got.Builtin != v.Builtin || !reflect.DeepEqual(got.DerivedFrom, v.DerivedFrom) {
			t.Errorf("%s@%d：内置标记或派生来源没有原样带出：%+v, %v", v.ID, v.Version, got, ok)
		}
	}
}

// fullTemplate 填上模板里每一个切片、map 和指针，复制测试因此能覆盖到它们。
func fullTemplate() Template {
	timeout, required := 600, false
	return Template{
		ID:          "baseline",
		Version:     4,
		Name:        "Linux 基线",
		State:       VersionApproved,
		Author:      "alice",
		Approver:    "bob",
		DerivedFrom: &TemplateKey{ID: "builtin-kickstart", Version: 3},
		Family:      "linux",
		Series:      []catalog.Series{"rocky", "almalinux"},
		Body:        "text\n{{lober:syslog}}\n{{slot:分区}}\n%post\n{{slot:装后配置@post-target}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n%end\n",
		Params: []ParamDecl{
			{Name: "profile", Label: "安装方式", Type: "enum", Options: []Option{{Value: "server", Label: "服务器"}}},
			{Name: "swap_gb", Label: "交换分区（GB）", Type: "int", Default: "4", Min: int64p(0), Max: int64p(64)},
		},
		Bindings: map[string]Binding{"profile": {Source: BindingFixed, Value: "server"}},
		Assembly: map[string][]Instance{
			"分区": {
				{HookID: "lvm", HookVersion: 1, Bindings: map[string]Binding{
					"disk": {Source: BindingAttr, Attr: "disk", Editable: true},
				}},
			},
			"装后配置": {
				{HookID: "nic", HookVersion: 2, TimeoutSec: &timeout, Required: &required, Bindings: map[string]Binding{
					"ip": {Source: BindingAttr, Attr: "ip"},
				}},
				{HookID: "nic", HookVersion: 2, Bindings: map[string]Binding{
					"ip": {Source: BindingDefault, Editable: true},
				}},
			},
		},
	}
}

// 返回的模板是副本（全局约定）：调用方改了它的切片、map 或指针指向的值，入参里的版本不变。
func TestCurrentApproved返回副本(t *testing.T) {
	versions := []Template{tplVersion(3, VersionApproved), fullTemplate()}
	got, ok := CurrentApproved(versions)
	if !ok || !reflect.DeepEqual(got, fullTemplate()) {
		t.Fatalf("应原样取到 v4，得到 %+v, %v", got, ok)
	}
	got.DerivedFrom.Version = 99
	got.Series[0] = "rhel"
	got.Params[0].Options[0].Value = "desktop"
	*got.Params[1].Min = 99
	*got.Params[1].Max = 99
	got.Bindings["profile"] = Binding{}
	part := got.Assembly["分区"]
	part[0].Bindings["disk"] = Binding{}
	post := got.Assembly["装后配置"]
	*post[0].TimeoutSec = 1
	*post[0].Required = true
	post[1] = Instance{}
	got.Assembly["新插槽"] = nil
	if want := []Template{tplVersion(3, VersionApproved), fullTemplate()}; !reflect.DeepEqual(versions, want) {
		t.Errorf("改动 CurrentApproved 返回的模板改到了入参：%+v", versions[1])
	}
}
