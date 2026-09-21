#!/usr/bin/env bash
#
# 部署前必跑（本 worktree 未装 packer/mkosi，脚本本身的逻辑在此环境用假数据
# 验证，见 pipeline/README.md 第 6 节）：
#   bash pipeline/scripts/package.sh --help
#   # 用一个假的 raw 镜像文件跑一遍全流程，确认产出四件套且结构正确：
#   dd if=/dev/zero of=/tmp/fake.raw bs=1M count=8
#   bash pipeline/scripts/package.sh \
#     --image /tmp/fake.raw --os-family rocky --os-version 9.4 \
#     --arch x86_64 --firmware any --source-iso /tmp/fake.raw \
#     --tool mkosi --tool-version 1.0.0 --packages-file /tmp/fake_packages.txt \
#     --output /tmp/pkg-out
#   ls /tmp/pkg-out   # 应看到 image.raw.zst manifest.yaml meta.yaml（sig 由任务 3 补）
#
# 用途：把 mkosi/Packer 产出的原始镜像，打包成 pipeline/README.md 第 2 节定义的
# 四件套交付物中的前两件（image.raw.zst、manifest.yaml、meta.yaml）——签名
# （manifest.yaml.sig）是计划任务 3 的产出，本脚本不生成，只留出这一件在
# 输出目录里缺失的事实（任务 3 的 sign.sh 补上后，目录才算完整）。
#
# 三条工具链（mkosi/Packer-Windows/Packer-ESXi）共用本脚本，理由见
# pipeline/README.md 第 2.1 节：压缩统一用系统 zstd 命令，避免三条工具链
# 各自发明不同的压缩实现导致产物格式不一致。
#
# 依赖：zstd、sha256sum、sort——三者都是常见 Linux 构建机的标准工具，不依赖
# yq/jq 等需要额外安装的 YAML/JSON 处理器（本 worktree 环境核对过没有装 yq，
# 不假设目标构建机装了它）。

set -euo pipefail

usage() {
  cat <<'EOF'
用法：
  package.sh --image <path> --os-family <family> --os-version <version> \
              --arch <arch> --firmware <any|uefi|bios> \
              --source-iso <path> --tool <name> --tool-version <version> \
              --packages-file <path> --output <dir>

参数：
  --image          原始镜像文件路径（未压缩，raw 格式）
  --os-family       对应 model.ArtifactSet.os_family（rhel/debian/ubuntu/suse/windows/esxi）
  --os-version      对应 model.ArtifactSet.os_version
  --arch             对应 model.ArtifactSet.arch
  --firmware         对应 model.ArtifactSet.firmware（any/uefi/bios，见 SPEC §2.1）
  --source-iso       原始 ISO 路径，用于计算 source_iso_sha256（可追溯性，见 README 第 2.4 节）
  --tool              封装工具名（mkosi/packer），写进 meta.yaml 排查用
  --tool-version      封装工具版本，写进 meta.yaml 排查用
  --packages-file     软件包清单文件，每行一个「包名==版本」字符串
  --output            输出目录，脚本在其中产出 image.raw.zst/manifest.yaml/meta.yaml
  --partition-table   分区表类型，gpt 或 mbr（默认 gpt，对应 mkosi 的 legacy-free 产物，
                        见 pipeline/README.md 第 1 节核实结论）

--partition-layout-json 之类的具体分区描述本脚本不接（README openQuestions 第 2 条：
分区布局的具体 schema 留给计划任务 4 的实现者定义），manifest.yaml 里 partition_layout
字段本脚本只写 table_type，partitions 留空数组占位。
EOF
}

# --- 参数解析：手写 getopt 风格循环，不依赖 GNU getopt 的长选项扩展
# （macOS/BSD 环境的 getopt 不支持长选项，写成手写循环避免这个跨平台坑，
# 尽管本项目当前只需要在 Linux 构建机上跑，但脚本本身不该无谓地依赖
# GNU 特有的 getopt 行为）---
IMAGE="" OS_FAMILY="" OS_VERSION="" ARCH="" FIRMWARE="" SOURCE_ISO=""
TOOL="" TOOL_VERSION="" PACKAGES_FILE="" OUTPUT="" PARTITION_TABLE="gpt"

while [ $# -gt 0 ]; do
  case "$1" in
    --image) IMAGE="$2"; shift 2 ;;
    --os-family) OS_FAMILY="$2"; shift 2 ;;
    --os-version) OS_VERSION="$2"; shift 2 ;;
    --arch) ARCH="$2"; shift 2 ;;
    --firmware) FIRMWARE="$2"; shift 2 ;;
    --source-iso) SOURCE_ISO="$2"; shift 2 ;;
    --tool) TOOL="$2"; shift 2 ;;
    --tool-version) TOOL_VERSION="$2"; shift 2 ;;
    --packages-file) PACKAGES_FILE="$2"; shift 2 ;;
    --output) OUTPUT="$2"; shift 2 ;;
    --partition-table) PARTITION_TABLE="$2"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "未知参数：$1" >&2; usage >&2; exit 1 ;;
  esac
done

