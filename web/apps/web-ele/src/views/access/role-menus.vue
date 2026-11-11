<script setup lang="ts">
import type { TreeInstance } from 'element-plus';

import type { RoleMenuList } from '#/api/console/role-menus';
import type { ConsoleRole } from '#/api/console/session';

import { computed, onMounted, reactive, ref, watch } from 'vue';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElEmpty,
  ElForm,
  ElFormItem,
  ElOption,
  ElSelect,
  ElTag,
  ElTree,
} from 'element-plus';

import { roleLabels } from '#/api/console/accounts';
import { getRoleMenusApi, saveRoleMenusApi } from '#/api/console/role-menus';
import { useConsoleAccess } from '#/composables/use-console-access';
import { useAuthStore } from '#/store';
const { can, refresh } = useConsoleAccess();
const auth = useAuthStore();
const role = ref<ConsoleRole>('viewer');
const state = ref<null | RoleMenuList>(null);
const drafts = reactive<Record<string, string[]>>({});
const error = ref('');
const loading = ref(false);
const notice = ref('');
const saving = ref(false);
const tree = ref<TreeInstance>();
const current = computed(() =>
  state.value?.roles.find((item) => item.role === role.value),
);
const menus = computed(
  () =>
    state.value?.menus.filter((menu) =>
      current.value?.availableMenuIds.includes(menu.id),
    ) || [],
);
const treeData = computed(() =>
  (state.value?.menus.filter((menu) => !menu.parentId) || []).flatMap(
    (group) => {
      const children = menus.value
        .filter((menu) => menu.parentId === group.id)
        .map((menu) => ({
          ...menu,
          disabled: menu.required || saving.value || loading.value,
        }));
      return children.length
        ? [{ ...group, disabled: saving.value || loading.value, children }]
        : [];
    },
  ),
);
const selectedIds = computed(() =>
  menus.value
    .filter((menu) => menu.required || drafts[role.value]?.includes(menu.id))
    .map((menu) => menu.id),
);
watch(
  [tree, selectedIds],
  () => tree.value?.setCheckedKeys(selectedIds.value),
  { flush: 'post' },
);
watch(role, () => {
  notice.value = '';
});
async function load() {
  if (!can('user:manage')) return;
  loading.value = true;
  error.value = '';
  notice.value = '';
  try {
    const next = await getRoleMenusApi();
    state.value = next;
    for (const item of next.roles) drafts[item.role] = [...item.menuIds];
  } catch (e: any) {
    error.value =
      e?.response?.data?.message || e?.message || '读取配置失败，请重试';
  } finally {
    loading.value = false;
  }
}
function updateDraft() {
  const checked = new Set(tree.value?.getCheckedKeys(true) || []);
  drafts[role.value] = menus.value
    .filter((menu) => menu.required || checked.has(menu.id))
    .map((menu) => menu.id);
  notice.value = '';
}
async function save() {
  if (saving.value || !current.value || !can('user:manage')) return;
  saving.value = true;
  error.value = '';
  notice.value = '';
  const target = role.value;
  try {
    const menuIds = menus.value
      .filter((menu) => drafts[target]?.includes(menu.id) || menu.required)
      .map((menu) => menu.id);
    const result = await saveRoleMenusApi(target, {
      menuIds,
      rev: current.value.rev,
    });
    const index = state.value!.roles.findIndex((item) => item.role === target);
    state.value!.roles[index] = result;
    drafts[target] = [...result.menuIds];
    notice.value =
      '配置已保存，同角色用户将在下次导航、窗口聚焦或 30 秒内刷新。';
    if (target === auth.sessionUser?.role) await refresh();
  } catch (e: any) {
    error.value =
      e?.response?.status === 409
        ? '配置已被其他管理员修改，已保留当前编辑内容，请重新读取后再保存。'
        : e?.response?.data?.message || e?.message || '保存失败，请重试';
  } finally {
    saving.value = false;
  }
}
onMounted(load);
</script>
<template>
  <main class="p-6">
    <h1 class="mb-2 text-2xl font-semibold">角色与菜单</h1>
    <p class="mb-6 text-sm text-gray-500">
      选择角色并配置可见菜单。菜单分配受角色已有权限约束，操作按钮仍按权限控制。
    </p>
    <ElAlert
      v-if="!can('user:manage')"
      title="当前账号没有角色菜单管理权限"
      type="error"
      :closable="false"
    />
    <ElCard v-else class="max-w-3xl">
      <ElAlert
        v-if="error"
        class="mb-4"
        :title="error"
        type="error"
        :closable="false"
        role="alert"
      />
      <ElAlert
        v-if="notice"
        class="mb-4"
        :title="notice"
        type="success"
        :closable="false"
      />
      <template v-if="state">
        <ElForm label-position="top" :disabled="saving || loading">
          <ElFormItem label="配置角色">
            <ElSelect
              v-model="role"
              aria-label="配置角色"
              class="w-full max-w-sm"
            >
              <ElOption
                v-for="item in state.roles"
                :key="item.role"
                :value="item.role"
                :label="`${roleLabels[item.role]}（${item.role}）`"
              />
            </ElSelect>
          </ElFormItem>
          <ElFormItem label="可见菜单（二级）">
            <ElTree
              ref="tree"
              class="w-full rounded border p-3"
              :data="treeData"
              node-key="id"
              :props="{ label: 'title', disabled: 'disabled' }"
              show-checkbox
              default-expand-all
              :expand-on-click-node="false"
              aria-label="可见菜单"
              @check="updateDraft"
            >
              <template #default="{ data }">
                <span class="flex items-center gap-2">
                  {{ data.title }}
                  <ElTag v-if="data.required" size="small" type="info">必留入口</ElTag>
                </span>
              </template>
            </ElTree>
          </ElFormItem>
        </ElForm>
        <p class="mb-5 text-sm text-gray-500">
          勾选一级目录可批量选择二级菜单，部分选中时目录显示半选。仅保存勾选的二级菜单，空目录自动隐藏。可保存空菜单列表，管理员的“角色与菜单”入口必须保留。
        </p>
      </template>
      <ElEmpty v-else-if="!loading && !error" description="暂无配置" />
      <ElButton
        data-action="save"
        type="primary"
        :loading="saving"
        :disabled="!state || loading || saving"
        @click="save"
      >
        保存配置
      </ElButton>
      <ElButton :disabled="saving || loading" :loading="loading" @click="load">
        重新读取
      </ElButton>
    </ElCard>
  </main>
</template>
