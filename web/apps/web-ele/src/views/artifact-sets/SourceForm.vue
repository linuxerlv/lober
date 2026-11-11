<script lang="ts" setup>
import type { ISOSource } from '#/api/console/artifact';

import { ElInput, ElInputNumber, ElOption, ElSelect } from 'element-plus';

export interface SourceDraft {
  source: ISOSource;
  file?: File;
}
const props = defineProps<{ label: string; disabled?: boolean }>();
const draft = defineModel<SourceDraft>({ required: true });
function selectFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  draft.value.file = file;
  draft.value.source.size = file?.size;
}
function changeKind() {
  draft.value.file = undefined;
  draft.value.source.addr = '';
  draft.value.source.size = undefined;
}
</script>
<template>
  <fieldset class="rounded border p-4" :disabled="props.disabled">
    <legend class="px-1 font-medium">{{ props.label }}</legend>
    <label class="mb-3 block">
      来源方式
      <ElSelect
        v-model="draft.source.kind"
        class="w-full"
        :aria-label="`${props.label}来源方式`"
        :disabled="props.disabled"
        @change="changeKind"
      >
        <ElOption value="url" label="远程链接（服务器下载）" />
        <ElOption value="upload" label="浏览器上传（支持续传）" />
        <ElOption value="path" label="服务器本地路径" />
      </ElSelect>
    </label>
    <label v-if="draft.source.kind === 'url'" class="mb-3 block">
      下载 URL
      <ElInput
        v-model="draft.source.addr"
        :aria-label="`${props.label}下载 URL`"
        placeholder="https://example.org/install.iso"
        :disabled="props.disabled"
      />
    </label>
    <label v-else-if="draft.source.kind === 'path'" class="mb-3 block">
      服务器绝对路径
      <ElInput
        v-model="draft.source.addr"
        :aria-label="`${props.label}本地路径`"
        placeholder="/data/iso/install.iso"
        :disabled="props.disabled"
      />
      <span class="text-muted-foreground text-xs">
        文件须在服务器允许登记的目录内，由服务端检查。
      </span>
    </label>
    <label v-else class="mb-3 block">
      上传文件
      <input
        type="file"
        :aria-label="`${props.label}上传文件`"
        :disabled="props.disabled"
        @change="selectFile"
      />
      <span v-if="draft.file" class="mt-1 block text-xs">
        {{ draft.file.name }} · {{ draft.file.size }} 字节
      </span>
    </label>
    <div class="grid grid-cols-2 gap-3">
      <label>
        期望 SHA256（可选）
        <ElInput
          v-model="draft.source.sha256"
          :aria-label="`${props.label}期望 SHA256`"
          :disabled="props.disabled"
        />
      </label>
      <label v-if="draft.source.kind !== 'upload'">
        大小（字节，可选）
        <ElInputNumber
          v-model="draft.source.size"
          :min="0"
          :precision="0"
          :controls="false"
          class="!w-full"
          :aria-label="`${props.label}大小`"
          :disabled="props.disabled"
        />
      </label>
    </div>
  </fieldset>
</template>
