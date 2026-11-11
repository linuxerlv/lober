package selection

import (
	"github.com/lober/lober/internal/domain/admission"
	"github.com/lober/lober/internal/domain/model"
)

// TemplateEligible 供可信调用方检查一个已选版本的候选资格，不认证用户。
// 输入为模板、它钉住的组件、应用已取得的ISO及本次发布清单；缺失引用沿用TemplateUsable语义。
// 唯一规则为CurrentBuiltin与admission.TemplateUsable，不重做激活，也不选择版本；返回布尔值，无错误先后。
// 纯计算不改输入，不写库、审计或通知；重复输入结果相同，无冲突、提交复查或取消操作。
func TemplateEligible(t model.Template, hooks map[model.HookKey]model.Hook, iso model.ISO, manifest Manifest) bool {
	return CurrentBuiltin(t, hooks, manifest) && len(admission.TemplateUsable(t, hooks, iso)) == 0
}
