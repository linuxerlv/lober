// 本文件测试磁盘 dry-run 端点。对应实现计划任务 8「测试要锁住的行为」
// dry-run 部分。
package console

import (
	"context"
	"net/http"
	"testing"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
)

// TestDryRun_ReturnsLayoutAndNonEmptyReasoning 锁住「dry-run 返回布局与
// 说明，说明非空」。
func TestDryRun_ReturnsLayoutAndNonEmptyReasoning(t *testing.T) {
	ts := newTestServer(t)
	ts.Server.deps.DiskPlanner = &stubDiskPlanner{
		planFunc: func(ctx context.Context, disks []contract.Disk, intent contract.DiskIntent) (contract.Layout, error) {
			return contract.Layout{Selected: []contract.LayoutDisk{
				{Disk: contract.Disk{ByIDPath: "/dev/disk/by-id/b", SizeBytes: 2000}, Reason: "容量最大"},
			}}, nil
		},
	}
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/disk-policy/dryrun", tok, map[string]any{
		"diskPolicy": "largest",
		"facts":      map[string]string{"LOBER_DISKS": "/dev/disk/by-id/a,1000,true,ModelA;/dev/disk/by-id/b,2000,true,ModelB"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("dry-run 失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data diskPlanResult `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Data.Reasoning == "" {
		t.Errorf("dry-run 的说明不应为空")
	}
}

// TestDryRun_UnknownPolicyRejected 锁住「未知策略名被拒」。
func TestDryRun_UnknownPolicyRejected(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/disk-policy/dryrun", tok, map[string]any{
		"diskPolicy": "not-a-real-policy",
	})
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeBadRequest) {
		t.Errorf("未知策略名应回 CodeBadRequest(%d)，实际 code=%d", apierr.CodeBadRequest, env.Code)
	}
}
