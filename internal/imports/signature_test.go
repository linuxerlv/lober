// 本文件测试镜像清单的签名校验（P3-03 任务 3），依据该计划「测试要锁住的
// 行为」五条与 SPEC §9.6。
package imports

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestSigner 生成一对 ECDSA P-256 密钥，返回 PEM 编码的公钥与一个签名函数。
//
// P-256 是 cosign key-based 模式的默认曲线，因此本测试用的密钥形态与真实
// pipeline/sign.sh 产出的一致——不是为测试方便另选一套算法。
func newTestSigner(t *testing.T) (pubPEM []byte, sign func([]byte) []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成测试密钥失败: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("编码公钥失败: %v", err)
	}
	pubPEM = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

	sign = func(payload []byte) []byte {
		digest := sha256.Sum256(payload)
		sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
		if err != nil {
			t.Fatalf("签名失败: %v", err)
		}
		// cosign sign-blob 输出 base64 编码的 ASN.1 DER 签名。
		out := make([]byte, base64.StdEncoding.EncodedLen(len(sig)))
		base64.StdEncoding.Encode(out, sig)
		return out
	}
	return pubPEM, sign
}

// TestSignatureVerifiesCorrectlySignedManifest 正确签名的清单校验通过。
func TestSignatureVerifiesCorrectlySignedManifest(t *testing.T) {
	pubPEM, sign := newTestSigner(t)
	manifest := []byte("schema_version: 1\nimage_sha256: abc\n")

	if err := VerifyManifestSignature(manifest, sign(manifest), pubPEM); err != nil {
		t.Errorf("正确签名的清单校验失败: %v —— 签名格式是 cosign key-based 模式的"+
			"「base64(ASN.1 DER ECDSA-P256)」，与 pipeline/sign.sh 的产出一致", err)
	}
}

// TestSignatureRejectsTamperedManifest 篡改清单后校验失败。
//
// 这是签名机制的核心用途：清单里有镜像的 sha256，改了清单就能让一个被替换的
// 镜像通过后续的 sha256 校验——签名是拦住这条路的唯一手段。
func TestSignatureRejectsTamperedManifest(t *testing.T) {
	pubPEM, sign := newTestSigner(t)
	manifest := []byte("schema_version: 1\nimage_sha256: abc\n")
	sig := sign(manifest)

	tampered := []byte("schema_version: 1\nimage_sha256: DEADBEEF\n")
	if err := VerifyManifestSignature(tampered, sig, pubPEM); err == nil {
		t.Error("篡改清单后校验仍然通过 —— 清单里有镜像的 sha256，" +
			"改清单就能让一个被替换的镜像通过后续校验，签名是拦住这条路的唯一手段")
	}
}

// TestSignatureRejectsMissingPublicKey 公钥缺失时**拒绝**，不是跳过校验。
//
// 这条是本组里最要紧的一条：「没有公钥就不校验」这种退化会让签名机制在部署
// 配置漏了一项的情况下静默失效，而运维完全看不出来——系统照常工作，只是不再
// 校验任何东西。§9.1 的默认拒绝在这里的具体形式就是「宁可导入失败」。
func TestSignatureRejectsMissingPublicKey(t *testing.T) {
	_, sign := newTestSigner(t)
	manifest := []byte("schema_version: 1\n")

	for name, pub := range map[string][]byte{
		"nil": nil,
		"空切片": {},
		"全空白": []byte("   \n\t "),
	} {
		t.Run(name, func(t *testing.T) {
			err := VerifyManifestSignature(manifest, sign(manifest), pub)
			if err == nil {
				t.Error("公钥缺失时校验通过 —— 必须拒绝而不是跳过校验。" +
					"「没有公钥就不校验」会让签名机制在配置漏项时静默失效，" +
					"系统照常工作但不再校验任何东西")
			}
			if !strings.Contains(err.Error(), "公钥") {
				t.Errorf("错误信息 = %q, want 提到「公钥」 —— 运维要能一眼看出是配置问题"+
					"而不是镜像本身有问题", err)
			}
		})
	}
}

