// 本文件实现主机清单两个端点：查询（带文本搜索与分页）、录入。对应实现
// 计划任务 3，依据 docs/contracts/http-console.md §2、SPEC §4.3、§7.4。
//
// MVP 没有 discovery image，主机清单只能手工录入——§7.4 认可这条路径
// （Inventory provider 失败时允许手工兜底）。这个列表是建任务页 SN 字段
// 的唯一数据源（§4.3 硬要求）。
package console

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/model"
)

// routesHost 注册主机清单两个端点。
func (s *Server) routesHost() {
	// 查询只读，已登录（含 viewer）即可；录入需要 artifact:import——手工
	// 录入是「运维手工补数据」的兜底路径，与制品导入同一类操作，不为此
	// 单独发明一个权限码。
	s.handle("GET", "/hosts", "", "", s.handleListHosts)
	s.handle("POST", "/hosts", codeArtifactImport, "host.create", s.handleCreateHost)
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

	// Disks 回显磁盘清单。运维需要看到「这台机器录了哪些盘」——那是建任务时
	// 磁盘规划的唯一输入（除 by-hook 之外的四个策略都要它），录错或漏录的
	// 后果是建任务失败或选错盘，而不回显就只能靠翻数据库核对。
	Disks []hostDiskItem `json:"disks"`
}

// hostDiskItem 是回显用的一块盘。字段与 createHostDisk 对称——同一个概念在
// 请求与响应里用同样的键名，避免调用方两头各记一套。
type hostDiskItem struct {
	ByIDPath   string `json:"byIdPath"`
	SizeBytes  int64  `json:"sizeBytes"`
	Rotational bool   `json:"rotational"`
	Model      string `json:"model"`
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
// 是同一个字符串，postgres.Store 的主键形态（hosts 表的 mac 列裸值）与之
// 也不一致。本函数改用
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
		Disks:      toHostDiskItems(h.Disks),
	}
}

// toHostDiskItems 把磁盘清单转成回显形态。空清单返回 nil 而不是空切片：
// JSON 里就是 null，与「这台机器没录过盘」这个事实一致；空数组会让调用方
// 以为录过一个空清单。
func toHostDiskItems(disks []model.HostDisk) []hostDiskItem {
	if len(disks) == 0 {
		return nil
	}
	out := make([]hostDiskItem, 0, len(disks))
	for _, d := range disks {
		out = append(out, hostDiskItem{
			ByIDPath:   d.ByIDPath,
			SizeBytes:  d.SizeBytes,
			Rotational: d.Rotational,
			Model:      d.Model,
		})
	}
	return out
}

// defaultListLimit 是列表端点未传 limit 参数时的默认上限（契约约定 2：
// 防止一次拉出几万行，不是给分页器用）。
const defaultListLimit = 1000

