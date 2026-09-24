// 本文件实现镜像清单的签名校验（P3-03 任务 3），依据 SPEC §9.6。
//
// §9.6 的标题就是结论：**气隙下 sigstore 的无密钥模式走不通。** 无密钥模式
// 依赖两个外部服务——Fulcio（签发短期证书，需外网 OIDC）与 Rekor（透明日志，
// 签名时会把记录上传到公开日志）。气隙环境两者均不可达。
//
// 所以用 key-based 签名：私钥在外网构建机上对清单签名，公钥预置在网关配置里。
//
// # 签名格式
//
// 本实现按 cosign 的 key-based blob 签名格式校验，不引入 sigstore 的任何
// Go 依赖——那套库会连带拉进 OCI registry 客户端、Rekor 客户端等一大批气隙
// 环境用不到的代码。格式本身很简单，标准库就能验：
//
//	公钥：PEM 编码的 PKIX ECDSA 公钥（cosign.pub，P-256）
//	签名：base64(ASN.1 DER ECDSA 签名)，签的是清单内容的 sha256
//
// 因此 pipeline/sign.sh 用 `cosign sign-blob --key cosign.key` 产出的签名，
// 本函数可以直接校验；反过来 `cosign verify-blob --key cosign.pub
// --insecure-ignore-tlog` 也能校验本项目认可的签名。两边不是各自发明格式。
//
// **不要指望 cosign 的默认流程能跑**（§9.6 原话）：它当前版本的文档示例已经
// 全面转向 `--bundle`，`--key` 只出现在 flag 说明里。详见 pipeline/README.md
// 第 3.1 节的核实结论。
//
// # 三条设计要求（P3-03 任务 3）
//
//  1. **签的是清单而不是镜像本身。** 镜像有几 GB，签它要完整读一遍；清单里
//     已经有镜像的 sha256，签清单等于间接签了镜像，而清单只有几 KB。
//  2. **公钥来自配置，不从网络取。** 取公钥的通道若不可信，整个签名机制就
//     没有意义。
//  3. **校验在导入时做，不在装机时做。** 取舍见 VerifyImageArtifact 的文档。
package imports

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// VerifyManifestSignature 用公钥校验清单的签名。
//
// manifest 是清单文件的原始字节（不做任何规范化——签名是对字节签的，重新
// 序列化一遍 YAML 就会导致校验失败，即使语义相同）。signature 是签名文件的
// 原始内容（base64 文本，容忍首尾空白与换行）。publicKeyPEM 是配置里预置的
// 公钥。
//
// 公钥为空时**返回错误而不是跳过校验**：「没有公钥就不校验」这种退化会让签名
// 机制在部署配置漏了一项时静默失效——系统照常工作，只是不再校验任何东西，而
// 运维完全看不出来。§9.1 的默认拒绝在这里的具体形式就是宁可导入失败。
func VerifyManifestSignature(manifest, signature, publicKeyPEM []byte) error {
	if len(strings.TrimSpace(string(publicKeyPEM))) == 0 {
		return fmt.Errorf("imports: 未配置镜像签名公钥，拒绝导入镜像直写制品集" +
			"（不是跳过校验——没有公钥就不校验会让签名机制在配置漏项时静默失效，" +
			"系统照常工作但不再校验任何东西，见 SPEC §9.6）")
	}

	pub, err := parseECDSAPublicKeyPEM(publicKeyPEM)
	if err != nil {
		return err
	}

	sigDER, err := decodeBase64Signature(signature)
	if err != nil {
		return err
	}

	digest := sha256.Sum256(manifest)
	if !ecdsa.VerifyASN1(pub, digest[:], sigDER) {
		return fmt.Errorf("imports: 清单签名校验不通过 —— 清单内容与签名不匹配。" +
			"可能是清单被改过（含被改过 image_sha256 以放行一个替换过的镜像），" +
			"也可能是签名用的私钥与网关配置里的公钥不是一对")
	}
	return nil
}

// parseECDSAPublicKeyPEM 解析 PEM 编码的 PKIX ECDSA 公钥。
//
// 三类失败各自给不同的错误文案：不是 PEM、不是合法 PKIX、不是 ECDSA。**这三
// 类与「签名不匹配」是完全不同的问题**——前三类是配置或交付物格式错了，后者
// 是内容被改过；报同一句话会让排查方向跑偏（P3-03 任务 3 的测试要求「签名
// 格式不识别时报明确错误」）。
func parseECDSAPublicKeyPEM(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("imports: 镜像签名公钥不是合法的 PEM 块" +
			"（期望 cosign.pub 那种 -----BEGIN PUBLIC KEY----- 开头的文本）")
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("imports: 镜像签名公钥的 PEM 块不是合法的 PKIX 公钥: %w", err)
	}

	pub, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("imports: 镜像签名公钥的算法是 %T，本实现只支持 ECDSA"+
			"（cosign key-based 模式的默认算法是 ECDSA P-256）", parsed)
	}
	return pub, nil
}

// decodeBase64Signature 解码签名文件内容。
//
// 容忍首尾空白与换行：签名文件多半是 shell 重定向产出的，末尾带一个换行是
// 常态，不该因此判成格式错误。
func decodeBase64Signature(signature []byte) ([]byte, error) {
	trimmed := strings.TrimSpace(string(signature))
	if trimmed == "" {
		return nil, fmt.Errorf("imports: 签名文件为空 —— 交付物四件套里的 " +
			"manifest.yaml.sig 缺失或未写入内容")
	}
	der, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("imports: 签名文件不是合法的 base64: %w"+
			"（cosign sign-blob 输出的是 base64 编码的 ASN.1 DER 签名）", err)
	}
	return der, nil
}

