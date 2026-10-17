// 本文件是外部测试包：同时 import activation 与 params，核对两边对同一格的定位（C-3）。依赖守卫只查
// 非测试源文件，activation 不依赖 params 这条规则不受影响。
package params_test

import (
	"slices"
	"testing"

	"github.com/lober/lober/internal/core/activation"
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/params"
)

// 同一格在激活判定与参数求值里标在同一处（C-3）：作者审批时看到的红格，与操作员建任务时看到的是同一格，
// 第 5、6 层才能按同一套 Path 定位。期望不经字段键的构造函数去算：两边的 Path 先互相比，再与写死的写法
// 比，构造函数改坏了、两边一起错也逃不过。
//
// 模板参数与实例参数各有一格锁在 IPv6 上，正文与片段又取 .netmask，这与主机无关，激活与求值都报
// param_accessor_invalid；第二个实例引用的 hook 不存在，两边都报 hook_ref_missing，落在实例上。
func Test同一格在激活判定与参数求值里标在同一处(t *testing.T) {
	nic := model.Hook{
		ID: "nic", Version: 1, Name: "Static-NIC", State: model.VersionApproved, Author: "alice", Approver: "bob",
		Params: []model.ParamDecl{{Name: "addr", Label: "地址与前缀", Type: "cidr"}},
		Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: model.ImplFragment,
			Body: "network --bootproto=static --ip={{param:addr.addr}} --netmask={{param:addr.netmask}}\n"}},
	}
	tpl := model.Template{
		ID: "net-base", Version: 2, Name: "Net-Base", State: model.VersionApproved, Author: "alice", Approver: "bob",
		Family: "linux", Series: []catalog.Series{"rocky"},
		Body: "text\n{{lober:syslog}}\nnetwork --gateway={{param:gw.addr}} --netmask={{param:gw.netmask}}\n" +
			"{{slot:网络}}\n%post\n{{lober:firstboot}}\n{{lober:cleanup}}\n%end\n",
		Params:   []model.ParamDecl{{Name: "gw", Label: "网关", Type: "cidr"}},
		Bindings: map[string]model.Binding{"gw": {Source: model.BindingFixed, Value: "2001:db8::1/64"}},
		Assembly: map[string][]model.Instance{"网络": {
			{HookID: "nic", HookVersion: 1, Bindings: map[string]model.Binding{
				"addr": {Source: model.BindingFixed, Value: "2001:db8::5/64"},
			}},
			{HookID: "gone", HookVersion: 1},
		}},
	}
	hooks := map[model.HookKey]model.Hook{nic.Key(): nic}
	iso := model.ISO{ID: "iso-rocky96", Name: "Rocky Linux 9.6", Family: "linux", Series: "rocky",
		Version: "9.6", Arch: "x86_64", State: model.ISOReady}

	act := activation.Template(tpl, hooks)
	ev := params.Evaluate(params.Input{Template: tpl, Hooks: hooks, ISO: iso, Host: model.Host{SN: "SN-1"}})
	wantCodes := []model.Code{"param_accessor_invalid", "param_accessor_invalid", "hook_ref_missing"}
	wantPaths := []string{"tpl/gw", "网络/1/addr", "网络/2"}
	for _, side := range []struct {
		name     string
		problems []model.Problem
	}{{"激活判定", act}, {"参数求值", ev.Problems}} {
		var codes []model.Code
		var paths []string
		for _, p := range side.problems {
			codes, paths = append(codes, p.Code), append(paths, p.Subject.Path)
		}
		if !slices.Equal(codes, wantCodes) || !slices.Equal(paths, wantPaths) {
			t.Errorf("%s报出 %v，Path 依次是 %q；应为 %v，Path 依次是 %q", side.name, codes, paths, wantCodes, wantPaths)
		}
	}
	// 落在格子上的问题，Path 就是表单里那一格的键。
	for _, key := range wantPaths[:2] {
		if !slices.ContainsFunc(ev.Fields, func(f params.Field) bool { return string(f.Key) == key }) {
			t.Errorf("表单里没有键为 %q 的格子", key)
		}
	}
}
