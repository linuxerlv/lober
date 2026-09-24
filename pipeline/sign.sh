#!/usr/bin/env bash
# sign.sh 对镜像清单签名，产出交付物四件套的第三项 manifest.yaml.sig。
# 对应 phase3-03 计划任务 3，依据 SPEC §9.6。
#
# 这是封装流水线的**最后一步**，跑在外网构建机上（pipeline/README.md 第 3.3 节
# 的第 3 步）。签完之后整个目录才可以拷进内网。
#
# ---------------------------------------------------------------------------
# 为什么是 key-based 而不是 cosign 默认的 keyless
# ---------------------------------------------------------------------------
#
# SPEC §9.6 的标题就是结论：气隙下 sigstore 的无密钥模式走不通。keyless 依赖
# 两个外部服务——Fulcio（签发短期证书，需外网 OIDC 登录）与 Rekor（透明日志，
# 签名时默认会把记录上传到公开日志）。气隙内网两者均不可达。
#
# **不要指望 cosign 的默认流程能跑**（§9.6 原话）。本轮核实确认这句话现在比
# 写 SPEC 时更真：cosign v3.0.x 的 `sign-blob` / `verify-blob` 官方示例已经
# 全面转向 `--bundle` 这个单一 JSON 文件格式，`--key` 只出现在 flag 说明里、
# 不出现在任何一条示例命令中。照抄文档最上面的示例会先卡在「以为必须先跑
# keyless 流程生成 bundle」这一步。`--key` 与 `--insecure-ignore-tlog` 两个
# flag 仍然存在且受支持（cosign CHANGELOG v3.0.4："Don't require a trusted
# root to verify offline with a key"）。详见 pipeline/README.md 第 3.1 节。
#
# ---------------------------------------------------------------------------
# 签名格式（与 internal/imports/signature.go 的校验侧是一对）
# ---------------------------------------------------------------------------
#
#   公钥：PEM 编码的 PKIX ECDSA 公钥（cosign.pub，P-256）
#   签名：base64(ASN.1 DER ECDSA 签名)，签的是 manifest.yaml 内容的 sha256
#
# 校验侧只用 Go 标准库实现（不引入 sigstore 的任何 Go 依赖——那套库会连带拉进
# OCI registry 客户端、Rekor 客户端等一大批气隙环境用不到的代码）。因此两侧
# 不是各自发明格式：本脚本产出的签名 `cosign verify-blob --key` 能验，
# internal/imports/signature.go 也能验。
#
# **签的是清单不是镜像**（计划任务 3 第 1 条）：镜像有几 GB，签它要完整读一遍；
# 清单里已经有镜像的 sha256，签清单等于间接签了镜像，而清单只有几 KB。
#
# ---------------------------------------------------------------------------
# 私钥放哪（计划任务 3 的操作性问题）
# ---------------------------------------------------------------------------
#
# **放构建机并限制访问。** 构建机本身就已经承担「产出被系统信任的镜像」这个
# 信任根的角色，私钥放在这里不扩大攻击面。
#
#   不放开发者机器：分散的私钥副本无法审计谁签过什么。
#   不放内网：内网只做校验，不需要签名能力。
#
# 这个决定是「谁能封装出被系统信任的镜像」这个权限问题的答案——能访问构建机上
# 这个私钥的人，就能让任意镜像被内网接受。据此设置构建机的访问控制。
#
# 用法：
#   sign.sh <交付物目录> <私钥路径>
#
# 交付物目录里应已有 manifest.yaml（由 scripts/package.sh 产出）。
# 产出：<交付物目录>/manifest.yaml.sig
set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "用法: $0 <交付物目录> <私钥路径>" >&2
	echo "" >&2
	echo "交付物目录里应已有 manifest.yaml（scripts/package.sh 的产出）。" >&2
	exit 1
fi

delivery_dir="$1"
key_path="$2"

manifest="${delivery_dir}/manifest.yaml"
sig_out="${delivery_dir}/manifest.yaml.sig"

if [ ! -f "${manifest}" ]; then
	echo "错误: 找不到 ${manifest}" >&2
	echo "签名对象是清单文件，先跑 scripts/package.sh 产出它。" >&2
	exit 1
fi

if [ ! -f "${key_path}" ]; then
	echo "错误: 找不到私钥 ${key_path}" >&2
	exit 1
fi

# ---------------------------------------------------------------------------
# 两条路径：有 cosign 用 cosign，没有就退回 openssl
# ---------------------------------------------------------------------------
#
# 两者产出的签名字节格式相同（base64 的 ASN.1 DER ECDSA 签名），校验侧不区分
# 是哪个工具签的。留 openssl 这条退路是因为构建机上不一定装了 cosign，而
# openssl 几乎总是有——而「因为缺一个工具就没法签名」会让整条流水线停摆。
if command -v cosign >/dev/null 2>&1; then
	echo "用 cosign 签名（key-based 模式）..." >&2
	# COSIGN_PASSWORD 由调用方通过环境变量提供；留空表示私钥无口令保护。
	# --tlog-upload=false 是关键：不加它 cosign 会尝试往 Rekor 公开日志上传
	# 记录，而气隙环境连不上，命令会卡住或超时失败。
	COSIGN_PASSWORD="${COSIGN_PASSWORD:-}" cosign sign-blob \
		--key "${key_path}" \
		--tlog-upload=false \
		--yes \
		--output-signature "${sig_out}" \
		"${manifest}"
else
	echo "未找到 cosign，退回 openssl 签名（产出格式相同）..." >&2
	# openssl dgst -sha256 -sign 产出的就是「对内容的 sha256 做 ECDSA 签名」
	# 的 ASN.1 DER 字节，再 base64 一层即与 cosign 的输出一致。
	openssl dgst -sha256 -sign "${key_path}" "${manifest}" |
		openssl base64 -A >"${sig_out}"
fi

echo "已签名: ${sig_out}" >&2

# ---------------------------------------------------------------------------
# 自检：立刻验一遍自己刚签的东西
# ---------------------------------------------------------------------------
#
# 不自检的后果：一个签坏的交付物要等到拷进内网、导入失败时才发现，而那时候
# 构建机可能已经不在手边了（气隙环境下两边往返一次的成本很高）。
if command -v openssl >/dev/null 2>&1; then
	pub_tmp="$(mktemp)"
	trap 'rm -f "${pub_tmp}"' EXIT
	if openssl pkey -in "${key_path}" -pubout -out "${pub_tmp}" 2>/dev/null; then
		if openssl base64 -d -A -in "${sig_out}" |
			openssl dgst -sha256 -verify "${pub_tmp}" -signature /dev/stdin "${manifest}" >/dev/null 2>&1; then
			echo "自检通过: 签名可被对应公钥验证" >&2
		else
			echo "自检失败: 刚产出的签名无法被对应公钥验证，不要交付这份产物" >&2
			exit 1
		fi
	else
		echo "自检跳过: 无法从私钥导出公钥（私钥可能有口令保护）" >&2
	fi
fi

echo "" >&2
echo "下一步：把公钥交给内网，配进网关的 store.image_signature_public_key_path。" >&2
echo "  openssl pkey -in ${key_path} -pubout -out cosign.pub" >&2
