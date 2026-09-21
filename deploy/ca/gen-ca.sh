#!/usr/bin/env bash
# 部署前必跑的校验命令（本机若有 openssl）：
#   bash deploy/ca/gen-ca.sh --dry-run
# --dry-run 只打印将要执行的 openssl 命令与目标路径，不实际生成任何文件、
# 不接触私钥。正式生成前先跑一遍这个，核对路径与主题字段是否符合预期。
#
# 建立 Lober 内网根 CA（离线保管私钥）。
#
# 依据 docs/superpowers/plans/2026-09-20-lober-phase4-02-tls.md 任务 2：
# 气隙环境没有公共 CA 可用，必须自建；CA 私钥不能生成在或留在装机服务器上。
#
# 用法：
#   bash gen-ca.sh [--out-dir <目录>] [--days <天数>] [--dry-run]
#
# 默认输出目录：./out（相对当前工作目录，不写死绝对路径）。
# 默认有效期：3650 天（10 年）——见 README.md「有效期与轮换节奏」一节的理由：
# 嵌入 iPXE 二进制的是这份 CA 证书，CA 若频繁轮换会连带触发 iPXE 重编译。
#
# 本脚本只依赖 openssl，不依赖任何 Go 工具链或本仓库的其它脚本，
# 因为它必须能在一台离线、专用于保管 CA 私钥的机器上独立运行
# （README.md「CA 私钥怎么产生与保管」一节要求这台机器不是装机服务器）。
set -euo pipefail

# ---------------------------------------------------------------------------
# 占位值说明（务必修改）：
#   下面 CA_SUBJECT 中的 O= 与 OU= 字段是示例占位符，不是可直接使用的值。
#   部署前必须改成你所在组织的真实名称——这个值会出现在每一张由此 CA
#   签发的证书的签发者字段里，运维与审计都会看到它。
# ---------------------------------------------------------------------------
CA_SUBJECT="${LOBER_CA_SUBJECT:-/C=ZZ/O=CHANGE-ME-ORG-NAME/OU=CHANGE-ME-UNIT/CN=Lober Internal Root CA}"

OUT_DIR="./out"
DAYS=3650
DRY_RUN=0

while [ $# -gt 0 ]; do
  case "$1" in
    --out-dir)
      OUT_DIR="$2"
      shift 2
      ;;
    --days)
      DAYS="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      grep -E '^# ' "$0" | sed 's/^# //'
      exit 0
      ;;
    *)
      echo "未知参数: $1" >&2
      exit 1
      ;;
  esac
done

CA_KEY="$OUT_DIR/ca.key.pem"
CA_CERT="$OUT_DIR/ca.cert.pem"

echo "== Lober 内网根 CA 生成 =="
echo "输出目录:   $OUT_DIR"
echo "有效期:     $DAYS 天"
echo "主题:       $CA_SUBJECT"
echo

if [ "$DRY_RUN" -eq 1 ]; then
  echo "[--dry-run] 不会实际执行，以下是将要运行的命令："
  echo
  echo "mkdir -p '$OUT_DIR'"
  echo "openssl genrsa -aes256 -out '$CA_KEY' 4096"
  echo "openssl req -x509 -new -nodes -key '$CA_KEY' -sha256 -days $DAYS \\"
  echo "  -subj '$CA_SUBJECT' -out '$CA_CERT' \\"
  echo "  -addext 'basicConstraints=critical,CA:true' \\"
  echo "  -addext 'keyUsage=critical,keyCertSign,cRLSign'"
  echo
  echo "[--dry-run] 未生成任何文件。"
  exit 0
fi

mkdir -p "$OUT_DIR"

if [ -e "$CA_KEY" ]; then
  echo "错误: $CA_KEY 已存在，本脚本不会覆盖既有 CA 私钥。" >&2
  echo "如需轮换 CA，另建新目录，不要覆盖旧的——README.md 有轮换步骤。" >&2
  exit 1
fi

echo "生成 CA 私钥（4096 位 RSA，AES-256 加密保护，会提示输入密码短语）..."
# -aes256：私钥文件本身用密码短语加密。这道密码短语与 README 中「离线保管」
# 是两件叠加的防护——即便磁盘或备份介质泄露，没有密码短语也无法用这把私钥
# 签发证书。密码短语本身不由本脚本生成或存储，运维手工输入并按组织的密钥
# 管理规范保管（例如拆分交给两名人员各记一半）。
openssl genrsa -aes256 -out "$CA_KEY" 4096
chmod 400 "$CA_KEY"

echo "生成 CA 自签名根证书..."
openssl req -x509 -new -nodes -key "$CA_KEY" -sha256 -days "$DAYS" \
  -subj "$CA_SUBJECT" -out "$CA_CERT" \
  -addext "basicConstraints=critical,CA:true" \
  -addext "keyUsage=critical,keyCertSign,cRLSign"
chmod 444 "$CA_CERT"

echo
echo "完成。"
echo "  CA 私钥:  $CA_KEY   （权限 400，必须离线保管，绝不拷贝到装机服务器）"
echo "  CA 证书:  $CA_CERT  （权限 444，这是要嵌入 iPXE 二进制、导入浏览器的文件）"
echo
echo "下一步："
echo "  1. 把 $CA_KEY 移到离线介质（加密 U 盘 / 硬件安全模块），从任何联网机器删除本地副本。"
echo "  2. 用 issue-cert.sh 签发服务端证书时，把这个目录作为 --ca-dir 传入。"
echo "  3. 把 $CA_CERT 交给 deploy/ipxe 的构建脚本做 TRUST= 嵌入，以及分发给运维浏览器。"
