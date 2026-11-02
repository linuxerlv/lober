package model

// AssetKind 是资产的种类。字面量会进日志与界面，发布后不能改名。
type AssetKind string

const (
	AssetHook     AssetKind = "hook"
	AssetTemplate AssetKind = "template"
	AssetISO      AssetKind = "iso"
)

// Asset 是「有种类、有 ID、有名字」的对象：hook、模板、ISO 都满足它。
//
// 刻意只声明三样。这三样是三者唯一真正共有的：它们都按 ID 寻址、都有给人看的名字、
// 都要求调用方知道拿到的是哪一类。版本号与状态看着像共有，其实不是——
// hook 与模板有版本号，ISO 没有，ISO 的 version 是介质自带的版本而不是记录的版本；
// 两者的状态取值也完全不同（draft/review/approved 对 acquiring/importing/ready）。
// 把这些塞进同一个接口，只会得到一串对某些类型没有意义的空实现。
//
// 用途只有一个：按种类分组、按名字查找这类与生命周期无关的操作。审批、派生、
// 导入、建任务都有自己的类型与规则，不经过这个接口。
type Asset interface {
	AssetKind() AssetKind
	AssetID() string
	AssetName() string
}

// AssetKind 报告这是一份 hook 版本。
func (h Hook) AssetKind() AssetKind { return AssetHook }

// AssetID 是 hook 的 ID，不含版本。
func (h Hook) AssetID() string { return h.ID }

// AssetName 是 hook 的名字。
func (h Hook) AssetName() string { return h.Name }

// AssetKind 报告这是一份模板版本。
func (t Template) AssetKind() AssetKind { return AssetTemplate }

// AssetID 是模板的 ID，不含版本。
func (t Template) AssetID() string { return t.ID }

// AssetName 是模板的名字。
func (t Template) AssetName() string { return t.Name }

// AssetKind 报告这是一份 ISO 记录。
func (i ISO) AssetKind() AssetKind { return AssetISO }

// AssetID 是 ISO 的编号。
func (i ISO) AssetID() string { return i.ID }

// AssetName 是 ISO 的名字。
func (i ISO) AssetName() string { return i.Name }
