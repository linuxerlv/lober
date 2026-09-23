// 本文件实现制品授权的签名 URL 签发：SPEC §9.3「大制品走 nginx
// secure_link（HMAC + 过期），Go 只签发 URL，完全不在数据路径上」。
//
// 架构约束（本文件存在的理由）：internal/render 是纯函数，不持有任何密钥
// （渲染上下文 model.RenderContext 没有 secret 字段，也不该有——见
// internal/render 包文档「渲染器不查库」的同一条设计原则：不该持有的东西
// 就不该出现在它的输入里）。但 SPEC §9.3 要求的签名必须知道
// secure_link 的共享密钥。折中方式：渲染器继续用
// model.BaseURLs.ArtifactURL 产出未签名 URL（不变），网关在拿到
// RenderMenu 的产出文本之后，对文本中出现的「大制品」URL 子串做一次
// 定位替换，把 `?md5=...&expires=...` 查询参数拼接上去——网关是唯一持有
// secure_link 密钥的模块，这个后处理步骤发生在网关自己的 handleBootGrant
// 里（boot.go），不需要改 internal/render 的任何文件。
//
// 第二个真实的架构约束（记入 contractGaps，此处只记落地方式）：
// SPEC §9.3 的表格把制品分三类——离线仓库（必须开放）、大制品（
// secure_link）、少量敏感大文件（X-Accel-Redirect）。但
// docs/contracts/filesystem.md 的磁盘布局显示 kernel/initrd/repo/hooks
// 四类文件实际共享同一个 nginx location（/artifacts/，同一个
// alias），且 model.BaseURLs.ArtifactURL 与 RepoURL 当前实现完全相同
// （都拼同一个 /artifacts/ 前缀，见 render.go 文档）。这意味着不能直接在
// 现有 /artifacts/ location 上加 secure_link 指令——那样 repo 与 hook
// 脚本的请求（同样落在这个 location 下）会因为不带签名而被一起 403，
// 违反 SPEC §9.3「离线仓库必须保持开放，签了会让装机在装包阶段整体失败」
// 这条硬性要求。
//
// 本次实现的做法：deploy/nginx/lober.conf 新增一个独立的
// /artifacts/secure/ location，用第二个 alias 指向与 /artifacts/
// 完全相同的物理目录（/srv/lober/artifacts/），只在这一个新 location
// 上启用 secure_link；仅这个前缀要求签名，原有 /artifacts/ 与 /repo/
// 两个 location 的行为不变（未签名请求仍然可达，向后兼容且不破坏离线
// 仓库与 hook 脚本的开放访问）。SignedArtifactURL 把未签名 URL 的路径
// 前缀从 "/artifacts/" 重写成 "/artifacts/secure/" 后再签名——重写后的
// 路径就是 nginx 侧 secure_link_md5 公式里 $uri 的值，两侧必须逐字节
// 一致（该公式的陷阱见 §1.6，本文件 clientIP 复用 boot.go 已有的实现）。
package gateway

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// securedArtifactPrefix 是签名制品 location 的 URL 路径前缀，对应
// deploy/nginx/lober.conf 新增的 location /artifacts/secure/。
//
// unsecuredArtifactPrefix 是 model.BaseURLs.ArtifactURL 产出的原始前缀
// （render.go: joinURL(base, "artifacts", segments...)）——本文件用字符串
// 替换把前者换成后者，是唯一需要与渲染器产出格式保持一致的耦合点，若
// 将来 ArtifactURL 的前缀拼接方式变化，这两个常量与
// TestSignedArtifactURL_RewritesToSecurePrefix 会一起需要更新。
const (
	unsecuredArtifactPrefix = "/artifacts/"
	securedArtifactPrefix   = "/artifacts/secure/"
)

// SecureLinkSecret 是 secure_link 签名的共享密钥，必须与
// deploy/nginx/lober.conf 里 secure_link_md5 指令引用的密钥字面值逐字节
// 相同（部署期核对项，见 docs/real-machine/06-securelink.md）。
//
// 类型定义成 string 的具名类型而不是裸 string——防止调用方不小心把普通
// 字符串（如 clientIP 的返回值）传进需要密钥的参数位，编译期就能发现
// 顺序颠倒的调用错误（SignedArtifactURL 与 signLargeArtifactURLs 都要求
// 这个类型）。导出（大写开头）是因为装配点 cmd/lober/wire.go 需要从
// internal/config.Gateway.SecureLinkSecret（裸 string）构造出这个类型的
// 值，赋给 gateway.Deps.SecureLinkSecret 字段。
type SecureLinkSecret string

