package catalog

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestLinuxSupportScope(t *testing.T) {
	got := SeriesOf(FamilyLinux)
	slices.Sort(got)
	want := []Series{"almalinux", "debian", "rhel", "rocky", "ubuntu"}
	if !slices.Equal(got, want) {
		t.Fatalf("Linux 系 = %q，想要恰好 %q", got, want)
	}
	for _, f := range []Format{"autoyast", "agama"} {
		if f.Valid() || len(f.Timings()) != 0 || len(DeliveryFiles(f)) != 0 {
			t.Errorf("已停用格式 %q 仍可执行", f)
		}
	}
}

func TestRetiredSeriesRemainReadableAsHistory(t *testing.T) {
	for _, value := range []string{"sles15", "leap15", "sles16", "leap16", "openeuler", "opencloudos"} {
		var got Series
		if err := json.Unmarshal([]byte(`"`+value+`"`), &got); err != nil || string(got) != value {
			t.Fatalf("历史系 %q 读取失败：%q，%v", value, got, err)
		}
		if got.Valid() {
			t.Errorf("历史系 %q 仍可新建安装", got)
		}
	}
}
