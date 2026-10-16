<script setup lang="ts">
import type { Host } from '#/api/console/operations';
import { reactive, ref } from 'vue';
import {
  ElAlert,
  ElButton,
  ElCard,
  ElDialog,
  ElInput,
  ElMessageBox,
  ElTable,
  ElTableColumn,
} from 'element-plus';
import { operations } from '#/api/console/operations';
import AccessButton from '#/components/access-button.vue';
import { useConsoleAccess } from '#/composables/use-console-access';
import { errorMessage, useConsoleList } from '#/composables/use-console-list';
const { can, run } = useConsoleAccess();
const prefix = ref('');
const { items, next, loading, error, load } = useConsoleList((cursor) =>
  operations.hosts({ prefix: prefix.value, cursor, limit: 100 }),
);
const editor = ref(false),
  editing = ref<Host | null>(null),
  saving = ref(false);
const form = reactive({
  sn: '',
  arch: 'x86_64',
  firmware: 'uefi',
  attrs: '{}',
});
function edit(item?: Host) {
  if (!can('host:write')) return;
  editing.value = item || null;
  Object.assign(form, {
    sn: item?.sn || '',
    arch: item?.arch || 'x86_64',
    firmware: item?.firmware || 'uefi',
    attrs: JSON.stringify(item?.attrs || {}, null, 2),
  });
  editor.value = true;
}
async function save() {
  if (saving.value || !can('host:write')) return;
  saving.value = true;
  try {
    const attrs = JSON.parse(form.attrs);
    if (
      !attrs ||
      Array.isArray(attrs) ||
      Object.values(attrs).some((v) => typeof v !== 'string')
    )
      throw new Error('主机属性必须为键值都是字符串的 JSON 对象');
    const data = {
      sn: form.sn.trim(),
      arch: form.arch,
      firmware: form.firmware,
      attrs,
    };
    const result = await run('host:write', () =>
      editing.value
        ? operations.updateHost({ ...data, rev: editing.value.rev })
        : operations.createHost(data),
    );
    if (result) {
      editor.value = false;
      await load();
    }
  } catch (e) {
    error.value = errorMessage(e);
  } finally {
    saving.value = false;
  }
}
async function remove(item: Host) {
  try {
    await ElMessageBox.confirm(`确认删除主机 ${item.sn}？`, '删除主机');
  } catch {
    return;
  }
  try {
    if (
      (await run('host:write', () => operations.deleteHost(item))) !== undefined
    )
      await load();
  } catch (e) {
    error.value = errorMessage(e);
  }
}
</script>
<template>
  <main class="p-6">
    <header class="mb-5 flex flex-wrap items-center gap-3">
      <h1 class="mr-auto text-2xl font-semibold">主机清单</h1>
      <ElInput
        v-model="prefix"
        class="!w-56"
        placeholder="SN 前缀"
        @keyup.enter="load()"
      /><ElButton :loading="loading" @click="load()">查询</ElButton
      ><AccessButton permission="host:write" type="primary" @click="edit()"
        >手工录入主机</AccessButton
      >
    </header>
    <ElAlert
      v-if="error"
      class="mb-4"
      :title="error"
      type="error"
      :closable="false"
    /><ElCard
      ><ElTable :data="items" row-key="sn"
        ><ElTableColumn prop="sn" label="SN" min-width="180" /><ElTableColumn
          prop="arch"
          label="架构"
        /><ElTableColumn prop="firmware" label="固件" /><ElTableColumn
          label="主机属性"
          min-width="240"
          ><template #default="{ row }"
            ><span v-for="(value, key) in row.attrs" :key="key" class="mr-3"
              >{{ key }}：{{ value }}</span
            ></template
          ></ElTableColumn
        ><ElTableColumn v-if="can('host:write')" label="操作" width="160"
          ><template #default="{ row }"
            ><AccessButton
              permission="host:write"
              link
              @click="edit(row as Host)"
              >编辑</AccessButton
            ><AccessButton
              permission="host:write"
              link
              type="danger"
              @click="remove(row as Host)"
              >删除</AccessButton
            ></template
          ></ElTableColumn
        ></ElTable
      ><ElButton v-if="next" class="mt-4" :loading="loading" @click="load(true)"
        >加载更多</ElButton
      ></ElCard
    ><ElDialog
      v-model="editor"
      :title="editing ? '编辑主机' : '手工录入主机'"
      width="min(580px,95vw)"
      ><form @submit.prevent="save" class="grid gap-4">
        <label>SN<ElInput v-model="form.sn" :disabled="!!editing" /></label
        ><label
          >架构<select
            v-model="form.arch"
            class="ml-3 rounded border bg-transparent p-2"
          >
            <option>x86_64</option>
            <option>arm64</option>
          </select></label
        ><label
          >固件<select
            v-model="form.firmware"
            class="ml-3 rounded border bg-transparent p-2"
          >
            <option>uefi</option>
            <option>bios</option>
          </select></label
        ><label
          >主机属性（JSON 字符串对象）<ElInput
            v-model="form.attrs"
            type="textarea"
            :rows="6"
            placeholder='{"rack":"A-1","mac":"00:11:22:33:44:55"}'
        /></label>
        <div>
          <AccessButton
            permission="host:write"
            native-type="submit"
            type="primary"
            :loading="saving"
            >保存</AccessButton
          ><ElButton @click="editor = false">取消</ElButton>
        </div>
      </form></ElDialog
    >
  </main>
</template>
