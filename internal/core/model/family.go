// 本文件定义族类型 Family 与族-家族映射规则。对应四层解耦重构计划任务 3，
// 依据组件化 hook 与四层解耦设计文档。
package model

// Family 是三个 OS 族（linux/windows/esxi），是用户创建 ISO 制品时选的
// 分组标签——比 OSFamily 更粗的一层，供制品筛选与模板归属使用。
//
// 族不是家族的别名，两者服务于完全不同的目的：
//
//	OSFamily（六个值）决定应答文件格式：rhel→ks.cfg、debian→preseed.cfg、
//	ubuntu→user-data、suse→autoinst.xml、windows→unattend.xml、
//	esxi→boot.cfg（见 render.go 的 answerFileNames 表）。这是渲染器唯一
//	认的分派键，一行不改。
//
//	Family（三个值）只是分组、筛选与模板归属的标签。linux 族下的四家
//	不是同一格式的四种方言——kickstart 是行式加 %section 块、preseed 是
//	debconf 键值、autoinstall 是 YAML、autoyast 是 XML，没有任何写法能让
//	同一段文本同时合法。所以族**不能顶替家族**：任何试图用 Family 替代
//	OSFamily 去做格式分派的写法都是错的。
//
// 族与家族的来源也不同：族是用户创建 ISO 时**选的**（下拉框三选一），
// 家族是导入时从 ISO 元数据里**识别出来的**。两者独立产生，因此需要
// 交叉校验——选了 windows 族却识别出 rhel 家族，说明运维选错了文件，
// 见 ArtifactSet.FamilyConsistent（inventory.go）。
type Family string

const (
	FamilyLinux   Family = "linux"
	FamilyWindows Family = "windows"
	FamilyESXi    Family = "esxi"
)

// familyOSFamilies 是族到家族列表的映射表，唯一真源。family.go 内所有
// 函数（Valid、FamilyAllows、OSFamilies）与 FamilyOf 的反向查找都从这张
// 表派生，不各自维护一份判断逻辑——否则「linux 族允许哪些家族」这条规则
// 会散落成多处，改一处漏改另一处。
//
// map 字面量风格与 inventory.go 的 firmwareCompat 一致。
//
// 切片按稳定顺序手写（不从别处的 const 遍历生成），因为顺序本身是契约的
// 一部分：这个顺序会被 OSFamilies() 原样返回，供 UI 的 tab 排列与错误
// 文案使用，写错顺序不会导致编译或运行时错误，只会让人「感觉」不对，
// 所以在这里显式排好、由测试锁住（TestFamily_OSFamilies_Content /
// TestFamily_OSFamilies_OrderStable）。
var familyOSFamilies = map[Family][]OSFamily{
	FamilyLinux:   {OSFamilyRHEL, OSFamilyDebian, OSFamilyUbuntu, OSFamilySUSE},
	FamilyWindows: {OSFamilyWindows},
	FamilyESXi:    {OSFamilyESXi},
}

// Valid 判定该 Family 是否为三个合法取值之一。
//
// 空串不合法，与 HookStage.Valid（authoring.go）同一处置方向：族没有
// 合理默认值，猜一个会让 ISO 落到错的分组里，未知值不能静默接受。
//
// 这里刻意不写 `f == FamilyLinux || f == FamilyWindows || f == FamilyESXi`
// 这种手写枚举比较，而是查 familyOSFamilies——同一张表既定义「族允许哪些
// 家族」也定义「族本身合不合法」，两条规则不该有两份独立的真源。
func (f Family) Valid() bool {
	_, ok := familyOSFamilies[f]
	return ok
}

// osFamilyToFamily 是 familyOSFamilies 的反向索引，由 init 从正向表派生，
// 供 FamilyOf 做 O(1) 查找。不手写第二份映射：族与家族的对应关系只应该
// 有一处真源（familyOSFamilies），派生表只是它的另一种视图。
var osFamilyToFamily = func() map[OSFamily]Family {
	out := make(map[OSFamily]Family)
	for family, osFamilies := range familyOSFamilies {
		for _, osf := range osFamilies {
			out[osf] = family
		}
	}
	return out
}()

// FamilyOf 返回某 OS 家族所属的族：rhel/debian/ubuntu/suse → linux，
// windows → windows，esxi → esxi。未知家族返回 ok=false——与
// FirmwareCompatible、RepoIndexPattern 对未知家族的处置方向一致，不静默
// 接受（inventory.go）。
func FamilyOf(os OSFamily) (Family, bool) {
	family, ok := osFamilyToFamily[os]
	return family, ok
}

// FamilyAllows 判定某族是否允许某家族。族或家族任一非法都返回 false——
// 这是导入完成写回前的校验入口之一（另见 ArtifactSet.FamilyConsistent），
// 用户选了 windows 族却识别出 rhel 家族必须在这里被挡住，不能因为传参
// 里有一个非法值就放行。
func FamilyAllows(f Family, os OSFamily) bool {
	allowed, ok := familyOSFamilies[f]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == os {
			return true
		}
	}
	return false
}

// OSFamilies 返回该族允许的家族列表，顺序稳定（供 UI 的 tab 顺序与错误
// 文案使用，不稳定会让 golden 与快照测试随机失败）。非法族返回 nil。
//
// 返回新分配的切片，写法与 facts.go 的 FactKeysV1 一致——调用方对返回值
// 增删不得影响 familyOSFamilies 这张内部状态表，否则第一次调用篡改了
// 切片内容，第二次调用会看到被污染的数据。
func (f Family) OSFamilies() []OSFamily {
	allowed, ok := familyOSFamilies[f]
	if !ok {
		return nil
	}
	out := make([]OSFamily, len(allowed))
	copy(out, allowed)
	return out
}
