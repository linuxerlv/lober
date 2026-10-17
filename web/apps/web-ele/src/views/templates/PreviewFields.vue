<script setup lang="ts">
import type { Schemas } from '#/api/console/generated';

import { ElForm, ElFormItem, ElInput, ElOption, ElSelect } from 'element-plus';

const props = defineProps<{
  fields: Schemas['ParamFieldDTO'][];
  disabled?: boolean;
}>();
const entered = defineModel<Record<string, string>>({ required: true });
function value(field: Schemas['ParamFieldDTO']) {
  return field.locked
    ? field.prefill
    : (entered.value[field.key] ?? field.prefill);
}
function update(field: Schemas['ParamFieldDTO'], input: string | string[]) {
  if (field.locked || props.disabled) return;
  entered.value[field.key] = Array.isArray(input) ? input.join('\n') : input;
}
</script>
<template>
  <ElForm label-width="150px">
    <ElFormItem
      v-for="field in fields"
      :key="field.key"
      :label="field.label"
      :required="field.required"
    >
      <ElSelect
        v-if="field.type === 'enum'"
        :model-value="
          field.list ? value(field).split('\n').filter(Boolean) : value(field)
        "
        :multiple="field.list"
        :disabled="field.locked || disabled"
        clearable
        @update:model-value="update(field, $event)"
      >
        <ElOption
          v-for="option in field.options"
          :key="option.value"
          :label="option.label"
          :value="option.value"
        />
      </ElSelect>
      <ElInput
        v-else
        :model-value="value(field)"
        :type="field.list || field.type === 'text' ? 'textarea' : 'text'"
        :rows="field.list ? 4 : undefined"
        :disabled="field.locked || disabled"
        :placeholder="field.key"
        @update:model-value="update(field, $event)"
      />
      <span v-if="field.locked" class="ml-2 text-sm text-gray-500">已锁定，使用服务端预填值</span>
    </ElFormItem>
  </ElForm>
</template>
