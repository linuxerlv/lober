package main

import (
	"context"
	"strings"
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
		OSFamily: model.OSFamilyRHEL,
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
		ID: "set-complete", Name: "rocky9.4-全路径种类", State: model.ArtifactSetReady,
		// 家族取 RHEL 而不是 ESXi：本夹具的用途是「四种路径种类（内核、
		// 多个 initrd、仓库、额外文件）一次全覆盖」，因此必须选一个**确实
		// 有仓库树**的家族。真实 ESXi 制品集的 Repo 恒为空串（只产出 Extra
		// 里的一组文件，见 imports/extract_esxi.go），配上 Repo: "repo/"
		// 是自相矛盾的，会让读者以为 ESXi 有仓库判据。
		OSFamily: model.OSFamilyRHEL,
		Paths: model.ArtifactPaths{
			Kernel: "vmlinuz",
			Initrd: []string{"initrd1.img", "initrd2.img"},
			Repo:   "repo/",
			Extra:  map[string]string{"boot_cfg": "boot.cfg"},
		},
	}
	files := fstest.MapFS{
		"set-complete/vmlinuz":                  &fstest.MapFile{Data: []byte("k")},
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

// TestPreflightCheck_DebianAPTRepoProducesNoIssues 断言启动自检认得 APT 的
// 仓库索引入口，不对 Debian 制品集误报缺失。
//
// 与 internal/statemachine 侧那条同源：判据此前是散落三处的同一条常量
// repodata/repomd.xml（RPM-MD 专属），Debian 的 repo/ 下只有 dists/ 与
// pool/ 两棵 APT 树。这一处的后果与引导时校验不同但同样实在——启动自检会
// 对每一个 ready 的 Debian 制品集报一条「文件缺失」，运维每次重启服务都看到
// 一批假告警，久了就会开始忽略这份报告，真缺文件时也一起被忽略。
func TestPreflightCheck_DebianAPTRepoProducesNoIssues(t *testing.T) {
	set := model.ArtifactSet{
		ID: "set-debian13", Name: "debian13", State: model.ArtifactSetReady,
		OSFamily: model.OSFamilyDebian,
		Paths:    model.ArtifactPaths{Kernel: "linux", Initrd: []string{"initrd.gz"}, Repo: "repo/"},
	}
	files := fstest.MapFS{
		"set-debian13/linux":                     &fstest.MapFile{Data: []byte("k")},
		"set-debian13/initrd.gz":                 &fstest.MapFile{Data: []byte("i")},
		"set-debian13/repo/dists/trixie/Release": &fstest.MapFile{Data: []byte("Suite: stable\n")},
	}

	issues, err := preflightCheck(context.Background(), fakeArtifactSetLister{sets: []model.ArtifactSet{set}}, files)
	if err != nil {
		t.Fatalf("preflightCheck 失败: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("一份 dists/<suite>/Release 齐备的 Debian 制品集不应产生任何问题报告，但报出: %+v —— "+
			"APT 的仓库索引入口不是 repodata/repomd.xml，不能沿用 RPM-MD 那条判据", issues)
	}
}

// TestPreflightCheck_DebianRepoWithoutReleaseReportsIssue 断言 Debian 仓库
// 只有 pool/ 而没有任何 dists/<suite>/Release 时仍报缺失——换判据不能换成
// 「有 repo 目录就算过」，那是同一个 bug 的另一个方向。
func TestPreflightCheck_DebianRepoWithoutReleaseReportsIssue(t *testing.T) {
	set := model.ArtifactSet{
		ID: "set-debnorelease", Name: "debian13", State: model.ArtifactSetReady,
		OSFamily: model.OSFamilyDebian,
		Paths:    model.ArtifactPaths{Kernel: "linux", Initrd: []string{"initrd.gz"}, Repo: "repo/"},
	}
	files := fstest.MapFS{
		"set-debnorelease/linux":                                      &fstest.MapFile{Data: []byte("k")},
		"set-debnorelease/initrd.gz":                                  &fstest.MapFile{Data: []byte("i")},
		"set-debnorelease/repo/pool/main/b/bash/bash_5.2-5_amd64.deb": &fstest.MapFile{Data: []byte("deb")},
	}

	issues, err := preflightCheck(context.Background(), fakeArtifactSetLister{sets: []model.ArtifactSet{set}}, files)
	if err != nil {
		t.Fatalf("preflightCheck 失败: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("期望恰好 1 条问题（APT 仓库缺 dists/<suite>/Release），got %d: %+v", len(issues), issues)
	}
	if !strings.Contains(issues[0].MissingPath, "dists/*/Release") {
		t.Errorf("MissingPath = %q, want 含 APT 的索引入口判据 dists/*/Release —— "+
			"报告要让运维直接看出「期望的是哪个文件」，报成 repodata/repomd.xml 会把人引到"+
			"一个 Debian 仓库永远不该有的路径上去", issues[0].MissingPath)
	}
}

// TestPreflightCheck_UnknownFamilyWithRepoReportsIssue 断言判据表里没有条目
// 的家族带着非空 Repo 时报问题，而不是静默放行（§9.1 默认拒绝）。
func TestPreflightCheck_UnknownFamilyWithRepoReportsIssue(t *testing.T) {
	set := model.ArtifactSet{
		ID: "set-unknownfam", Name: "mystery", State: model.ArtifactSetReady,
		OSFamily: model.OSFamily("plan9"),
		Paths:    model.ArtifactPaths{Repo: "repo/"},
	}
	files := fstest.MapFS{
		"set-unknownfam/repo/somefile": &fstest.MapFile{Data: []byte("x")},
	}

	issues, err := preflightCheck(context.Background(), fakeArtifactSetLister{sets: []model.ArtifactSet{set}}, files)
	if err != nil {
		t.Fatalf("preflightCheck 失败: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("期望恰好 1 条问题（未知家族无法确认仓库可用），got %d: %+v", len(issues), issues)
	}
}

// TestPreflightCheck_EmptyRepoDirNotEnough 断言仓库目录存在但缺
// repodata/repomd.xml 时仍报缺失——空目录能 stat 成功但对 installer
// 毫无用处，检查目录本身等于没检查（计划任务 2「设计要点」）。
func TestPreflightCheck_EmptyRepoDirNotEnough(t *testing.T) {
	set := model.ArtifactSet{
		ID: "set-emptyrepo", Name: "rocky9.4", State: model.ArtifactSetReady,
		OSFamily: model.OSFamilyRHEL,
		Paths:    model.ArtifactPaths{Kernel: "vmlinuz", Initrd: []string{"initrd.img"}, Repo: "repo/"},
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
