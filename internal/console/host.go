// 本文件实现主机清单两个端点：查询（带文本搜索与分页）、录入。对应实现
// 计划任务 3，依据 docs/contracts/http-console.md §2、SPEC §4.3、§7.4。
//
// MVP 没有 discovery image，主机清单只能手工录入——§7.4 认可这条路径
// （Inventory provider 失败时允许手工兜底）。这个列表是建任务页 SN 字段
// 的唯一数据源（§4.3 硬要求）。
package console

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/model"
)

// routesHost 注册主机清单两个端点。
func (s *Server) routesHost() {
	// 查询只读，已登录（含 viewer）即可；录入需要 artifact:import——手工
	// 录入是「运维手工补数据」的兜底路径，与制品导入同一类操作，不为此
	// 单独发明一个权限码。
	s.handleFunc("GET", "/hosts", "", "", s.handleListHosts)
	s.handleFunc("POST", "/hosts", codeArtifactImport, "host.create", s.handleCreateHost)
}

// hostListItem 是主机清单一行的响应形态，字段名与 model.Host 逐一对应
// （契约约定 1：不另起名字）。
//
// Id 字段是本模块对「主机清单 id」这一契约概念的具体落地方式——见本文件
// 末尾 hostID 函数的说明与 contractGaps 记录：model.Host 本身没有 ID
// 字段，本实现取 SN（回退 MAC，再回退 UUID）作为对外暴露的 id，与
// fake.Store 内部 hostKey() 的判优先级完全一致，因此「建任务传入的
// hostId」与「Store 内部按身份查找」两条路径的判据不会走散。
type hostListItem struct {
	ID         string `json:"id"`
	SN         string `json:"sn"`
	MAC        string `json:"mac"`
	UUID       string `json:"uuid"`
	Arch       string `json:"arch"`
	Firmware   string `json:"firmware"`
	Rack       string `json:"rack"`
	LastSeenAt int64  `json:"lastSeenAt"`
}

// hostID 计算暴露给人面 API 的主机 id：SN → MAC → UUID 优先级取第一个
// 非空值，与 internal/core/fake 包内部 hostKey() 的判优先级一致（该函数
// 未导出，本模块无法直接调用，只能镜像同一套逻辑——已记入 contractGaps：
// model.Host 缺一个显式的服务端主键字段，contract.HostStore.GetHost 的
// 文档也明确说明「id 的取法由实现决定，本接口不规定主机主键形态」，因此
// 本模块不能假设 GetHost 认识 hostID() 算出的这个字符串本身）。
func hostID(h model.Host) string {
	if h.SN != "" {
		return h.SN
	}
	if h.MAC != "" {
		return h.MAC
	}
	return h.UUID
}

// findHostByExposedID 按 hostID() 算出的字符串反查 Host。
//
// 不能直接调用 HostStore.GetHost(id)——GetHost 的 id 取法是「由实现决定」
// （contract.HostStore 文档明确说明），fake.Store 的具体实现用一个带前缀
// 的内部 key（"sn:xxx"/"mac:xxx"/"uuid:xxx"），与 hostID() 返回的裸值不
// 是同一个字符串，SQLite 实现的主键形态目前也未知。本函数改用
// FindHostByIdentity——它的判据是 contract 明确文档化的 SN→MAC→UUID
// 回退（identityMatches 语义），任何符合契约的 Store 实现都必须支持这个
// 判据，因此这是唯一能在不依赖具体实现细节的前提下，把 hostID() 算出的
// 值反查回 Host 的方法。
//
// identityMatches 的语义是「一旦某层 query 非空就只判定那一层」，不会
// 在 SN 不匹配时自动降级去试 MAC，因此这里必须依次用「只填一个字段」的
// 三次独立调用去试，而不是一次性把 id 同时填进三个参数（那样只会命中
// SN 分支，MAC/UUID 为主键的主机永远查不到）。
func (s *Server) findHostByExposedID(ctx context.Context, id string) (model.Host, error) {
	if h, err := s.deps.Hosts.FindHostByIdentity(ctx, id, "", ""); err == nil {
		return h, nil
	}
	if h, err := s.deps.Hosts.FindHostByIdentity(ctx, "", id, ""); err == nil {
		return h, nil
	}
	return s.deps.Hosts.FindHostByIdentity(ctx, "", "", id)
}

func toHostListItem(h model.Host) hostListItem {
	return hostListItem{
		ID:         hostID(h),
		SN:         h.SN,
		MAC:        h.MAC,
		UUID:       h.UUID,
		Arch:       string(h.Arch),
		Firmware:   string(h.Firmware),
		Rack:       h.Rack,
		LastSeenAt: h.LastSeenAt,
	}
}

// defaultListLimit 是列表端点未传 limit 参数时的默认上限（契约约定 2：
// 防止一次拉出几万行，不是给分页器用）。
const defaultListLimit = 1000

