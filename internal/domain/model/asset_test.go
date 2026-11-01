package model

import (
	"sort"
	"testing"
)

// 三个类型都要满足 Asset，且取出来的东西要与各自的字段一致。
func Test资产三样都满足接口(t *testing.T) {
	var assets []Asset = []Asset{
		Hook{ID: "builtin-partition", Version: 2, Name: "lober:partition"},
		Template{ID: "builtin-kickstart", Version: 3, Name: "lober:kickstart"},
		ISO{ID: "znah4xjxydsib3ckq2j4cspzom", Name: "AlmaLinux 10.2 Minimal"},
	}
	want := []struct {
		kind AssetKind
		id   string
		name string
	}{
		{AssetHook, "builtin-partition", "lober:partition"},
		{AssetTemplate, "builtin-kickstart", "lober:kickstart"},
		{AssetISO, "znah4xjxydsib3ckq2j4cspzom", "AlmaLinux 10.2 Minimal"},
	}
	for i, a := range assets {
		if a.AssetKind() != want[i].kind || a.AssetID() != want[i].id || a.AssetName() != want[i].name {
			t.Fatalf("第 %d 个：%q %q %q", i, a.AssetKind(), a.AssetID(), a.AssetName())
		}
	}
}

// ID 不含版本号：同一个 hook 的两个版本，AssetID 相同。
func Test资产ID不含版本号(t *testing.T) {
	v1 := Hook{ID: "builtin-partition", Version: 1}
	v2 := Hook{ID: "builtin-partition", Version: 2}
	if v1.AssetID() != v2.AssetID() {
		t.Fatal("版本号混进了 AssetID")
	}
	t1 := Template{ID: "builtin-kickstart", Version: 1}
	t2 := Template{ID: "builtin-kickstart", Version: 5}
	if t1.AssetID() != t2.AssetID() {
		t.Fatal("版本号混进了 AssetID")
	}
}

// 三种资产的种类字面量互不相同，否则按种类分组会把两类混成一组。
func Test资产种类互不相同(t *testing.T) {
	seen := map[AssetKind]bool{}
	for _, k := range []AssetKind{AssetHook, AssetTemplate, AssetISO} {
		if seen[k] {
			t.Fatalf("种类 %q 重复", k)
		}
		seen[k] = true
	}
}

// 按种类分组是这个接口的唯一用途，这里守住它确实能分出三组。
func Test按种类分组(t *testing.T) {
	assets := []Asset{
		Hook{ID: "h1", Name: "h1"},
		Template{ID: "t1", Name: "t1"},
		ISO{ID: "i1", Name: "i1"},
		Hook{ID: "h2", Name: "h2"},
	}
	groups := map[AssetKind][]string{}
	for _, a := range assets {
		groups[a.AssetKind()] = append(groups[a.AssetKind()], a.AssetID())
	}
	if len(groups) != 3 {
		t.Fatalf("分成了 %d 组", len(groups))
	}
	for _, kind := range []AssetKind{AssetHook, AssetTemplate, AssetISO} {
		sort.Strings(groups[kind])
	}
	if got := groups[AssetHook]; len(got) != 2 || got[0] != "h1" || got[1] != "h2" {
		t.Fatalf("hook 组 = %v", got)
	}
}
