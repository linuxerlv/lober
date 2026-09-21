#!/usr/bin/env bash
# 部署前必跑的校验命令（在有外网、装好构建工具链的机器上）：
#   bash deploy/ipxe/build-ipxe.sh --dry-run --ca-cert /path/to/ca.cert.pem
# --dry-run 只打印将要执行的 git clone / make 命令与目标产物路径，不实际
# 拉取源码、不实际编译。正式构建前先跑一遍，核对 CA 证书路径与四个目标
# 是否正确。
#
# 为四种固件架构自行编译带 crypto 支持、嵌入内网 CA 的 iPXE 二进制。
#
# 依据 docs/superpowers/plans/2026-09-20-lober-phase4-02-tls.md 任务 4，
# 与 SPEC.md §1.5：iPXE 走 HTTPS 需要重新编译（带 crypto、用 TRUST= 嵌入
# 内网 CA），这是 HTTPS 在这套栈里最贵的部分。
#
# 本脚本必须在一台有外网访问的机器上运行——要拉取 iPXE 源码与编译工具链
# （gcc、binutils、perl 等），与 SPEC.md §12.3 前端构建「必须在有外网的
# 机器上做」同理。编译产物（四个二进制）随后作为文件带入气隙内网，
# 不需要在内网里编译。
#
# 用法：
#   bash build-ipxe.sh --ca-cert <CA证书路径> [--out-dir <输出目录>] \
#        [--ipxe-repo <git地址>] [--ipxe-ref <分支/tag/commit>] [--dry-run]
#
# 示例：
#   bash build-ipxe.sh --ca-cert ./ca.cert.pem --out-dir ./out
set -euo pipefail

CA_CERT=""
OUT_DIR="./out"
IPXE_REPO="https://github.com/ipxe/ipxe.git"
# 固定到具体 commit/tag 而非跟随分支头，理由与 SPEC §12.9「pin 住一个上游
# commit，升级当独立任务做」一致：编译产物要能被复现，且升级 iPXE 版本本身
# 是一个需要独立验证的变更，不该在「为部署固定一个 TLS 版本」的过程中
# 意外发生。这里给的是占位值，正式构建前必须核实并锁定到实际选用的版本。
IPXE_REF="CHANGE-ME-PIN-A-COMMIT-OR-TAG"
DRY_RUN=0

while [ $# -gt 0 ]; do
  case "$1" in
    --ca-cert)
      CA_CERT="$2"
      shift 2
      ;;
    --out-dir)
      OUT_DIR="$2"
      shift 2
      ;;
    --ipxe-repo)
      IPXE_REPO="$2"
      shift 2
      ;;
    --ipxe-ref)
      IPXE_REF="$2"
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

if [ -z "$CA_CERT" ]; then
  echo "错误: 必须指定 --ca-cert（内网 CA 的证书文件，来自 deploy/ca/gen-ca.sh 的产物）。" >&2
  echo "注意: 这里嵌入的是 CA 证书，不是某一张服务端证书——见 deploy/ca/README.md" >&2
  echo "「有效期与轮换节奏」一节，嵌 CA 是常规轮换不触碰 iPXE 的关键。" >&2
  exit 1
fi

SRC_DIR="$OUT_DIR/src/ipxe"
BIN_DIR="$OUT_DIR/bin"

# 四个编译目标，对应 SPEC.md §1.3 的四条 dhcp-boot 配置与四种固件架构。
# 每一项：iPXE 的 make 目标路径、TFTP 目录里最终应该叫的文件名、说明。
#
# BIOS 目标传统上叫 undionly.kpxe（UNDI 驱动 + iPXE，不需要 crypto 时的
# 常规选择）。加了 crypto 之后二进制会变大（见 README「坑」一节），但
# 目标文件名与格式不变——变的只是编译时传的 make 变量。
declare -a TARGETS=(
  "bin/undionly.kpxe|undionly.kpxe|BIOS PXE（对应 SPEC §1.3 的 dhcp-match=set:bios,option:client-arch,0）"
  "bin-i386-efi/snponly.efi|snponlyx32.efi|IA32 UEFI（对应 efi32，option:client-arch,6）"
  "bin-x86_64-efi/snponly.efi|snponly.efi|x64 UEFI（对应 efi64，option:client-arch,7/9）"
  "bin-arm64-efi/snponly.efi|snp-arm64.efi|ARM64 UEFI（对应 arm64，option:client-arch,11）"
)

echo "== iPXE 带 crypto 重新编译 =="
echo "CA 证书:    $CA_CERT"
echo "输出目录:   $OUT_DIR"
echo "iPXE 仓库:  $IPXE_REPO"
echo "iPXE 版本:  $IPXE_REF"
echo

if [ "$IPXE_REF" = "CHANGE-ME-PIN-A-COMMIT-OR-TAG" ]; then
  echo "警告: --ipxe-ref 仍是占位值，未锁定具体版本。" >&2
  echo "正式构建前必须选定并锁定一个 iPXE 的 commit 或 tag——" >&2
  echo "跟随分支头会让「今天编的」与「下次编的」可能是不同代码。" >&2
  if [ "$DRY_RUN" -eq 0 ]; then
    echo "拒绝以占位版本号执行真实构建。加 --dry-run 可以先看看命令，" >&2
    echo "或用 --ipxe-ref 传入锁定后的版本。" >&2
    exit 1
  fi
