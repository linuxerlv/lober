<script lang="ts" setup>
import type { ISOSource } from '#/api/console/artifact';

import { ref } from 'vue';

import { ElInput, ElInputNumber, ElOption, ElSelect } from 'element-plus';

export interface SourceDraft {
  source: ISOSource;
  file?: File;
}
const props = defineProps<{ label: string; disabled?: boolean }>();
const emit = defineEmits<{ filePicked: [name: string] }>();
const draft = defineModel<SourceDraft>({ required: true });
const dragOver = ref(false);

const sourceModes: Array<{
  desc: string;
  label: string;
  value: ISOSource['kind'];
}> = [
  {
    value: 'url',
    label: '远程链接下载',
    desc: '由服务器后台从 HTTP/HTTPS 地址拉取',
  },
  {
    value: 'upload',
    label: '浏览器本地上传',
    desc: '从本机选择 ISO 文件分片上传，支持断点续传',
  },
  {
    value: 'path',
    label: '服务器本地路径',
    desc: '直接引用已上传到服务器目录内的 ISO 文件',
  },
];

function formatBytes(bytes?: number) {
  if (!bytes || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let val = bytes;
  let idx = 0;
  while (val >= 1024 && idx < units.length - 1) {
    val /= 1024;
    idx++;
  }
  return `${Number(val.toFixed(2))} ${units[idx]}`;
}

function applyFile(file?: File) {
  draft.value.file = file;
  draft.value.source.size = file?.size;
  if (file?.name) {
    emit('filePicked', file.name.replace(/\.iso$/i, ''));
  }
}

function selectFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  applyFile(file);
}

function dropFile(event: DragEvent) {
  dragOver.value = false;
  if (props.disabled) return;
  const file = event.dataTransfer?.files?.[0];
  if (file) applyFile(file);
}

