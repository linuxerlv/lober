#!/usr/bin/env bash
# 部署前必跑的校验命令（本机若有 openssl）：
#   bash deploy/ca/issue-cert.sh --dry-run --ca-dir ./out --domain boot.lober.internal --domain console.lober.internal
# --dry-run 只打印将要执行的命令，不接触 CA 私钥、不写任何文件。
#
# 用内网根 CA 签发服务端证书（机器面与人面）。
#
# 依据 docs/superpowers/plans/2026-09-20-lober-phase4-02-tls.md 任务 2 与
# SPEC.md §1.3/§1.5：证书的 CN/SAN 必须是域名，不能是 IP。理由见 SPEC §1.3
# 「使用域名而非 IP」一节的原话——将来签发证书时 CN 能对上域名，不必回头
# 修改所有 URL 与模板。本脚本用一条硬校验强制这一点：--domain 参数若看起来
# 像 IPv4/IPv6 字面量，直接拒绝签发。
#
# 用法：
#   bash issue-cert.sh --ca-dir <CA目录> --domain <域名> [--domain <域名> ...] \
#        [--out-dir <输出目录>] [--days <天数>] [--dry-run]
#
# 示例（机器面与人面各签一张，或按下面推荐用一张双域名证书）：
#   bash issue-cert.sh --ca-dir ./out \
#     --domain boot.lober.internal --domain console.lober.internal \
#     --out-dir ./out/server
#
# 是否签一张双域名证书还是两张单域名证书，取决于运维习惯，本脚本两种都支持
# （多个 --domain 即为多域名 SAN）。计划任务 2 原文允许两种做法。
set -euo pipefail

CA_DIR=""
OUT_DIR="./out/server"
DAYS=825   # 主流浏览器与 iPXE 的 TLS 库均未对服务端证书强制 <=398 天的
           # CA/Browser Forum 规则（那条规则只约束公网可信 CA），但 825 天
           # 是一个成熟、有先例的折中值：比公网惯例宽松（内网 CA 自己签发，
           # 不受该论坛约束），又不会长到「同一张证书用十年」这种一旦私钥
           # 泄露就补救无门的程度。README.md「有效期与轮换节奏」一节详述。
DOMAINS=()
DRY_RUN=0

while [ $# -gt 0 ]; do
  case "$1" in
    --ca-dir)
      CA_DIR="$2"
      shift 2
      ;;
    --domain)
      DOMAINS+=("$2")
      shift 2
      ;;
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