// TestSignatureRejectsUnrecognizedFormat 签名或公钥格式不识别时报明确错误。
func TestSignatureRejectsUnrecognizedFormat(t *testing.T) {
	pubPEM, sign := newTestSigner(t)
	manifest := []byte("schema_version: 1\n")

	t.Run("签名不是base64", func(t *testing.T) {
		err := VerifyManifestSignature(manifest, []byte("这不是 base64!!!"), pubPEM)
		if err == nil {
			t.Fatal("非 base64 的签名被接受")
		}
		if !strings.Contains(err.Error(), "base64") {
			t.Errorf("错误信息 = %q, want 提到 base64 —— 格式错误与「签名不匹配」"+
				"是两类不同的问题，前者是交付物损坏或格式换了，后者是内容被改过；"+
				"报同一句话会让排查方向跑偏", err)
		}
	})

	t.Run("公钥不是PEM", func(t *testing.T) {
		err := VerifyManifestSignature(manifest, sign(manifest), []byte("not a pem block"))
		if err == nil {
			t.Fatal("非 PEM 的公钥被接受")
		}
		if !strings.Contains(err.Error(), "PEM") {
			t.Errorf("错误信息 = %q, want 提到 PEM", err)
		}
	})

	t.Run("公钥算法不是ECDSA", func(t *testing.T) {
		// ed25519 是合法的公钥类型但不是 cosign key-based 的默认算法，本实现
		// 明确只支持 ECDSA——遇到别的类型要报「不支持这个算法」，而不是
		// 「签名不匹配」那种会把人引向错误方向的说法。
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("生成 ed25519 密钥失败: %v", err)
		}
		der, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			t.Fatalf("编码 ed25519 公钥失败: %v", err)
		}
		edPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

		verr := VerifyManifestSignature(manifest, sign(manifest), edPEM)
		if verr == nil {
			t.Fatal("非 ECDSA 公钥被接受")
		}
		if !strings.Contains(verr.Error(), "ECDSA") {
			t.Errorf("错误信息 = %q, want 提到 ECDSA", verr)
		}
	})
}

// TestVerifyImageArtifact_RejectsTamperedImage 篡改镜像（使 sha256 不符）后
// 导入失败。
//
// 校验在**导入时**做而不是装机时（P3-03 任务 3 第 3 条）：装机时再校验意味着
// 每次装机都要完整读一遍几 GB 的镜像。这个取舍假设制品目录不会被恶意篡改，
// 而那个假设由文件权限保证——取舍写在 VerifyImageArtifact 的文档里。
func TestVerifyImageArtifact_RejectsTamperedImage(t *testing.T) {
	dir := t.TempDir()
	pubPEM, sign := newTestSigner(t)

	imageBytes := []byte("这是一份假的压缩镜像内容")
	imageSum := sha256.Sum256(imageBytes)
	imagePath := filepath.Join(dir, "image.raw.zst")
	if err := os.WriteFile(imagePath, imageBytes, 0o644); err != nil {
		t.Fatalf("写镜像文件失败: %v", err)
	}

	manifest := []byte("schema_version: 1\nimage_sha256: " + hexString(imageSum[:]) + "\n")
	manifestPath := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("写清单失败: %v", err)
	}
	sigPath := filepath.Join(dir, "manifest.yaml.sig")
	if err := os.WriteFile(sigPath, sign(manifest), 0o644); err != nil {
		t.Fatalf("写签名失败: %v", err)
	}

	// 未篡改：通过。
	if err := VerifyImageArtifact(dir, ImageVerifyRequest{
		ImageRelPath:    "image.raw.zst",
		ManifestRelPath: "manifest.yaml",
		SigRelPath:      "manifest.yaml.sig",
		ExpectedSHA256:  hexString(imageSum[:]),
		PublicKeyPEM:    pubPEM,
	}); err != nil {
		t.Fatalf("未篡改的交付物校验失败: %v", err)
	}

	// 篡改镜像内容：sha256 不再匹配清单里声明的值，必须失败。
	if err := os.WriteFile(imagePath, []byte("已被替换的镜像内容"), 0o644); err != nil {
		t.Fatalf("篡改镜像失败: %v", err)
	}
	err := VerifyImageArtifact(dir, ImageVerifyRequest{
		ImageRelPath:    "image.raw.zst",
		ManifestRelPath: "manifest.yaml",
		SigRelPath:      "manifest.yaml.sig",
		ExpectedSHA256:  hexString(imageSum[:]),
		PublicKeyPEM:    pubPEM,
	})
	if err == nil {
		t.Error("镜像被替换后校验仍然通过 —— 签名保证了清单没被改，" +
			"sha256 比对保证了镜像与清单声明的一致，两者缺一不可")
	}
}

// hexString 是测试内的小工具，避免在断言里重复写 encoding/hex 的调用。
func hexString(b []byte) string {
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, hexDigits[c>>4], hexDigits[c&0x0f])
	}
	return string(out)
}
