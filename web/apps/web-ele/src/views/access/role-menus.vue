<script setup lang="ts">
import type { RoleMenuList } from '#/api/console/role-menus';
import type { ConsoleRole } from '#/api/console/session';

import { computed, onMounted, reactive, ref } from 'vue';

import { ElAlert, ElButton, ElCard, ElEmpty } from 'element-plus';

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
const labels: Record<ConsoleRole, string> = {
  viewer: '只读用户',
  operator: '运维操作员',
  'hook-author': '脚本与模板作者',
  'hook-approver': '脚本与模板审批人',
  admin: '管理员',
};
const current = computed(() =>
  state.value?.roles.find((item) => item.role === role.value),
);
const menus = computed(
  () =>
    state.value?.menus.filter((menu) =>
      current.value?.availableMenuIds.includes(menu.id),
    ) || [],
);
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
function toggle(id: string, event: Event) {
  const checked = (event.target as HTMLInputElement).checked;
  const ids = new Set(drafts[role.value] || []);
  if (checked) ids.add(id);
  else ids.delete(id);
  drafts[role.value] = [...ids];
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
        <label class="mb-5 block font-medium">配置角色
          <select
            v-model="role"
            aria-label="配置角色"
            :disabled="saving || loading"
            class="ml-4 rounded border bg-transparent p-2"
          >
            <option
              v-for="item in state.roles"
              :key="item.role"
              :value="item.role"
            >
              {{ labels[item.role] }}（{{ item.role }}）
            </option>
          </select>
        </label>
        <div class="mb-5 grid gap-3 sm:grid-cols-2">
          <label
            v-for="menu in menus"
            :key="menu.id"
            class="flex items-center gap-3 rounded border p-4"
          >
            <input
              type="checkbox"
              :value="menu.id"
              :checked="drafts[role]?.includes(menu.id) || menu.required"
              :disabled="menu.required || saving || loading"
              @change="toggle(menu.id, $event)"
            />
            <span>{{ menu.title
              }}<span v-if="menu.required" class="ml-2 text-xs text-gray-500">必留入口</span></span>
          </label>
        </div>
        <p class="mb-5 text-sm text-gray-500">
          可保存空菜单列表，用户将进入“暂无可访问菜单”页面。管理员的权限管理入口必须保留。
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