// SignedArtifactURL 把渲染器产出的一个未签名制品 URL 改写成带
// secure_link 签名查询参数的形式：路径前缀从 /artifacts/ 换成
// /artifacts/secure/，并追加 ?md5=<签名>&expires=<过期时间戳>。
//
// unsignedURL 必须是 model.BaseURLs.ArtifactURL(...) 的产出（形如
// "http://host/artifacts/<相对路径>"，不含 query）——这是本函数与调用方
// （boot.go 的 handleBootGrant）之间的隐式契约，由
// TestSignedArtifactURL_RewritesToSecurePrefix 锁住具体前缀形式。
// unsignedURL 若不含 unsecuredArtifactPrefix 子串（不符合预期的输入），
// 返回原始 URL 与 false，调用方应据此跳过替换而不是签发一个错误的 URL——
// 宁可退化为「这条 URL 没被签名」（功能上等价于本次实现之前的开放状态），
// 也不要签发一个路径重写失败但仍然附带 md5/expires 参数的畸形 URL。
//
// remoteAddr 必须是 nginx 校验时会看到的客户端 IP（clientIP 函数的
// 返回值，即 X-Real-IP 或直连 peer），不能是 Go 进程自己看到的 peer
// （SPEC §1.6/§9.3 的陷阱：Go 在 nginx 之后看到的是 loopback，必须用
// X-Real-IP 才能与 nginx 校验时看到的真实客户端 IP 一致）。
//
// expiresAt 是 Unix 时间戳（秒），调用方按部署配置的 TTL 加当前时间
// 算出（不在本函数内部调用时钟——保持本函数纯函数、可测试）。
func SignedArtifactURL(unsignedURL string, secret SecureLinkSecret, remoteAddr string, expiresAt int64) (string, bool) {
	parsed, err := url.Parse(unsignedURL)
	if err != nil {
		return unsignedURL, false
	}

	if !strings.HasPrefix(parsed.Path, unsecuredArtifactPrefix) {
		return unsignedURL, false
	}
	securedPath := securedArtifactPrefix + strings.TrimPrefix(parsed.Path, unsecuredArtifactPrefix)
	parsed.Path = securedPath

	md5sum := secureLinkMD5(securedPath, remoteAddr, expiresAt, secret)

	q := parsed.Query()
	q.Set("md5", md5sum)
	q.Set("expires", strconv.FormatInt(expiresAt, 10))
	parsed.RawQuery = q.Encode()

	return parsed.String(), true
}

// secureLinkMD5 计算 nginx secure_link_md5 公式的签名值：对
// "<expires><uri><remote_addr> <secret>" 取 MD5，再按 nginx 自己的
// base64 变体编码（这不是标准 base64url，是 nginx 文档记录的特定变体，
// 见 nginxSecureLinkBase64 的函数文档）。
//
// uri 参数必须是签名后的最终路径（即已经重写成
// /artifacts/secure/... 前缀之后的值，不含 query、不含 scheme/host）
// ——SPEC §9.3 nginx 示例里的 secure_link_md5 引用的是 $uri 变量，nginx
// 的 $uri 语义上不含 scheme/host（deploy/nginx/lober.conf TLS 增量段落
// 「签名链接与 scheme 无关」一节已经记录了这条结论，本函数是那条结论
// 依赖的实现）。
func secureLinkMD5(uri, remoteAddr string, expiresAt int64, secret SecureLinkSecret) string {
	input := fmt.Sprintf("%d%s%s %s", expiresAt, uri, remoteAddr, secret)
	sum := md5.Sum([]byte(input))
	return nginxSecureLinkBase64(sum[:])
}

// nginxSecureLinkBase64 按 nginx ngx_http_secure_link_module 文档记录的
// 编码方式对 MD5 摘要做 base64 编码：标准 base64 编码后，把 '+' 换成
// '-'、'/' 换成 '_'，去掉末尾的 '=' 填充。
//
// 这不是 Go 标准库里现成对应的任何一种 base64 变体——base64.URLEncoding
// 只处理了 +/ 到 -_ 的替换，仍会填充 '='；本函数在其基础上再去掉填充，
// 等价于 base64.RawURLEncoding。选择显式写出转换步骤而不是直接用
// base64.RawURLEncoding.EncodeToString，是为了让这段代码的正确性可以
// 直接对照 nginx 官方文档的措辞逐句核对（"replaces '+' with '-', '/'
// with '_', and drops the trailing '='"），不依赖读者已经知道
// RawURLEncoding 恰好等价于这个描述。
func nginxSecureLinkBase64(sum []byte) string {
	encoded := base64.StdEncoding.EncodeToString(sum)
	encoded = strings.ReplaceAll(encoded, "+", "-")
	encoded = strings.ReplaceAll(encoded, "/", "_")
	encoded = strings.TrimRight(encoded, "=")
	return encoded
}

