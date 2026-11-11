<script setup lang="ts">
import type { Schemas } from '#/api/console/generated';

import {
  ElButton,
  ElCheckbox,
  ElForm,
  ElFormItem,
  ElInput,
  ElInputNumber,
  ElOption,
  ElSelect,
} from 'element-plus';

import { newParam } from './authoring';
defineProps<{ disabled?: boolean }>();
const params = defineModel<Schemas['ParamDeclDTO'][]>({ required: true });
const types = [
  'word',
  'int',
  'enum',
  'ip',
  'cidr',
  'hostname',
  'diskpath',
  'sshkey',
  'pwhash',
  'text',
];
</script>
<template>
  <section class="space-y-3">
    <h3 class="text-lg font-semibold">参数声明</h3>
    <ElForm
      v-for="(param, index) in params"
      :key="index"
      :disabled="disabled"
      label-width="100px"
      class="rounded border p-3"
    >
      <div class="grid gap-3 md:grid-cols-2">
        <ElFormItem label="参数名">
<ElInput v-model="param.name" placeholder="例如 disk" />
</ElFormItem>
        <ElFormItem label="显示名称">
<ElInput v-model="param.label" placeholder="例如 系统盘" />
</ElFormItem>
        <ElFormItem label="类型">
<ElSelect v-model="param.type">
<ElOption
              v-for="type in types"
              :key="type"
              :label="type"
              :value="type"
/>
</ElSelect>
</ElFormItem>
        <ElFormItem label="声明选项">
<ElCheckbox v-model="param.list">列表（每行一项）</ElCheckbox><ElCheckbox v-model="param.required">必填</ElCheckbox>
</ElFormItem>
      </div>
      <ElFormItem label="默认值">
<ElInput v-model="param.default" type="textarea" :rows="2" />
</ElFormItem>
      <template v-if="param.type === 'int'">
        <ElFormItem label="最小值">
<ElInputNumber
            :model-value="param.min ?? undefined"
            @update:model-value="param.min = $event ?? null"
        />
</ElFormItem>
        <ElFormItem label="最大值">
<ElInputNumber
            :model-value="param.max ?? undefined"
            @update:model-value="param.max = $event ?? null"
        />
</ElFormItem>
      </template>
      <template v-if="param.type === 'enum'">
        <ElFormItem label="ISO 选项集合">
<ElInput
            v-model="param.offer"
            placeholder="可选，例如 editions；与静态选项二选一"
        />
</ElFormItem>
        <div
          v-for="(option, n) in param.options"
          :key="n"
          class="mb-2 flex gap-2"
        >
          <ElInput
            v-model="option.value"
            aria-label="选项值"
            placeholder="选项值"
          /><ElInput
            v-model="option.label"
            aria-label="选项名称"
            placeholder="选项名称"
          /><ElButton @click="param.options?.splice(n, 1)">删除选项</ElButton>
        </div>
        <ElButton @click="(param.options ??= []).push({ value: '', label: '' })">
添加静态选项
</ElButton>
      </template>
      <ElButton type="danger" plain @click="params.splice(index, 1)">
删除参数
</ElButton>
    </ElForm>
    <ElButton v-if="!disabled" @click="params.push(newParam())">
添加参数
</ElButton>
    <p v-if="params.length === 0" class="text-sm text-gray-500">尚未声明参数</p>
  </section>
</template>