if [ ${#DOMAINS[@]} -eq 0 ]; then
  echo "错误: 至少需要一个 --domain 参数。" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# 硬校验：拒绝 IP 字面量。SPEC §1.3 与 §1.5 都强调证书必须用域名——
# 这不是风格建议，是「将来 CN 要能对上配置里的 gateway_base/artifact_base」
# 这条约束的直接体现（那两个配置项本身也要求填域名，见 internal/config）。
# ---------------------------------------------------------------------------
is_ip_literal() {
  local candidate="$1"
  # IPv4：四段数字
  if [[ "$candidate" =~ ^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$ ]]; then
    return 0
  fi
  # IPv6：含冒号即视为 IP 字面量（域名不含冒号）
  if [[ "$candidate" == *:* ]]; then
    return 0
  fi
  return 1
}

for d in "${DOMAINS[@]}"; do
  if is_ip_literal "$d"; then
    echo "错误: '$d' 看起来是 IP 字面量，不是域名。" >&2
    echo "SPEC §1.3/§1.5 要求证书 CN/SAN 必须是域名——将来 CN 才能对上" >&2
    echo "gateway_base/artifact_base 里配置的域名，不必回头修改所有 URL 与模板。" >&2
    exit 1
  fi
done

if [ -z "$CA_DIR" ]; then
  echo "错误: 必须指定 --ca-dir（CA 私钥与证书所在目录，来自 gen-ca.sh 的 --out-dir）。" >&2
  exit 1
fi

CA_KEY="$CA_DIR/ca.key.pem"
CA_CERT="$CA_DIR/ca.cert.pem"

# 主域名（CN）取第一个 --domain；其余进 SAN。CN 字段在现代 TLS 校验中已被
# SAN 取代（浏览器与 Go 的 crypto/tls 只认 SAN），但仍然填上以兼容可能存在的
# 老旧客户端校验逻辑——这是通行做法，不是本脚本特有的选择。
PRIMARY_DOMAIN="${DOMAINS[0]}"

SAN_LIST=""
for d in "${DOMAINS[@]}"; do
  if [ -n "$SAN_LIST" ]; then
    SAN_LIST="$SAN_LIST,"
  fi
  SAN_LIST="${SAN_LIST}DNS:${d}"
done

SAFE_NAME=$(printf '%s' "$PRIMARY_DOMAIN" | tr -c 'A-Za-z0-9._-' '_')
KEY_FILE="$OUT_DIR/${SAFE_NAME}.key.pem"
CSR_FILE="$OUT_DIR/${SAFE_NAME}.csr.pem"
CERT_FILE="$OUT_DIR/${SAFE_NAME}.cert.pem"
EXT_FILE="$OUT_DIR/${SAFE_NAME}.ext.cnf"

echo "== 签发服务端证书 =="
echo "CA 目录:     $CA_DIR"
echo "主域名(CN):  $PRIMARY_DOMAIN"
echo "SAN:         $SAN_LIST"
echo "输出目录:    $OUT_DIR"
echo "有效期:      $DAYS 天"
echo

if [ "$DRY_RUN" -eq 1 ]; then
  echo "[--dry-run] 不会实际执行，以下是将要运行的命令："
  echo
  echo "mkdir -p '$OUT_DIR'"
  echo "openssl genrsa -out '$KEY_FILE' 2048"
  echo "openssl req -new -key '$KEY_FILE' -subj '/CN=$PRIMARY_DOMAIN' -out '$CSR_FILE'"
  echo "printf 'subjectAltName=%s\\nextendedKeyUsage=serverAuth\\nkeyUsage=digitalSignature,keyEncipherment' '$SAN_LIST' > '$EXT_FILE'"
  echo "openssl x509 -req -in '$CSR_FILE' -CA '$CA_CERT' -CAkey '$CA_KEY' -CAcreateserial \\"
  echo "  -days $DAYS -sha256 -extfile '$EXT_FILE' -out '$CERT_FILE'"
  echo
  echo "[--dry-run] 未生成任何文件，未接触 CA 私钥。"
  exit 0
fi

if [ ! -f "$CA_KEY" ]; then
  echo "错误: 找不到 CA 私钥 $CA_KEY。" >&2
  echo "本脚本必须在能访问 CA 私钥的机器上运行——README.md 已强调这台机器" >&2
  echo "不应是装机服务器，CA 私钥平时离线保管，仅签发时短暂在线。" >&2
  exit 1
fi
if [ ! -f "$CA_CERT" ]; then
  echo "错误: 找不到 CA 证书 $CA_CERT。" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"

echo "生成服务端私钥（不加密，供 nginx 直接加载）..."
openssl genrsa -out "$KEY_FILE" 2048
chmod 400 "$KEY_FILE"

echo "生成证书签发请求（CSR）..."
openssl req -new -key "$KEY_FILE" -subj "/CN=$PRIMARY_DOMAIN" -out "$CSR_FILE"

echo "写扩展文件（SAN 是校验实际生效的字段，CN 只是兼容旧客户端）..."
cat > "$EXT_FILE" <<EOF
subjectAltName=$SAN_LIST
extendedKeyUsage=serverAuth
keyUsage=digitalSignature,keyEncipherment
EOF

echo "用 CA 私钥签发证书（会提示输入 CA 私钥的密码短语）..."
openssl x509 -req -in "$CSR_FILE" -CA "$CA_CERT" -CAkey "$CA_KEY" -CAcreateserial \
  -days "$DAYS" -sha256 -extfile "$EXT_FILE" -out "$CERT_FILE"
chmod 444 "$CERT_FILE"

rm -f "$CSR_FILE"

echo
echo "完成。"
echo "  私钥:  $KEY_FILE   （权限 400，配 nginx ssl_certificate_key）"
echo "  证书:  $CERT_FILE  （权限 444，配 nginx ssl_certificate；本证书由 deploy/nginx/lober.conf"
echo "                      的 TLS 段引用，那部分改动由另一 agent 负责，见 deploy/ca/README.md）"
echo
echo "有效期到期日:"
openssl x509 -enddate -noout -in "$CERT_FILE"
echo
echo "提醒：本证书是服务端证书，不是要嵌入 iPXE 的那份——嵌入 iPXE 的是 CA 证书"
echo "本身（$CA_CERT）。常规轮换这张服务端证书不需要重编 iPXE。见 README.md。"