// handleListHosts 处理 GET /hosts?q=&limit=&offset=。
//
// 文本搜索在本 handler 层做，不下推到 Store（M1 契约已说明）：取回后在
// Go 里对 SN/MAC/机架位做大小写不敏感的子串匹配，主机数是百级，这个代价
// 可以接受。
func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.deps.Hosts.ListHosts(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}

	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := parseLimit(r.URL.Query().Get("limit"), defaultListLimit)
	offset := parseNonNegativeInt(r.URL.Query().Get("offset"), 0)

	matched := make([]model.Host, 0, len(hosts))
	for _, h := range hosts {
		if q != "" && !hostMatchesQuery(h, q) {
			continue
		}
		matched = append(matched, h)
	}

	// offset/limit 按契约约定 2 的语义：防止一次拉出过多行，不是给分页器
	// 用的总数感知分页——越界的 offset 直接得到空切片，不报错。
	if offset >= len(matched) {
		matched = matched[:0]
	} else {
		matched = matched[offset:]
	}
	if limit < len(matched) {
		matched = matched[:limit]
	}

	items := make([]hostListItem, 0, len(matched))
	for _, h := range matched {
		items = append(items, toHostListItem(h))
	}
	apierr.WriteOK(w, items)
}

// hostMatchesQuery 判定 h 是否匹配文本搜索 q（q 已经是小写、已 trim）。
// 匹配 SN/MAC/机架位三个字段的子串，大小写不敏感（q 已转小写，这里对
// 候选字段也转小写再比较）。
func hostMatchesQuery(h model.Host, q string) bool {
	return strings.Contains(strings.ToLower(h.SN), q) ||
		strings.Contains(strings.ToLower(h.MAC), q) ||
		strings.Contains(strings.ToLower(h.Rack), q)
}

// parseLimit 解析 limit 查询参数：空串或解析失败用 def；解析出的值
// 非正数同样用 def（0 或负数当作「未指定」处理，而不是「返回 0 条」，
// 因为契约里 limit 的语义是上限防护，不存在「故意要 0 条」的合理场景）；
// 解析出的正数值原样返回，即便超过 def（契约没有规定 limit 本身的上限，
// def 只是「未传参时」的默认值）。
func parseLimit(raw string, def int) int {
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

// parseNonNegativeInt 解析一个非负整数查询参数：空串、解析失败、或解析
// 出负数，都回退到 def。
func parseNonNegativeInt(raw string, def int) int {
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return def
	}
	return v
}

// createHostRequest 是 POST /hosts 的入参，字段名与契约逐字对应。
type createHostRequest struct {
	SN       string `json:"sn"`
	MAC      string `json:"mac"`
	UUID     string `json:"uuid"`
	Arch     string `json:"arch"`
	Firmware string `json:"firmware"`
	Rack     string `json:"rack"`
}

// validArchValues 与 validFirmwareValues 是 arch/firmware 字段的白名单
// ——取值必须是 M1 枚举内的字面量，不接受自由文本（计划任务 3 校验三）。
//
// firmware 的机器可上报子集不含 any（http-console.md §2 明确：any 只在
// ArtifactSet 上出现，Host 上报的固件模式永远是 bios 或 uefi 之一）。
var validArchValues = map[string]bool{
	string(model.ArchX86_64): true,
	string(model.ArchARM64):  true,
}

var validFirmwareValues = map[string]bool{
	string(model.FirmwareBIOS): true,
	string(model.FirmwareUEFI): true,
}

// handleCreateHost 处理 POST /hosts：手工录入兜底（§7.4）。
//
// 三条校验（计划任务 3）：
//  1. MAC 归一化后入库，非法值回校验错误
//  2. arch 与 firmware 必填
//  3. 取值必须是 M1 枚举内的字面量
func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	var req createHostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest, "请求体不是合法的 JSON"))
		return
	}

	if strings.TrimSpace(req.SN) == "" {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest, "sn 不能为空"))
		return
	}
	if strings.TrimSpace(req.MAC) == "" {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest, "mac 不能为空"))
		return
	}
	if req.Arch == "" {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest,
			"arch 不能为空（缺少它会让建任务时无法过滤可选 OS 列表）"))
		return
	}
	if !validArchValues[req.Arch] {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest,
			"arch 取值不合法，必须是 x86_64 或 arm64"))
		return
	}
	if req.Firmware == "" {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest,
			"firmware 不能为空（缺少它会让 §4.6 固件兼容校验形同虚设）"))
		return
	}
	if !validFirmwareValues[req.Firmware] {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest,
			"firmware 取值不合法，必须是 bios 或 uefi"))
		return
	}

	normalizedMAC := model.NormalizeMAC(req.MAC)
	if normalizedMAC == "" {
		// model.NormalizeMAC 空串即格式不合法（model 包作者说明：不能把
		// 非法值静默存成空值）。apierr 没有专门的 MAC 校验错误码（见
		// contractGaps），用 CodeBadRequest 承载，message 里写明具体
		// 原因。
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest,
			"mac 格式不合法，无法归一化为标准形式"))
		return
	}

	host := model.Host{
		SN:         strings.TrimSpace(req.SN),
		MAC:        normalizedMAC,
		UUID:       strings.TrimSpace(req.UUID),
		Arch:       model.Arch(req.Arch),
		Firmware:   model.FirmwareMode(req.Firmware),
		Rack:       req.Rack,
		LastSeenAt: s.clock.Now(),
	}

	created, err := s.deps.Hosts.UpsertHost(r.Context(), host)
	if err != nil {
		writeInternal(w, err)
		return
	}
	apierr.WriteOK(w, toHostListItem(created))
}
