package main

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/lober/lober/internal/core/model"
)

// fakeArtifactSetLister 是 artifactSetLister 的测试替身。
type fakeArtifactSetLister struct {
	sets []model.ArtifactSet
}

func (f fakeArtifactSetLister) ListArtifactSets(ctx context.Context) ([]model.ArtifactSet, error) {
	return f.sets, nil
}

// TestPreflightCheck_MissingInitrdReportsExactPathAndSetName 断言缺一个
// initrd 时精确报出那个路径与所属制品集名（计划任务 2「测试要锁住的
// 行为」）。
func TestPreflightCheck_MissingInitrdReportsExactPathAndSetName(t *testing.T) {
	set := model.ArtifactSet{
		ID: "set-1", Name: "rocky9.4", State: model.ArtifactSetReady,
		Paths: model.ArtifactPaths{
			Kernel: "vmlinuz",
			Initrd: []string{"initrd.img"}, // 故意不在文件系统里放这个文件
			Repo:   "repo/",
		},
	}
	files := fstest.MapFS{
		"set-1/vmlinuz":                  &fstest.MapFile{Data: []byte("k")},
		"set-1/repo/repodata/repomd.xml": &fstest.MapFile{Data: []byte("<repomd/>")},
	}

	issues, err := preflightCheck(context.Background(), fakeArtifactSetLister{sets: []model.ArtifactSet{set}}, files)
	if err != nil {
		t.Fatalf("preflightCheck 失败: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("期望恰好 1 条问题（缺 initrd），got %d: %+v", len(issues), issues)
	}
	got := issues[0]
	if got.ArtifactSetID != "set-1" || got.ArtifactSetName != "rocky9.4" {
		t.Errorf("问题未带正确的制品集标识: %+v", got)
	}
	if got.MissingPath != "set-1/initrd.img" {
		t.Errorf("MissingPath = %q, want %q——报告要带精确路径，不能只说『某个文件缺失』",
			got.MissingPath, "set-1/initrd.img")
	}
}

// TestPreflightCheck_OnlyReadyStateChecked 断言非 ready 状态的制品集一律
// 不报——三种状态（importing/failed/deprecated）各验一次（计划任务 2
// 「测试要锁住的行为」）。
func TestPreflightCheck_OnlyReadyStateChecked(t *testing.T) {
	// 三个制品集全部缺文件（连内核都不存在），但状态各不相同、且都不是
	// ready——若 preflightCheck 误检查了非 ready 状态，这里会报出问题。
	sets := []model.ArtifactSet{
		{ID: "importing-1", Name: "x", State: model.ArtifactSetImporting, Paths: model.ArtifactPaths{Kernel: "vmlinuz"}},
		{ID: "failed-1", Name: "y", State: model.ArtifactSetFailed, Paths: model.ArtifactPaths{Kernel: "vmlinuz"}},
		{ID: "deprecated-1", Name: "z", State: model.ArtifactSetDeprecated, Paths: model.ArtifactPaths{Kernel: "vmlinuz"}},
	}
	files := fstest.MapFS{} // 空文件系统——若被检查，三者都会报缺失

	issues, err := preflightCheck(context.Background(), fakeArtifactSetLister{sets: sets}, files)
	if err != nil {
		t.Fatalf("preflightCheck 失败: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("非 ready 状态的制品集不应被检查，但报出 %d 条问题: %+v", len(issues), issues)
	}
}

// TestPreflightCheck_CompleteArtifactSetProducesNoIssues 断言完整的制品集
// 不产生任何问题报告（含额外文件与仓库的 repodata），计划任务 2「测试要
// 锁住的行为」。
func TestPreflightCheck_CompleteArtifactSetProducesNoIssues(t *testing.T) {
	set := model.ArtifactSet{
		ID: "set-complete", Name: "esxi8", State: model.ArtifactSetReady,
		Paths: model.ArtifactPaths{
			Kernel: "mboot.efi",
			Initrd: []string{"initrd1.img", "initrd2.img"},
			Repo:   "repo/",
			Extra:  map[string]string{"boot_cfg": "boot.cfg"},
		},
	}
	files := fstest.MapFS{
		"set-complete/mboot.efi":                &fstest.MapFile{Data: []byte("k")},
		"set-complete/initrd1.img":              &fstest.MapFile{Data: []byte("i1")},
		"set-complete/initrd2.img":              &fstest.MapFile{Data: []byte("i2")},
		"set-complete/repo/repodata/repomd.xml": &fstest.MapFile{Data: []byte("<repomd/>")},
		"set-complete/boot.cfg":                 &fstest.MapFile{Data: []byte("cfg")},
	}

	issues, err := preflightCheck(context.Background(), fakeArtifactSetLister{sets: []model.ArtifactSet{set}}, files)
	if err != nil {
		t.Fatalf("preflightCheck 失败: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("完整的制品集不应产生任何问题报告，但报出: %+v", issues)
	}
}

// TestPreflightCheck_EmptyRepoDirNotEnough 断言仓库目录存在但缺
// repodata/repomd.xml 时仍报缺失——空目录能 stat 成功但对 installer
// 毫无用处，检查目录本身等于没检查（计划任务 2「设计要点」）。
func TestPreflightCheck_EmptyRepoDirNotEnough(t *testing.T) {
	set := model.ArtifactSet{
		ID: "set-emptyrepo", Name: "rocky9.4", State: model.ArtifactSetReady,
		Paths: model.ArtifactPaths{Kernel: "vmlinuz", Initrd: []string{"initrd.img"}, Repo: "repo/"},
	}
	files := fstest.MapFS{
		"set-emptyrepo/vmlinuz":    &fstest.MapFile{Data: []byte("k")},
		"set-emptyrepo/initrd.img": &fstest.MapFile{Data: []byte("i")},
		// repo/ 目录本身不存在任何文件（fstest.MapFS 不需要显式声明空
		// 目录，直接不放任何 repo/ 前缀的键即可模拟“目录不存在或为空”）。
	}

	issues, err := preflightCheck(context.Background(), fakeArtifactSetLister{sets: []model.ArtifactSet{set}}, files)
	if err != nil {
		t.Fatalf("preflightCheck 失败: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("期望恰好 1 条问题（仓库缺 repodata），got %d: %+v", len(issues), issues)
	}
	if issues[0].MissingPath != "set-emptyrepo/repo/repodata/repomd.xml" {
		t.Errorf("MissingPath = %q, want 指向 repodata/repomd.xml", issues[0].MissingPath)
	}
}