function changeKind(kind?: ISOSource['kind']) {
  if (kind) draft.value.source.kind = kind;
  draft.value.file = undefined;
  draft.value.source.addr = '';
  draft.value.source.size = undefined;
}
</script>
<template>
  <fieldset
    class="rounded-xl border border-border bg-muted/20 p-4"
    :disabled="props.disabled"
  >
    <legend class="px-2 text-sm font-semibold text-foreground">
      {{ props.label }}
    </legend>

    <!-- 可视化三选一来源方式卡片 -->
    <div class="mb-4">
      <div class="mb-2 text-xs text-muted-foreground">选择获取方式</div>
      <div class="grid grid-cols-1 gap-2.5 sm:grid-cols-3">
        <button
          v-for="mode in sourceModes"
          :key="mode.value"
          type="button"
          :disabled="props.disabled"
          class="rounded-lg border p-3 text-left transition"
          :class="
            draft.source.kind === mode.value
              ? 'border-primary bg-primary/10 ring-1 ring-primary'
              : 'border-border bg-card hover:border-primary/40'
          "
          @click="changeKind(mode.value)"
        >
          <div class="text-xs font-semibold text-foreground">
            {{ mode.label }}
          </div>
          <div class="mt-1 text-[11px] leading-relaxed text-muted-foreground">
            {{ mode.desc }}
          </div>
        </button>
      </div>
      <!-- 保留无障碍与测试兼容的隐藏下拉选择器 -->
      <ElSelect
        v-model="draft.source.kind"
        class="sr-only"
        :aria-label="`${props.label}来源方式`"
        :disabled="props.disabled"
        @change="changeKind()"
      >
        <ElOption value="url" label="远程链接（服务器下载）" />
        <ElOption value="upload" label="浏览器上传（支持续传）" />
        <ElOption value="path" label="服务器本地路径" />
      </ElSelect>
    </div>

    <!-- 方式 1：远程 URL -->
    <label v-if="draft.source.kind === 'url'" class="mb-3 block text-xs">
      <span class="mb-1.5 block font-medium text-foreground">
        镜像下载链接（HTTP / HTTPS）
      </span>
      <ElInput
        v-model="draft.source.addr"
        :aria-label="`${props.label}下载 URL`"
        placeholder="例如 https://mirrors.example.org/rocky/9.4/Rocky-9.4-x86_64-minimal.iso"
        :disabled="props.disabled"
      />
    </label>

    <!-- 方式 2：服务器本地路径 -->
    <label v-else-if="draft.source.kind === 'path'" class="mb-3 block text-xs">
      <span class="mb-1.5 block font-medium text-foreground">
        服务器上的绝对路径
      </span>
      <ElInput
        v-model="draft.source.addr"
        :aria-label="`${props.label}本地路径`"
        placeholder="例如 /var/lib/lober/import/Rocky-9.4-x86_64-minimal.iso"
        :disabled="props.disabled"
      />
      <span class="mt-1 block text-xs text-muted-foreground">
        文件须位于服务器允许导入的白名单目录内，提交后由服务端直接校验读取。
      </span>
    </label>

    <!-- 方式 3：现代化拖拽/点击文件上传区 -->
    <div v-else class="mb-3">
      <span class="mb-1.5 block text-xs font-medium text-foreground">
        上传本地镜像文件
      </span>
      <label
        class="flex cursor-pointer flex-col items-center justify-center rounded-xl border-2 border-dashed p-6 text-center transition"
        :class="
          dragOver
            ? 'border-primary bg-primary/10'
            : draft.file
              ? 'border-emerald-500/50 bg-emerald-500/5'
              : 'border-border bg-card hover:border-primary/50 hover:bg-muted/30'
        "
        @dragover.prevent="dragOver = true"
        @dragleave.prevent="dragOver = false"
        @drop.prevent="dropFile"
      >
        <input
          type="file"
          accept=".iso,.img,.tar.gz,application/octet-stream"
          class="sr-only"
          :aria-label="`${props.label}上传文件`"
          :disabled="props.disabled"
          @change="selectFile"
        />
        <template v-if="draft.file">
          <div class="text-sm font-semibold text-emerald-600 dark:text-emerald-400">
            已选择文件：{{ draft.file.name }}
          </div>
          <div class="mt-1 text-xs text-muted-foreground">
            文件大小：{{ formatBytes(draft.file.size) }}（{{
              draft.file.size.toLocaleString()
            }}
            字节）· 点击可重新选择
          </div>
        </template>
        <template v-else>
          <div class="text-sm font-medium text-foreground">
            点击选择本地镜像文件，或将 .iso 文件拖拽到此处
          </div>
          <div class="mt-1 text-xs text-muted-foreground">
            支持超大镜像自动分片上传与浏览器断点续传
          </div>
        </template>
      </label>
    </div>

    <!-- 折叠式高级校验项（默认收起，保持界面清爽） -->
    <details class="mt-3 rounded-lg border border-border bg-card px-3 py-2 text-xs">
      <summary class="cursor-pointer font-medium text-muted-foreground hover:text-foreground">
        高级校验选项（期望 SHA256 与字节大小，可选）
      </summary>
      <div class="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label>
          <span class="mb-1 block text-muted-foreground">期望 SHA256 摘要（可选）</span>
          <ElInput
            v-model="draft.source.sha256"
            placeholder="64 位十六进制校验和"
            :aria-label="`${props.label}期望 SHA256`"
            :disabled="props.disabled"
          />
        </label>
        <label v-if="draft.source.kind !== 'upload'">
          <span class="mb-1 block text-muted-foreground">文件大小（字节，可选）</span>
          <ElInputNumber
            v-model="draft.source.size"
            :min="0"
            :precision="0"
            :controls="false"
            placeholder="留空由服务端自动探测"
            class="!w-full"
            :aria-label="`${props.label}大小`"
            :disabled="props.disabled"
          />
        </label>
      </div>
    </details>
  </fieldset>
</template>
