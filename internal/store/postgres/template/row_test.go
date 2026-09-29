// 本文件测试 templateRow/templateHookRefRow 与 model.Template 之间的双向
// 映射，对应实现计划任务 9 的一半（template 聚合），依据 SPEC §2.3、§2.6、
// §2.7。往返测试只验证字段映射正确性，不碰数据库——index tag 是否写对由
// pgtest.NewDB 跑 AutoMigrate 的集成测试（store_test.go）负责，两者不能
// 互相替代（计划任务 3 同类风险提示对本包同样适用）。
package template

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/lober/lober/internal/core/model"
)

// TestTemplateRoundTrip 覆盖模板本体的映射往返：model.Template 转行结构体
// 再转回，必须与原始输入完全相等。覆盖四类输入：全字段填满、
// RequiredFormFields 为空 slice（非 nil）、RequiredFormFields 为 nil、
// HookRefs 为空。
func TestTemplateRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		tpl  model.Template
	}{
		{
			name: "全字段填满",
			tpl: model.Template{
				ID:                 "tpl-full",
				Name:               "rocky9-desktop",
				ArtifactSetID:      "artifact-rocky9",
				AnswerTemplate:     "# kickstart body",
				KernelArgs:         "console=ttyS0",
				DiskPolicy:         model.DiskPolicyLargest,
				OnFailure:          model.OnFailureShell,
				RequiredFormFields: []string{"hostname", "ip"},
				Version:            3,
				State:              model.TemplateStateActive,
			},
		},
		{
			name: "RequiredFormFields 为空 slice 非 nil",
			tpl: model.Template{
				ID:                 "tpl-empty-slice",
				Name:               "esxi-min",
				ArtifactSetID:      "artifact-esxi",
				AnswerTemplate:     "# ks body",
				DiskPolicy:         model.DiskPolicyFirst,
				OnFailure:          model.OnFailureReboot,
				RequiredFormFields: []string{},
				Version:            1,
				State:              model.TemplateStateDraft,
			},
		},
		{
			name: "RequiredFormFields 为 nil",
			tpl: model.Template{
				ID:             "tpl-nil-slice",
				Name:           "win-basic",
				ArtifactSetID:  "artifact-win",
				AnswerTemplate: "# unattend body",
				DiskPolicy:     model.DiskPolicyByModel,
				OnFailure:      model.OnFailureHalt,
				Version:        1,
				State:          model.TemplateStateDeprecated,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := fromModel(tc.tpl)
			got := row.toModel()
			if diff := cmp.Diff(tc.tpl, got); diff != "" {
				t.Errorf("往返映射不一致（-want +got）:\n%s", diff)
			}
		})
	}
}

// TestHookRefRoundTrip 覆盖 hook_refs 一行的映射往返，覆盖 ParamsDefaults
// 为 nil、为空 map、为非空 map 三类输入——nil 与空 map 必须能区分，否则
// 「没有默认参数」与「默认参数是个空对象」这两种不同的业务事实会被混淆
// （与 pgtype.StringMap 的 nil-vs-空 约定一致）。
// TestHookRefRoundTrip 验证 model.HookRef → row → model.HookRef 的往返。
//
// ParamsDefaults 这一列的往返**不是恒等映射**：空 map 会被折叠成 nil。
// 这是旧 SQLite 实现的既有行为（写入侧 nonNilStringMap 把 nil 归一成
// '{}'，读回侧 decodeJSONMapNilIfEmpty 把空对象归一成 nil，见
// sqlite/template.go），本次迁移只换实现不改行为，因此 want 与 ref 在这
// 一个用例上刻意不同——业务上「引用了 hook 但给了空参数表」与「没给参数
// 表」都是「无默认参数」，没有区分的必要。
func TestHookRefRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		ref  model.HookRef
		// want 为 nil 时表示期望与 ref 完全相同（恒等往返）；
		// 非 nil 时表示期望值与输入不同，需要显式写出。
		want *model.HookRef
	}{
		{
			name: "ParamsDefaults 非空",
			ref: model.HookRef{
				HookID:         "hook-a",
				HookDigest:     "digest-a-v1",
				Stage:          model.HookStagePre,
				Order:          1,
				Required:       true,
				ParamsDefaults: map[string]string{"disk": "/dev/sda"},
			},
		},
		{
			name: "ParamsDefaults 为空 map 被折叠成 nil",
			ref: model.HookRef{
				HookID:         "hook-b",
				HookDigest:     "digest-b-v1",
				Stage:          model.HookStagePost,
				Order:          2,
				Required:       false,
				ParamsDefaults: map[string]string{},
			},
			want: &model.HookRef{
				HookID:         "hook-b",
				HookDigest:     "digest-b-v1",
				Stage:          model.HookStagePost,
				Order:          2,
				Required:       false,
				ParamsDefaults: nil,
			},
		},
		{
			name: "ParamsDefaults 为 nil",
			ref: model.HookRef{
				HookID:     "hook-c",
				HookDigest: "digest-c-v1",
				Stage:      model.HookStageFirstboot,
				Order:      3,
				Required:   true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.ref
			if tc.want != nil {
				want = *tc.want
			}
			row := hookRefFromModel("tpl-x", 1, tc.ref)
			got := row.toModel()
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("往返映射不一致（-want +got）:\n%s", diff)
			}
		})
	}
}

// TestModels 断言 Models() 返回本聚合包的两个行结构体，供组合器收集给
// AutoMigrate（计划任务 9 接口契约：两个包各自导出 New 与 Models()）。
func TestModels(t *testing.T) {
	models := Models()
	if len(models) != 2 {
		t.Fatalf("Models() 返回 %d 个元素，want 2（templateRow 与 templateHookRefRow）", len(models))
	}
}
