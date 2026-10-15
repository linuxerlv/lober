<script setup lang="ts">
import type { Schemas } from '#/api/console/generated';

import { computed } from 'vue';

import {
  ElButton,
  ElCheckbox,
  ElInput,
  ElOption,
  ElSelect,
} from 'element-plus';
const props = defineProps<{
  params: Schemas['ParamDeclDTO'][];
  disabled?: boolean;
}>();
const bindings = defineModel<Record<string, Schemas['BindingDTO']>>({
  required: true,
});
const orphaned = computed(() =>
  Object.keys(bindings.value).filter(
    (name) => !props.params.some((p) => p.name === name),
  ),
);
function set(name: string, source: string) {
  bindings.value[name] = { source, value: '', attr: '', editable: true };
}
</script>
<template>
  <section class="space-y-3">
    <p class="text-sm text-gray-500">
      未设置时沿用参数默认值，向导可填写。写死值或主机属性可锁定，也可允许操作员修改。
    </p>
    <div v-for="param in params" :key="param.name" class="rounded border p-3">
      <div class="mb-2 font-medium">
        {{ param.label || param.name }} <code>{{ param.name }}</code>
      </div>
      <div class="flex flex-wrap gap-2">
        <ElSelect
          :model-value="bindings[param.name]?.source ?? 'inherit'"
          :disabled="disabled"
          aria-label="取值来源"
          @update:model-value="
            $event === 'inherit'
              ? delete bindings[param.name]
              : set(param.name, $event)
          "
        >
          <ElOption label="沿用默认（可填写）" value="inherit" /><ElOption
            label="默认值"
            value="default"
          /><ElOption label="写死的值" value="fixed" /><ElOption
            label="主机属性"
            value="attr"
          />
        </ElSelect>
        <template v-if="bindings[param.name]">
          <ElInput
            v-if="bindings[param.name]!.source === 'fixed'"
            v-model="bindings[param.name]!.value"
            :disabled="disabled"
            type="textarea"
            placeholder="参数值，列表每行一项"
            class="max-w-xl"
          />
          <ElInput
            v-if="bindings[param.name]!.source === 'attr'"
            v-model="bindings[param.name]!.attr"
            :disabled="disabled"
            placeholder="主机属性键"
            class="max-w-sm"
          />
          <ElCheckbox
            v-model="bindings[param.name]!.editable"
            :disabled="disabled"
            >
向导可改
</ElCheckbox>
        </template>
      </div>
    </div>
    <div
      v-for="name in orphaned"
      :key="name"
      class="rounded border border-orange-300 p-2"
    >
      未声明的绑定 {{ name
      }}<ElButton
        v-if="!disabled"
        type="danger"
        link
        @click="delete bindings[name]"
        >
删除绑定
</ElButton>
    </div>
  </section>
</template>