fi

if [ "$DRY_RUN" -eq 1 ]; then
  echo "[--dry-run] 不会实际执行，以下是将要运行的命令与产物："
  echo
  echo "git clone '$IPXE_REPO' '$SRC_DIR'"
  echo "git -C '$SRC_DIR' checkout '$IPXE_REF'"
  echo
  for t in "${TARGETS[@]}"; do
    IFS='|' read -r make_target out_name desc <<< "$t"
    echo "# $desc"
    echo "make -C '$SRC_DIR/src' '$make_target' \\"
    echo "  EMBED= \\"
    echo "  TRUST='$(cd "$(dirname "$CA_CERT")" 2>/dev/null && pwd)/$(basename "$CA_CERT")' \\"
    echo "  CERT=none"
    echo "cp '$SRC_DIR/src/$make_target' '$BIN_DIR/$out_name'"
    echo "sha256sum '$BIN_DIR/$out_name'"
    echo
  done
  echo "[--dry-run] 未拉取源码、未编译、未生成任何文件。"
  exit 0
fi

if [ ! -f "$CA_CERT" ]; then
  echo "错误: 找不到 CA 证书文件 $CA_CERT。" >&2
  exit 1
fi

CA_CERT_ABS="$(cd "$(dirname "$CA_CERT")" && pwd)/$(basename "$CA_CERT")"

mkdir -p "$OUT_DIR" "$BIN_DIR"

if [ ! -d "$SRC_DIR" ]; then
  echo "克隆 iPXE 源码..."
  git clone "$IPXE_REPO" "$SRC_DIR"
fi
echo "切到锁定版本 $IPXE_REF..."
git -C "$SRC_DIR" fetch --tags
git -C "$SRC_DIR" checkout "$IPXE_REF"

echo "记录本次构建用的 iPXE 版本..."
git -C "$SRC_DIR" rev-parse HEAD > "$BIN_DIR/ipxe-source-commit.txt"

for t in "${TARGETS[@]}"; do
  IFS='|' read -r make_target out_name desc <<< "$t"
  echo
  echo "== 编译 $desc =="
  echo "make 目标: $make_target"

  # EMBED= 留空：SPEC §1.5 与 §4.1 已明确首跳脚本不烧进 iPXE 二进制，而是
  # 由 DHCP 给出网关地址（/boot.ipxe 是全网共用的静态一行，见 SPEC §4.1）。
  # 这条约束在 TLS 改造后不变——重编译只是为了加 crypto 与嵌 CA，不改变
  # 「脚本来自 DHCP 而非二进制」这个设计决定。
  #
  # TRUST= 指向 CA 证书文件：这是把内网 CA 嵌进二进制的具体机制。iPXE 的
  # TRUST 变量接受一个或多个 PEM 格式证书文件路径，编译后 iPXE 在做 TLS
  # 握手时只信任由这些证书（或它们签发的证书）建立的链——这正是「嵌 CA
  # 而非嵌某张服务端证书」的实现方式，见 deploy/ca/README.md。
  #
  # CERT=none：不额外嵌入 iPXE 官方的默认信任锚点，只信任我们自己的内网 CA。
  # 若保留默认值，iPXE 会同时信任 ipxe.org 提供的一组公网证书——在气隙内网
  # 场景下这些证书永远不会被用到，但没有理由让二进制隐式信任一组我们不管理
  # 的证书。
  make -C "$SRC_DIR/src" "$make_target" \
    EMBED= \
    TRUST="$CA_CERT_ABS" \
    CERT=none

  cp "$SRC_DIR/src/$make_target" "$BIN_DIR/$out_name"
done

echo
echo "== 编译完成，计算校验值 =="
(
  cd "$BIN_DIR"
  sha256sum undionly.kpxe snponlyx32.efi snponly.efi snp-arm64.efi > sha256sums.txt
  cat sha256sums.txt
)

echo
echo "产物目录: $BIN_DIR"
echo "  undionly.kpxe    BIOS PXE"
echo "  snponlyx32.efi   IA32 UEFI"
echo "  snponly.efi      x64 UEFI"
echo "  snp-arm64.efi    ARM64 UEFI"
echo "  sha256sums.txt   四个二进制的校验值（README.md 要求记录）"
echo "  ipxe-source-commit.txt  本次构建用的 iPXE 源码 commit"
echo
echo "下一步（README.md「编译后必须做的验证」一节）："
echo "  1. 不要只看编译成功——用一次实际 HTTPS 请求验证嵌入的 CA 证书真的生效。"
echo "  2. 记录每个二进制相对未加 crypto 版本的体积增幅（判断是否需要 UEFI HTTP Boot 的依据）。"
echo "  3. 把这四个文件与 sha256sums.txt 一起带入内网，替换 TFTP 目录里的同名文件。"