// ImageVerifyRequest 是 VerifyImageArtifact 的入参：四件套交付物里各文件相对
// 制品集根目录的路径，加上期望的镜像校验值与公钥。
//
// 路径用相对路径加一个根目录参数，而不是让调用方拼好绝对路径：拼接逻辑放在
// 一处，顺带能拒绝掉指向目录外的路径（见 resolveUnderRoot）。
type ImageVerifyRequest struct {
	ImageRelPath    string
	ManifestRelPath string
	SigRelPath      string

	// ExpectedSHA256 是**压缩后**镜像文件的期望校验值，来自清单的
	// image_sha256 字段。
	//
	// 与解压后镜像的 sha256 是两个不同阶段、不同目的的校验，不能混用
	// （pipeline/README.md 第 2.2 节）：这一项校验「传输到内网的文件没有被
	// 篡改」，解压后那一项供写盘端回读校验「写到盘上的字节没有出错」。
	ExpectedSHA256 string

	// PublicKeyPEM 是网关配置里预置的公钥，空值导致拒绝（不是跳过）。
	PublicKeyPEM []byte
}

// VerifyImageArtifact 校验一份镜像直写交付物：先验清单签名，再比对镜像的
// sha256。
//
// 两步缺一不可，且顺序有意义：签名保证「清单没被改过」，sha256 比对保证
// 「镜像与清单声明的一致」。**只做 sha256 比对是没有意义的**——攻击者换掉
// 镜像的同时把清单里的 sha256 一起改掉即可通过；签名正是拦住这条路的手段。
//
// # 为什么在导入时校验而不是装机时（P3-03 任务 3 第 3 条）
//
// 装机时再校验意味着每次装机都要完整读一遍几 GB 的镜像，而导入时校验一次、
// 之后靠文件系统的完整性即可。
//
// **这个取舍有一个明确的假设：制品目录不会被恶意篡改。** 该假设由文件权限
// 保证（见 docs/contracts/filesystem.md 的权限一节）——制品目录对 nginx 只读、
// 只有导入进程可写。假设不成立时（比如制品目录被挂在一个可被其它服务写入的
// 共享存储上），本设计的安全性就退化到「导入那一刻是好的」。部署时要确认这
// 一条。
func VerifyImageArtifact(rootDir string, req ImageVerifyRequest) error {
	manifestPath, err := resolveUnderRoot(rootDir, req.ManifestRelPath)
	if err != nil {
		return err
	}
	sigPath, err := resolveUnderRoot(rootDir, req.SigRelPath)
	if err != nil {
		return err
	}
	imagePath, err := resolveUnderRoot(rootDir, req.ImageRelPath)
	if err != nil {
		return err
	}

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("imports: 读清单文件 %q 失败: %w", req.ManifestRelPath, err)
	}
	sigBytes, err := os.ReadFile(sigPath)
	if err != nil {
		return fmt.Errorf("imports: 读签名文件 %q 失败: %w", req.SigRelPath, err)
	}

	// 先验签名：清单不可信的话，它里面的 sha256 也不可信，比对它毫无意义。
	if err := VerifyManifestSignature(manifestBytes, sigBytes, req.PublicKeyPEM); err != nil {
		return err
	}

	actual, err := fileSHA256(imagePath)
	if err != nil {
		return fmt.Errorf("imports: 计算镜像文件 %q 的 sha256 失败: %w", req.ImageRelPath, err)
	}
	if !strings.EqualFold(actual, strings.TrimSpace(req.ExpectedSHA256)) {
		return fmt.Errorf("imports: 镜像文件 %q 的 sha256 与清单声明不符"+
			"（实际 %s，清单 %s）—— 传输损坏或文件被替换过",
			req.ImageRelPath, actual, req.ExpectedSHA256)
	}
	return nil
}

// resolveUnderRoot 把相对路径拼到 rootDir 下，并拒绝逃出 rootDir 的路径。
//
// 逃出检查不是多余的：这些相对路径来自清单与制品集记录，而清单是从外部带进来
// 的文件。一个写成 "../../etc/shadow" 的路径若被原样拼接，校验流程就变成了一个
// 任意文件读取原语。
func resolveUnderRoot(rootDir, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("imports: 镜像交付物的路径为空 —— 四件套（image.raw.zst、" +
			"manifest.yaml、manifest.yaml.sig、meta.yaml）缺了一项，制品集记录不完整")
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("imports: 解析制品根目录 %q 失败: %w", rootDir, err)
	}
	joined := filepath.Join(absRoot, filepath.FromSlash(rel))
	// filepath.Join 内部已经 Clean 过，因此这里比较前缀即可判定是否逃出。
	if joined != absRoot && !strings.HasPrefix(joined, absRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("imports: 镜像交付物路径 %q 指向制品目录之外，拒绝读取"+
			"（路径来自外部带进来的清单，原样拼接会让校验流程变成一个任意文件读取原语）", rel)
	}
	return joined, nil
}

// fileSHA256 流式计算文件的 sha256，返回小写十六进制。
//
// 流式而不是 os.ReadFile：镜像文件有几 GB，整个读进内存会让导入进程的内存
// 占用与镜像大小同量级——而导入与装机跑在同一个进程里（§1.1 单二进制）。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