// handleListHosts 处理 GET /hosts?q=&limit=&offset=。
//
// 文本搜索在本 handler 层做，不下推到 Store（M1 契约已说明）：取回后在
// Go 里对 SN/MAC/机架位做大小写不敏感的子串匹配，主机数是百级，这个代价
// 可以接受。
func (s *Server) handleListHosts(c *gin.Context) {
	hosts, err := s.deps.Hosts.ListHosts(c.Request.Context())
	if err != nil {
		apierr.Fail(c, err)
		return
	}

	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	limit := parseLimit(c.Query("limit"), defaultListLimit)
	offset := parseNonNegativeInt(c.Query("offset"), 0)

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
	apierr.OK(c, items)
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
//
// binding 标签对照迁移前的手写校验（迁移契约 §7、本任务 validationRules
// 字段有逐条对照）：
//   - sn/mac：required 承担原来的判空分支。mac 的格式校验（能否归一化为
//     合法值）标签表达不了——validator 内置的 mac 标签基于 net.ParseMAC，
//     不认识「无分隔的十二位十六进制」这种格式（TestHost_MACNormalization
//     明确要求这种格式必须被接受），因此 mac 格式校验留在 handler 里用
//     model.NormalizeMAC 做，不套 mac 标签。
//   - arch/firmware：required 加 oneof 一次表达「必填 + 取值必须在 M1
//     枚举内」两条规则——原代码是两个独立的 if，标签层没有必要拆成两个
//     标签，oneof 失败时 validator 报的就是同一个字段上的同一次绑定失败，
//     语义没有被合并弱化。
type createHostRequest struct {
	SN       string `json:"sn" binding:"required"`
	MAC      string `json:"mac" binding:"required"`
	UUID     string `json:"uuid"`
	Arch     string `json:"arch" binding:"required,oneof=x86_64 arm64"`
	Firmware string `json:"firmware" binding:"required,oneof=bios uefi"`
	Rack     string `json:"rack"`

	// Disks 可选。磁盘规划需要它——除 by-hook 之外的四个策略都要求服务端已知
	// 盘（见 model.Host.Disks 的文档）。不传时建出的主机磁盘清单为空，用
	// largest 等策略建任务会以「磁盘清单为空」失败。
	Disks []createHostDisk `json:"disks"`
}

// handleCreateHost 处理 POST /hosts：手工录入兜底（§7.4）。
//
// 校验分两层（迁移契约 §7）：
//  1. sn/mac 必填、arch/firmware 必填且取值在 M1 枚举内——交给
//     createHostRequest 的 binding 标签，ShouldBindJSON 失败时返回
//     validator.ValidationErrors，交给 apierr.Fail 翻译成中文 422 响应。
//  2. mac 归一化后是否合法——标签表达不了（见 createHostRequest 的字段
//     注释），留在这里用 Go 代码判断，命中时用 *apierr.Error 构造
//     一个独立的校验失败响应。
func (s *Server) handleCreateHost(c *gin.Context) {
	var req createHostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		var validationErrs validator.ValidationErrors
		if errors.As(err, &validationErrs) {
			apierr.Fail(c, err)
			return
		}
		// 不是字段级校验失败，是请求体本身不是合法 JSON（或类型不匹配到
		// 无法反序列化）——这两种错误 apierr.Fail 会当成未识别错误落进
		// CodeInternal/500（见 apierr/gin.go resolveFailure 的判定顺序），
		// 但请求体格式错是客户端的问题，不该记 500；这里用 CodeBadRequest
		// 显式区分。
		apierr.Fail(c, apierr.New(http.StatusBadRequest, apierr.CodeBadRequest, "请求体不是合法的 JSON"))
		return
	}

	normalizedMAC := model.NormalizeMAC(req.MAC)
	if normalizedMAC == "" {
		// model.NormalizeMAC 空串即格式不合法（model 包作者说明：不能把
		// 非法值静默存成空值）。apierr 没有专门的 MAC 校验错误码（见
		// contractGaps），用 CodeBadRequest 承载，message 里写明具体
		// 原因。
		apierr.Fail(c, apierr.New(http.StatusBadRequest, apierr.CodeBadRequest,
			"mac 格式不合法，无法归一化为标准形式"))
		return
	}

	disks, derr := toHostDisks(req.Disks)
	if derr != nil {
		apierr.Fail(c, derr)
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
		Disks:      disks,
	}

	created, err := s.deps.Hosts.UpsertHost(c.Request.Context(), host)
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	apierr.OK(c, toHostListItem(created))
}

// createHostDisk 是 POST /hosts 请求体里的一块盘。
type createHostDisk struct {
	ByIDPath   string `json:"byIdPath"`
	SizeBytes  int64  `json:"sizeBytes"`
	Rotational bool   `json:"rotational"`
	Model      string `json:"model"`
}

// toHostDisks 把请求体里的磁盘清单转成 model.HostDisk，并逐块校验 byIdPath。
//
// byIdPath 必填且不接受内核名：/dev/sda 这类名字在不同启动之间会漂移，写进
// 分区指令可能擦错盘（SPEC §10 失败模式 5）。空路径的盘进了清单，磁盘规划器
// 会选中它然后渲染出空的 --ondisk=——那个失败发生在目标机上，只能到 BMC
// 控制台看。
func toHostDisks(in []createHostDisk) ([]model.HostDisk, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]model.HostDisk, 0, len(in))
	for i, d := range in {
		path := strings.TrimSpace(d.ByIDPath)
		if path == "" {
			return nil, apierr.New(http.StatusBadRequest, apierr.CodeBadRequest,
				fmt.Sprintf("disks[%d] 缺 byIdPath——磁盘必须用 by-id/WWN 这类持久"+
					"标识，内核名（/dev/sda）在不同启动之间会漂移，写进分区指令"+
					"可能擦错盘", i))
		}
		out = append(out, model.HostDisk{
			ByIDPath:   path,
			SizeBytes:  d.SizeBytes,
			Rotational: d.Rotational,
			Model:      strings.TrimSpace(d.Model),
		})
	}
	return out, nil
}