// largeArtifactRelPaths 枚举一份 model.ArtifactSet 里「大制品」类别
// （SPEC §9.3 表格第二行：kernel/initrd/镜像/WIM）对应的相对路径——
// 仓库离线仓库（Paths.Repo）与 hook 脚本（不在 ArtifactPaths 里，由
// clientlib.go/firstboot.go 等直接拼 "hooks/<digest>/script.sh"）都不在
// 这份列表里，必须保持开放（SPEC §9.3：「离线仓库必须保持开放……只对大
// 制品签名，不要把仓库也签了——签了会让装机在装包阶段整体失败」）。
//
// 返回值不含空字符串（Paths.Kernel 为空、Initrd 为空切片、或某个
// Extra 键缺失时，对应条目直接跳过而不是塞进一个空路径——空路径经过
// ArtifactURL 拼接后会指向制品集根目录本身，签一个不存在的资源没有
// 意义，也会在后续字符串替换阶段找不到匹配子串而静默什么都不做，
// 效果上是安全的，但显式跳过更清楚地表达意图）。
//
// Windows 的四个 Extra 键（wimboot/BCD/boot.sdi/boot.wim）与 ESXi 的
// mboot.efi 目前在 internal/render/menu.go 的分派 switch 里仍走「未
// 实现」分支（归 t08-dispatch 一轝接线，见本文件顶部 contractGaps
// 记录），本函数仍然把它们纳入枚举——一旦 t08 接线完成，这两个家族的
// 菜单会自动获得签名保护，不需要再回来改本文件。
func largeArtifactRelPaths(artifact model.ArtifactSet) []string {
	var paths []string
	if artifact.Paths.Kernel != "" {
		paths = append(paths, artifact.Paths.Kernel)
	}
	if len(artifact.Paths.Initrd) > 0 && artifact.Paths.Initrd[0] != "" {
		paths = append(paths, artifact.Paths.Initrd[0])
	}
	for _, key := range windowsAndESXiExtraKeys {
		if rel, ok := artifact.Paths.Extra[key]; ok && rel != "" {
			paths = append(paths, rel)
		}
	}
	return paths
}

// windowsAndESXiExtraKeys 是 ArtifactPaths.Extra 里可能出现的大制品键名
// （对照 internal/render/winpe.go 的 windowsExtraKeys 与
// internal/render/esxi.go 的 mboot.efi 用法，见 docs/contracts/
// filesystem.md「windows: wimboot、BCD、boot.sdi、boot.wim；esxi:
// mboot.efi、boot.cfg 模板」一行）。本包不 import internal/render
// （archtest 的跨模块 import 守卫禁止），因此在这里独立复制一份键名
// 列表，而不是引用 render 包的 windowsExtraKeys——两份列表若未来出现
// 分歧，表现是「新增的 Extra 键没有被签名」（功能退化到未签名，不是
// 签名错误），不是安全问题，但仍记入 contractGaps 供关注。
//
// 不包含 "boot.cfg 模板"——那是 ESXi 应答文件模板本身（渲染 boot.cfg
// 用的原始素材），不是机器引导时直接下载执行的大制品，其获取路径与
// 大小都不属于 SPEC §9.3 表格第二行描述的对象。
var windowsAndESXiExtraKeys = []string{"wimboot", "BCD", "boot.sdi", "boot.wim", "mboot.efi"}

// signLargeArtifactURLs 在渲染出的菜单文本 menu 中，把每一个「大制品」
// URL 子串替换成对应的签名版本（SignedArtifactURL 的产出）。
//
// 实现方式是字符串定位替换，不是解析 iPXE 语法重新生成文本——原因是
// menu 的具体格式因 OS 家族而异（六个渲染函数各自的 fmt.Sprintf 模板，
// 见 internal/render/menu.go、winpe.go、esxi.go），网关不应该，也不能
// （不 import render 包）重新实现一份 iPXE 语法解析器。但每个大制品的
// 未签名 URL 都是 ctx.Bases.ArtifactURL(相对路径) 的确定性产出
// （render.go 的 joinURL 是纯函数），网关可以独立算出与渲染器算出的
// 完全相同的未签名 URL 字符串，再用它作为替换的定位锚点——这不依赖
// 「读懂」菜单文本的语法，只依赖「渲染器与网关对同一个相对路径调用同一个
// 纯函数会产出同一个字符串」这条更弱、也更容易保证成立的性质。
//
// 若某个未签名 URL 子串在 menu 里出现零次（例如该 OS 家族当前分派到
// 「未实现」错误分支、根本不会走到这里；或者渲染器对该家族的模板格式
// 恰好不含某个大制品，如 Windows 只有 initrd 行没有独立的 kernel 行
// 引用 Paths.Kernel），strings.Replace 对「找不到子串」的行为是原样
// 返回输入、替换次数 0——不是错误，函数继续处理下一个候选路径。
//
// clientRemoteAddr 必须是 clientIP(r) 的返回值（§1.6/§9.3 的陷阱：
// 必须是 nginx 校验时会看到的客户端 IP，不是 Go 进程自己看到的
// loopback peer），expiresAt 是调用方按配置的 TTL 加当前时间算出的
// Unix 时间戳。
func signLargeArtifactURLs(menu string, ctx model.RenderContext, secret SecureLinkSecret, clientRemoteAddr string, expiresAt int64) string {
	for _, relPath := range largeArtifactRelPaths(ctx.Artifact) {
		unsignedURL := ctx.Bases.ArtifactURL(relPath)
		signedURL, ok := SignedArtifactURL(unsignedURL, secret, clientRemoteAddr, expiresAt)
		if !ok {
			continue
		}
		menu = strings.ReplaceAll(menu, unsignedURL, signedURL)
	}
	return menu
}
