<script setup lang="ts">
import type { TreeInstance } from 'element-plus';

import type { RoleMenuList } from '#/api/console/role-menus';
import type { ConsoleRole } from '#/api/console/session';
import type { User } from '#/api/console/accounts';

import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute } from 'vue-router';

import { Page } from '@vben/common-ui';

import {
  ElAlert,
  ElButton,
  ElEmpty,
  ElTable,
  ElTableColumn,
  ElTabPane,
  ElTabs,
  ElTag,
  ElTree,
} from 'element-plus';

import { roleLabels } from '#/api/console/accounts';
import { getRoleMenusApi, saveRoleMenusApi } from '#/api/console/role-menus';
import { useConsoleAccess } from '#/composables/use-console-access';
import { useAuthStore } from '#/store';
import PermissionGroups from './PermissionGroups.vue';
import { readAccountDirectory } from './account-directory';
import { roleDescriptions, rolePermissionGroups } from './role-permissions';
const { can, refresh } = useConsoleAccess();
const auth = useAuthStore();
const route = useRoute();
const role = computed(() =>
  typeof route.query.role === 'string' &&
  Object.hasOwn(roleLabels, route.query.role)
    ? (route.query.role as ConsoleRole)
    : undefined,
);
const section = ref('permissions');
const accounts = ref<User[]>();
const accountsError = ref('');
const accountsLoading = ref(false);
const state = ref<null | RoleMenuList>(null);
const drafts = reactive<Partial<Record<ConsoleRole, string[]>>>({});
const error = ref('');
const loading = ref(false);
const notice = ref('');
const saving = ref(false);
const tree = ref<TreeInstance>();
const current = computed(() =>
  state.value?.roles.find((item) => item.role === role.value),
);
const permissions = computed(() =>
  role.value ? rolePermissionGroups(role.value) : [],
);
const members = computed(
  () => accounts.value?.filter((account) => account.role === role.value) ?? [],
);
function accountCount(target: ConsoleRole) {
  return (
    accounts.value?.filter((account) => account.role === target).length ?? '—'
  );
}
function roleLink(target?: ConsoleRole) {
  return { path: '/access/role-menus', query: target ? { role: target } : {} };
}
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
    .filter(
      (menu) =>
        menu.required || (role.value && drafts[role.value]?.includes(menu.id)),
    )
    .map((menu) => menu.id),
);
watch(
  [tree, selectedIds],
  () => tree.value?.setCheckedKeys(selectedIds.value),
  { flush: 'post' },
);
watch(role, () => {
  notice.value = '';
  error.value = '';
  section.value = 'permissions';
});
async function loadAccounts() {
  if (!can('user:manage') || accountsLoading.value) return;
  accountsLoading.value = true;
  accountsError.value = '';
  try {
    accounts.value = await readAccountDirectory();
  } catch {
    accounts.value = undefined;
    accountsError.value = '读取关联账号失败，请重试。';
  } finally {
    accountsLoading.value = false;
  }
}
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
  if (!role.value) return;
  const checked = new Set(tree.value?.getCheckedKeys(true) || []);
  drafts[role.value] = menus.value
    .filter((menu) => menu.required || checked.has(menu.id))
    .map((menu) => menu.id);
  notice.value = '';
}
async function save() {
  if (saving.value || !current.value || !role.value || !can('user:manage'))
    return;
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
    notice.value = `已保存「${roleLabels[target]}」的菜单配置。该角色的账号刷新权限后生效。`;
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
onMounted(() => {
  void load();
  void loadAccounts();
});
watch(
  () => route.path,
  (path, previous) => {
    if (path === '/access/role-menus' && previous !== path) void loadAccounts();
  },
);
</script>
<template>
  <Page
    :title="role ? roleLabels[role] : '角色管理'"
    :description="
      role
        ? roleDescriptions[role]
        : '查看内置角色的操作权限、关联账号和可见菜单。'
    "
  >
    <template #extra>
      <RouterLink v-if="role" :to="roleLink()"
        ><ElButton>返回角色列表</ElButton></RouterLink
      >
      <ElButton
        v-else
        :loading="loading || accountsLoading"
        @click="
          load();
          loadAccounts();
        "
        >刷新列表</ElButton
      >
    </template>
    <ElAlert
      v-if="!can('user:manage')"
      title="当前账号没有角色管理权限"
      type="error"
      :closable="false"
    />
    <section
      v-else
      class="overflow-hidden rounded-lg border border-border bg-card"
    >
      <div v-if="error || accountsError" class="grid gap-2 px-5 py-3">
        <ElAlert
          v-if="error"
          :title="error"
          type="error"
          :closable="false"
          role="alert"
        >
          <ElButton link @click="load">重新读取配置</ElButton>
        </ElAlert>
        <ElAlert
          v-if="accountsError"
          :title="accountsError"
          type="warning"
          :closable="false"
        >
          <ElButton link @click="loadAccounts">重新读取账号</ElButton>
        </ElAlert>
      </div>
      <template v-if="!role">
        <div
          class="flex items-center justify-between border-b border-border px-5 py-4"
        >
          <h2 class="font-semibold">内置角色</h2>
          <RouterLink
            v-if="auth.sessionUser?.menuIds.includes('users')"
            class="text-sm text-primary"
            to="/access/users"
            >账号管理</RouterLink
          >
        </div>
        <ElTable
          style="--el-table-header-bg-color: hsl(var(--accent))"
          :data="state?.roles ?? []"
          row-key="role"
          :empty-text="loading ? '正在读取角色…' : '暂无角色'"
        >
          <ElTableColumn label="角色" min-width="190">
            <template #default="{ row }"
              ><strong>{{ roleLabels[row.role as ConsoleRole] }}</strong>
              <div class="text-xs text-muted-foreground">
                {{ row.role }}
              </div></template
            >
          </ElTableColumn>
          <ElTableColumn label="用途" min-width="260">
            <template #default="{ row }">{{
              roleDescriptions[row.role as ConsoleRole]
            }}</template>
          </ElTableColumn>
          <ElTableColumn label="关联账号" width="115">
            <template #default="{ row }">{{ accountCount(row.role) }}</template>
          </ElTableColumn>
          <ElTableColumn label="可见菜单" width="115">
            <template #default="{ row }">{{ row.menuIds.length }}</template>
          </ElTableColumn>
          <ElTableColumn label="操作" width="120">
            <template #default="{ row }"
              ><RouterLink class="text-primary" :to="roleLink(row.role)"
                >查看详情</RouterLink
              ></template
            >
          </ElTableColumn>
        </ElTable>
        <div class="px-5 py-3 text-sm text-muted-foreground">
          共 {{ state?.roles.length ?? 0 }} 个角色
        </div>
      </template>
      <ElEmpty
        v-else-if="!current"
        :description="loading ? '正在读取角色…' : '未找到该角色'"
      />
      <div v-else class="px-5 pb-5">
        <ElTabs v-model="section" aria-label="角色详情">
          <ElTabPane label="操作权限" name="permissions">
            <p class="mb-4 text-sm text-muted-foreground">
              这是内置角色的操作权限。实际执行还需满足业务条件，例如只有符合条件的任务才能取消。
            </p>
            <PermissionGroups :groups="permissions" />
          </ElTabPane>
          <ElTabPane label="关联账号" name="accounts" lazy>
            <p class="mb-3 text-sm text-muted-foreground">
              关联账号 {{ accountCount(role) }} 个
            </p>
            <ElTable
              style="--el-table-header-bg-color: hsl(var(--accent))"
              :data="members"
              row-key="username"
              :empty-text="
                accountsLoading
                  ? '正在读取账号…'
                  : accountsError
                    ? '账号读取失败'
                    : '暂无关联账号'
              "
            >
              <ElTableColumn prop="username" label="用户名" min-width="140" />
              <ElTableColumn
                prop="displayName"
                label="显示名称"
                min-width="160"
              />
              <ElTableColumn label="状态" width="110">
                <template #default="{ row }"
                  ><ElTag :type="row.disabled ? 'danger' : 'success'">{{
                    row.disabled ? '已禁用' : '正常'
                  }}</ElTag></template
                >
              </ElTableColumn>
              <ElTableColumn
                v-if="auth.sessionUser?.menuIds.includes('users')"
                label="操作"
                width="120"
              >
                <template #default="{ row }"
                  ><RouterLink
                    class="text-primary"
                    :to="{
                      path: '/access/users',
                      query: { username: row.username },
                    }"
                    >查看账号</RouterLink
                  ></template
                >
              </ElTableColumn>
            </ElTable>
          </ElTabPane>
          <ElTabPane label="菜单可见性" name="menus" lazy>
            <p class="mb-4 text-sm text-muted-foreground">
              这些选项控制侧栏显示哪些入口。操作权限由内置角色规则决定。
            </p>
            <ElAlert
              v-if="notice"
              class="mb-4"
              :title="notice"
              type="success"
              :closable="false"
              role="status"
            />
            <ElTree
              ref="tree"
              class="rounded-lg border border-border p-3"
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
                <span class="flex items-center gap-2"
                  >{{ data.title }}
                  <ElTag v-if="data.required" size="small" type="info"
                    >必留入口</ElTag
                  >
                </span>
              </template>
            </ElTree>
            <div class="mt-4 flex flex-wrap items-center justify-between gap-3">
              <span class="text-sm text-muted-foreground"
                >已选
                {{ selectedIds.length }}
                个入口，保存后对该角色的所有账号生效。</span
              >
              <div class="flex gap-2">
                <ElButton :disabled="saving || loading" @click="load"
                  >重新读取</ElButton
                >
                <ElButton
                  data-action="save"
                  type="primary"
                  :loading="saving"
                  :disabled="loading || saving"
                  @click="save"
                  >保存配置</ElButton
                >
              </div>
            </div>
          </ElTabPane>
        </ElTabs>
      </div>
    </section>
  </Page>
</template>
