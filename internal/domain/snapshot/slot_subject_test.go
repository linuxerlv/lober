// 本文件是外部测试包：同时 import activation 与 snapshot，核对两边对正文里插槽问题的定位。依赖守卫只查
// 非测试源文件，snapshot 不依赖 activation 这条规则不受影响。
package snapshot_test

import (
	"reflect"
	"testing"

	"github.com/lober/lober/internal/domain/activation"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/snapshot"
)

// 插槽重名、执行插槽的时机该格式不支持，激活判定与快照构建都报，问题标在同一处：作者审批时看到的红标，
// 与预览、建任务被拒时看到的是同一处，第 5、6 层才能按同一套 Subject 定位。两边各自对写死的字面量断言的话，
// 只改一边的 Subject 另一边照样绿，所以这里拿同一份草稿过两边，先互相比，再与写死的写法比。
//
// 正文按 unattend 写：装后在 post-target 上，该格式不支持；它在行内又写了一次，换成 post-installer，该格式
// 同样不支持，这一处既重名、时机又不支持，两条的先后也要两边一致。其余都合规则，激活判定也就只报这几条
// 插槽的问题。
func Test插槽的问题在激活判定与快照构建里标在同一处(t *testing.T) {
	s := catalog.Series("windows-server")
	tpl := model.Template{
		ID: "win-base", Version: 2, Name: "Win-Base", State: model.VersionDraft, Author: "alice",
		Family: s.Family(), Series: []catalog.Series{s},
		Body: "{{lober:firstboot}}\n{{lober:cleanup}}\n{{slot:装后@post-target}}\n{{slot:装前@pre}}\nrem {{slot:装后@post-installer}}\n",
	}
	iso := model.ISO{ID: "iso-win2025", Name: "Windows Server 2025", Family: s.Family(), Series: s,
		Version: "2025", Arch: "x86_64", State: model.ISOReady}

	act := activation.Template(tpl, nil)
	snap, built := snapshot.Build(params.Input{Template: tpl, ISO: iso, Host: model.Host{SN: "SN-1"}})
	if !reflect.DeepEqual(snap, model.Snapshot{}) {
		t.Errorf("有问题时不应产出快照，得到 %+v", snap)
	}

	at := func(code model.Code, line, col int) model.Problem {
		return model.Problem{Code: code, Subject: model.Subject{Kind: "slot", ID: "win-base@2", Path: "装后", Line: line, Col: col}}
	}
	want := []model.Problem{at("slot_timing_unsupported", 3, 1), at("slot_duplicate", 5, 5), at("slot_timing_unsupported", 5, 5)}
	located := func(ps []model.Problem) []model.Problem {
		out := make([]model.Problem, len(ps))
		for i, p := range ps {
			out[i] = model.Problem{Code: p.Code, Subject: p.Subject}
		}
		return out
	}
	gotAct, gotBuild := located(act), located(built)
	if !reflect.DeepEqual(gotAct, gotBuild) {
		t.Errorf("两边标的位置不同：\n激活判定 %v\n快照构建 %v", gotAct, gotBuild)
	}
	for _, side := range []struct {
		name string
		got  []model.Problem
	}{{"激活判定", gotAct}, {"快照构建", gotBuild}} {
		if !reflect.DeepEqual(side.got, want) {
			t.Errorf("%s报出\n%v\n应为\n%v", side.name, side.got, want)
		}
	}
}
