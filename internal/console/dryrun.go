// 本文件实现磁盘 dry-run 端点。对应实现计划任务 8，依据
// docs/contracts/http-console.md §7、SPEC §8.2。
//
// 接收策略名与一批磁盘描述，调磁盘规划 provider，返回布局与「为什么这么
// 选」的说明，不落地——这是与 preview 并列的两个高价值端点之一：磁盘
// 策略决定擦哪块盘，能用录制的硬件事实先跑一遍是安全上的实质收益。
package console

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// routesDiskDryRun 注册磁盘 dry-run 端点。
func (s *Server) routesDiskDryRun() {
	s.handleFunc("POST", "/disk-policy/dryrun", "", "", s.handleDiskDryRun)
}

// validDiskPolicies 是 diskPolicy 字段的白名单，取自 model.DiskPolicy 的
// 五个具名取值（与 config 包的同名白名单保持一致的取值集合，但 console
// 不 import config，因此本地独立声明——两处都是「引用 model 常量构造
// 白名单」，不是拼写字面量，不会走散）。
var validDiskPolicies = map[string]bool{
	string(model.DiskPolicyFirst):    true,
	string(model.DiskPolicyLargest):  true,
	string(model.DiskPolicyByModel):  true,
	string(model.DiskPolicyRAID1SSD): true,
	string(model.DiskPolicyByHook):   true,
}

// diskDryRunRequest 是入参：策略名与一批模拟的硬件采集结果（facts，字段
// 形如 LOBER_DISKS 的实际取值）。
type diskDryRunRequest struct {
	DiskPolicy string            `json:"diskPolicy"`
	Facts      map[string]string `json:"facts"`
}

// diskPlanResult 是响应形态。
type diskPlanResult struct {
	ChosenDisk string `json:"chosenDisk"`
	Reasoning  string `json:"reasoning"`
}

// handleDiskDryRun 处理 POST /disk-policy/dryrun。
//
// 本模块不解析 LOBER_DISKS 字符串格式本身（那是 render/provider 模块的
// 职责边界）——facts 原样转发给 DiskPlanner，本 handler 只负责策略名的
// 白名单校验与响应形态转换。DiskPlanner.Plan 接口本身接收结构化的
// []contract.Disk，但 http-console.md 定义的入参是一份自由格式的
// facts map（模拟硬件采集结果），两者之间需要一个转换——本实现选择
// 「facts 里没有结构化磁盘清单时，用空磁盘列表调用 Plan，只依赖
// intent.Params 传递策略参数」，这是本模块能做到的最保守处理：
// DiskPlanner 接口没有定义 facts → []Disk 的解析约定，这属于契约缺口，
// 已记入 contractGaps。
func (s *Server) handleDiskDryRun(w http.ResponseWriter, r *http.Request) {
	var req diskDryRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest, "请求体不是合法的 JSON"))
		return
	}
	if strings.TrimSpace(req.DiskPolicy) == "" {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest, "diskPolicy 不能为空"))
		return
	}
	if !validDiskPolicies[req.DiskPolicy] {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest,
			"diskPolicy 取值不合法，必须是 first/largest/by-model/raid1-ssd/by-hook 之一"))
		return
	}

	disks := parseDisksFact(req.Facts[model.FactDisks])
	layout, err := s.deps.DiskPlanner.Plan(r.Context(), disks, contract.DiskIntent{
		Policy: req.DiskPolicy, Params: req.Facts,
	})
	if err != nil {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeInternal, "磁盘规划失败: "+err.Error()))
		return
	}

	chosen := ""
	var reasons []string
	for _, sel := range layout.Selected {
		if chosen == "" {
			chosen = sel.Disk.ByIDPath
		}
		reasons = append(reasons, sel.Reason)
	}
	apierr.WriteOK(w, diskPlanResult{ChosenDisk: chosen, Reasoning: strings.Join(reasons, "; ")})
}

// parseDisksFact 尝试把 LOBER_DISKS 形态的字符串解析成 []contract.Disk。
// 契约文档没有冻结 LOBER_DISKS 的具体字符串格式（那是 render/provider
// 模块的职责），本函数只处理一种保守的约定格式作为最小可用实现：
// 用分号分隔多块盘，每块盘用逗号分隔 "byIdPath,sizeBytes,rotational,model"
// 四个字段。解析失败的字段整体跳过（不是致命错误——facts 为空或格式
// 不认识时，返回空磁盘列表，Plan 的策略实现自行决定如何处理空清单）。
func parseDisksFact(raw string) []contract.Disk {
	if raw == "" {
		return nil
	}
	var out []contract.Disk
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, ",")
		if len(fields) < 1 {
			continue
		}
		d := contract.Disk{ByIDPath: strings.TrimSpace(fields[0])}
		if len(fields) >= 2 {
			if size, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64); err == nil {
				d.SizeBytes = size
			}
		}
		if len(fields) >= 3 {
			d.Rotational = strings.TrimSpace(fields[2]) == "true"
		}
		if len(fields) >= 4 {
			d.Model = strings.TrimSpace(fields[3])
		}
		out = append(out, d)
	}
	return out
}