for name_value in "IMAGE:$IMAGE" "OS_FAMILY:$OS_FAMILY" "OS_VERSION:$OS_VERSION" \
                   "ARCH:$ARCH" "FIRMWARE:$FIRMWARE" "SOURCE_ISO:$SOURCE_ISO" \
                   "TOOL:$TOOL" "TOOL_VERSION:$TOOL_VERSION" \
                   "PACKAGES_FILE:$PACKAGES_FILE" "OUTPUT:$OUTPUT"; do
  key="${name_value%%:*}"
  value="${name_value#*:}"
  if [ -z "$value" ]; then
    echo "缺少必需参数：--$(echo "$key" | tr '[:upper:]' '[:lower:]' | tr '_' '-')" >&2
    usage >&2
    exit 1
  fi
done

if [ ! -f "$IMAGE" ]; then
  echo "镜像文件不存在：$IMAGE" >&2
  exit 1
fi
if [ ! -f "$SOURCE_ISO" ]; then
  echo "源 ISO 文件不存在：$SOURCE_ISO" >&2
  exit 1
fi
if [ ! -f "$PACKAGES_FILE" ]; then
  echo "软件包清单文件不存在：$PACKAGES_FILE" >&2
  exit 1
fi
case "$FIRMWARE" in
  any|uefi|bios) ;;
  *) echo "firmware 取值必须是 any/uefi/bios 之一，收到：$FIRMWARE" >&2; exit 1 ;;
esac
case "$PARTITION_TABLE" in
  gpt|mbr) ;;
  *) echo "partition-table 取值必须是 gpt/mbr 之一，收到：$PARTITION_TABLE" >&2; exit 1 ;;
esac

mkdir -p "$OUTPUT"

echo "==> 计算解压态（原始镜像）sha256" >&2
IMAGE_UNCOMPRESSED_SHA256="$(sha256sum "$IMAGE" | cut -d' ' -f1)"
IMAGE_UNCOMPRESSED_SIZE="$(stat -c%s "$IMAGE" 2>/dev/null || stat -f%z "$IMAGE")"

echo "==> 计算源 ISO sha256（可追溯性字段）" >&2
SOURCE_ISO_SHA256="$(sha256sum "$SOURCE_ISO" | cut -d' ' -f1)"

echo "==> 压缩镜像（zstd，见 pipeline/README.md 第 2.1 节的压缩算法选择说明）" >&2
zstd -q -f "$IMAGE" -o "$OUTPUT/image.raw.zst"

echo "==> 计算压缩态（image.raw.zst）sha256——导入时校验对象，见 README 第 4 节" >&2
IMAGE_SHA256="$(sha256sum "$OUTPUT/image.raw.zst" | cut -d' ' -f1)"
IMAGE_SIZE="$(stat -c%s "$OUTPUT/image.raw.zst" 2>/dev/null || stat -f%z "$OUTPUT/image.raw.zst")"

echo "==> 计算软件包清单 sha256（可重现性验证字段，见 README 第 5 节）" >&2
# 排序后再算 sha256：保证同一组包不因枚举顺序不同产生不同的哈希——这是
# 「可重现」这个要求在本脚本里的具体落实方式（计划任务 2 明确要求）。
SORTED_PACKAGES="$(sort "$PACKAGES_FILE")"
PACKAGE_LIST_SHA256="$(printf '%s' "$SORTED_PACKAGES" | sha256sum | cut -d' ' -f1)"

echo "==> 写 manifest.yaml" >&2
{
  echo "schema_version: 1"
  echo "image_sha256: \"$IMAGE_SHA256\""
  echo "image_uncompressed_sha256: \"$IMAGE_UNCOMPRESSED_SHA256\""
  echo "image_size_bytes: $IMAGE_SIZE"
  echo "image_uncompressed_size_bytes: $IMAGE_UNCOMPRESSED_SIZE"
  echo "partition_layout:"
  echo "  table_type: $PARTITION_TABLE"
  # partitions 留空数组：具体分区描述的 schema 由计划任务 4 的实现者定义，
  # 见 pipeline/README.md openQuestions 第 2 条，本脚本不预先猜测其结构。
  echo "  partitions: []"
  echo "package_list_sha256: \"$PACKAGE_LIST_SHA256\""
  echo "packages:"
  while IFS= read -r pkg; do
    [ -z "$pkg" ] && continue
    echo "  - \"$pkg\""
  done <<< "$SORTED_PACKAGES"
} > "$OUTPUT/manifest.yaml"

echo "==> 写 meta.yaml" >&2
PACKAGED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
{
  echo "os_family: $OS_FAMILY"
  echo "os_version: \"$OS_VERSION\""
  echo "arch: $ARCH"
  echo "firmware: $FIRMWARE"
  echo "packaged_at: \"$PACKAGED_AT\""
  echo "source_iso_sha256: \"$SOURCE_ISO_SHA256\""
  echo "packaging_tool: $TOOL"
  echo "packaging_tool_version: \"$TOOL_VERSION\""
} > "$OUTPUT/meta.yaml"

echo "==> 完成。输出目录：$OUTPUT" >&2
echo "==> 注意：manifest.yaml.sig 尚未生成——签名是计划任务 3 的产出，本脚本不做，" >&2
echo "    见 pipeline/README.md 第 2.3/3.1 节。" >&2
ls -la "$OUTPUT" >&2
