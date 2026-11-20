<script setup lang="ts">
import type { User } from '#/api/console/accounts';
import type { Schemas } from '#/api/console/generated';
import type { RoleMenuList } from '#/api/console/role-menus';

import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import { Page } from '@vben/common-ui';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElDialog,
  ElForm,
  ElFormItem,
  ElInput,
  ElOption,
  ElSelect,
  ElSwitch,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import { consoleMenus } from '#/access/policy';
import { accountsApi, roleLabels } from '#/api/console/accounts';
import { getRoleMenusApi } from '#/api/console/role-menus';
import ChangeRecords from '#/components/console/ChangeRecords.vue';
import ListPagination from '#/components/console/ListPagination.vue';
import { NumberedPage } from '#/composables/numbered-page';
import { useConsoleAccess } from '#/composables/use-console-access';
import { useAuthStore } from '#/store';

import { readAccountDirectory } from './account-directory';
import PermissionGroups from './PermissionGroups.vue';
import { accountPermissionGroups, roleDescriptions } from './role-permissions';
const route = useRoute();
const router = useRouter();
const auth = useAuthStore();
const { can, refresh } = useConsoleAccess();
const allowed = computed(
  () => auth.sessionUser?.role === 'admin' && can('user:manage'),
);
const page = reactive(new NumberedPage<User>());
const editing = ref(false);
const error = ref('');
const items = computed(() => page.items);
const loading = computed(() => page.loading);
const notice = ref('');
const saving = ref(false);
const form = reactive({
  username: '',
  displayName: '',
  role: 'viewer' as Schemas['StoreRole'],
  password: '',
  disabled: false,
  rev: 0,
});
const detailOpen = ref(false);
const detail = ref<User>();
const detailError = ref('');
const detailLoading = ref(false);
const detailMenus = ref<RoleMenuList>();
const menusError = ref('');
const restricted = computed(
  () => detail.value?.disabled || detail.value?.passwordChangeRequired,
);
const operations = computed(() =>
  accountPermissionGroups(detail.value?.permissions ?? []),
);
const visibleMenus = computed(() => {
  const user = detail.value;
  const config = detailMenus.value;
  if (!user || !config || restricted.value) return [];
  const ids = new Set(
    config.roles.find((role) => role.role === user.role)?.menuIds,
  );
  const permissions = new Set(user.permissions ?? []);
  return config.menus
    .filter(
      (menu) =>
        menu.parentId &&
        Object.hasOwn(consoleMenus, menu.id) &&
        ids.has(menu.id) &&
        permissions.has(menu.permission),
    )
    .map((menu) => ({
      ...menu,
      title:
        menu.id === 'role-menus'
          ? '角色管理'
          : menu.id === 'isos'
            ? '系统镜像'
            : menu.title,
      group:
        menu.id === 'isos'
          ? '制品管理'
          : config.menus.find((group) => group.id === menu.parentId)?.title,
    }));
});
const canOpenRoles = computed(() =>
  auth.sessionUser?.menuIds.includes('role-menus'),
);
function roleLink(role: Schemas['StoreRole']) {
  return { path: '/access/role-menus', query: { role } };
}
watch(
  () => (route.path === '/access/users' ? route.query.username : undefined),
  async (username, _, onCleanup) => {
    if (!allowed.value || typeof username !== 'string' || !username) {
      detailOpen.value = false;
      return;
    }
    let stale = false;
    onCleanup(() => {
      stale = true;
    });
    detailOpen.value = true;
    detail.value = undefined;
    detailMenus.value = undefined;
    menusError.value = '';
    detailError.value = '';
    detailLoading.value = true;
    try {
      const [account, menus] = await Promise.allSettled([
        readAccountDirectory(username),
        getRoleMenusApi(),
      ]);
      if (stale) return;
      if (account.status === 'rejected') throw account.reason;
      detail.value = account.value.find((row) => row.username === username);
      if (
        menus.status === 'fulfilled' &&
        menus.value.roles.some((role) => role.role === detail.value?.role)
      ) {
        detailMenus.value = menus.value;
      } else {
        menusError.value = '无法读取该账号的菜单配置。';
      }
      if (!detail.value) detailError.value = '未找到该账号';
    } catch {
      if (!stale) detailError.value = '读取账号失败';
    } finally {
      if (!stale) detailLoading.value = false;
    }
  },
  { immediate: true },
);
function closeDetails() {
  if (route.path === '/access/users' && route.query.username) {
    const { username: _username, ...query } = route.query;
    void router.replace({ path: route.path, query });
  }
}
async function load() {
  if (!allowed.value) return;
  error.value = '';
  try {
    await page.load((pagination) => accountsApi.list(pagination));
  } catch (e: any) {
    error.value = e?.response?.data?.message || e.message || '读取账号失败';
  }
}
function changePage(value: number) {
  page.setPage(value);
  void load();
}
function changePageSize(value: number) {
  page.setPageSize(value);
  void load();
}
function edit(user?: User) {
  Object.assign(
    form,
    user
      ? {
          username: user.username,
          displayName: user.displayName,
          role: user.role,
          password: '',
          disabled: user.disabled,
          rev: user.rev,
        }
      : {
          username: '',
          displayName: '',
          role: 'viewer',
          password: '',
          disabled: false,
          rev: 0,
        },
  );
  editing.value = true;
  error.value = '';
  notice.value = '';
}
function closeEditor() {
  if (saving.value) return;
  editing.value = false;
  form.password = '';
  error.value = '';
}
function beforeCloseEditor(done: () => void) {
  if (saving.value) return;
  closeEditor();
  done();
}
async function save() {
  if (!allowed.value || saving.value) return;
  saving.value = true;
  error.value = '';
  notice.value = '';
  try {
    const isNew = !form.rev;
    if (isNew)
      await accountsApi.create({
        username: form.username,
        displayName: form.displayName,
        role: form.role,
        password: form.password,
        disabled: false,
      });
    else
      await accountsApi.update(form.username, {
        displayName: form.displayName,
        role: form.role,
        disabled: form.disabled,
        rev: form.rev,
        ...(form.password ? { password: form.password } : {}),
      });
    const self = form.username === auth.sessionUser?.username;
    form.password = '';
    editing.value = false;
    notice.value = isNew ? '账号已创建' : '账号已更新';
    if (self) await refresh();
    await load();
  } catch (e: any) {
    error.value =
      e?.response?.status === 409
        ? '账号已变更或操作不允许，请重新读取核对；当前编辑已保留'
        : e?.response?.data?.message || e.message || '保存失败';
  } finally {
    saving.value = false;
  }
}
onMounted(() => load());
</script>
<template>
  <Page title="账号管理" description="查看账号、分配角色和管理账号状态。">
    <ElCard v-if="!allowed"><p>仅管理员可以管理账号及分配角色。</p></ElCard>
    <section
      v-else
      class="overflow-hidden rounded-lg border border-border bg-card"
    >
      <ElAlert
        v-if="error && !editing"
        :title="error"
        type="error"
        :closable="false"
        role="alert"
      /><ElAlert
        v-if="notice"
        :title="notice"
        type="success"
        :closable="false"
        role="status"
      />
      <div class="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
        <h2 class="font-semibold">账号列表</h2>
        <div class="ml-auto flex flex-wrap items-center justify-end gap-2">
          <ElButton type="primary" @click="edit()" :disabled="saving">
            创建账号
          </ElButton>
          <ElButton @click="load()" :disabled="loading || saving">
            刷新列表
          </ElButton>
        </div>
      </div>
      <ElTable
        style="--el-table-header-bg-color: hsl(var(--accent))"
        :data="items"
        row-key="username"
        :empty-text="loading ? '正在加载…' : '暂无账号'"
      >
        <ElTableColumn label="用户名" min-width="160">
          <template #default="{ row }">
<RouterLink
              class="text-primary"
              :to="{ path: '/access/users', query: { username: row.username } }"
              >
{{ row.username }}
</RouterLink>
</template>
        </ElTableColumn>
        <ElTableColumn prop="displayName" label="显示名称" min-width="160">
          <template #default="{ row }">{{ row.displayName || '—' }}</template>
        </ElTableColumn>
        <ElTableColumn label="角色" min-width="180">
          <template #default="{ row }">
            <RouterLink
              v-if="canOpenRoles"
              class="text-primary"
              :to="roleLink(row.role)"
            >
              {{ roleLabels[row.role as Schemas['StoreRole']] }}
            </RouterLink>
            <ElTag v-else>
{{
              roleLabels[row.role as Schemas['StoreRole']]
            }}
</ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="状态" width="110">
          <template #default="{ row }">
            <ElTag :type="row.disabled ? 'danger' : 'success'">
              {{ row.disabled ? '已禁用' : '正常' }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="操作" width="100">
          <template #default="{ row }">
            <ElButton @click="edit(row as User)" :disabled="saving">
              编辑
            </ElButton>
          </template>
        </ElTableColumn>
      </ElTable>
      <div class="px-4 pb-3">
        <ListPagination
          :current-page="page.page"
          :page-size="page.pageSize"
          :total="page.total"
          :page-count="items.length"
          :disabled="loading || saving"
          @page-change="changePage"
          @size-change="changePageSize"
        />
      </div>
      <ElDialog
        v-if="detailOpen"
        v-model="detailOpen"
        title="账号详情"
        width="min(1080px, 94vw)"
        append-to-body
        @close="closeDetails"
      >
        <ElAlert
          v-if="detailError"
          type="error"
          :title="detailError"
          :closable="false"
        />
        <p v-else-if="detailLoading">正在读取账号…</p>
        <div
          v-else-if="detail"
          class="grid max-h-[68dvh] gap-5 overflow-auto p-1"
        >
          <dl
            class="grid grid-cols-2 gap-4 rounded-lg border border-border bg-card p-4 md:grid-cols-4"
          >
            <div>
              <dt class="text-muted-foreground">用户名</dt>
              <dd>{{ detail.username }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">显示名称</dt>
              <dd>{{ detail.displayName || '—' }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">角色</dt>
              <dd>
                <RouterLink
                  v-if="canOpenRoles"
                  class="text-primary"
                  :to="roleLink(detail.role)"
                  >
{{ roleLabels[detail.role] }}
</RouterLink>
                <template v-else>{{ roleLabels[detail.role] }}</template>
              </dd>
            </div>
            <div>
              <dt class="text-muted-foreground">状态</dt>
              <dd>{{ detail.disabled ? '已禁用' : '正常' }}</dd>
            </div>
          </dl>
          <ElAlert
            v-if="restricted"
            type="warning"
            :closable="false"
            :title="
              detail.disabled
                ? '账号已禁用，无法使用控制台。'
                : '该账号需要先修改密码，才能使用业务功能。'
            "
          />
          <section aria-label="实际操作权限">
            <h2 class="mb-2 font-semibold">实际操作权限</h2>
            <p v-if="restricted" class="text-sm text-muted-foreground">
              当前没有可用的业务操作。
            </p>
            <p
              v-else-if="!detail.permissions?.length"
              class="text-sm text-muted-foreground"
            >
              当前没有可用的业务操作。
            </p>
            <PermissionGroups v-else :groups="operations" />
          </section>
          <section
            aria-label="可见菜单"
            class="rounded-lg border border-border p-4"
          >
            <h2 class="mb-2 font-semibold">可见菜单</h2>
            <ElAlert
              v-if="menusError && !restricted"
              type="warning"
              :title="menusError"
              :closable="false"
            />
            <ul v-else-if="visibleMenus.length" class="flex flex-wrap gap-2">
              <li v-for="menu in visibleMenus" :key="menu.id">
                <ElTag type="info">
{{ menu.group ? `${menu.group } / ` : ''
                  }}{{ menu.title }}
</ElTag>
              </li>
            </ul>
            <p v-else class="text-sm text-muted-foreground">
              当前没有可见菜单。
            </p>
          </section>
          <ChangeRecords
            v-if="can('audit:read')"
            class="rounded-lg border border-border p-4"
            target-kind="user"
            :target-id="detail.username"
          />
        </div>
        <template #footer>
          <ElButton
            v-if="detail"
            type="primary"
            @click="
              detailOpen = false;
              edit(detail);
            "
            >
编辑账号
</ElButton>
        </template>
      </ElDialog>
      <ElDialog
        v-model="editing"
        :title="form.rev ? '编辑账号' : '创建账号'"
        width="min(600px, calc(100vw - 32px))"
        top="8vh"
        append-to-body
        destroy-on-close
        :close-on-click-modal="false"
        :close-on-press-escape="!saving"
        :show-close="!saving"
        :before-close="beforeCloseEditor"
      >
        <ElAlert
          v-if="error"
          :title="error"
          type="error"
          :closable="false"
          role="alert"
          class="mb-4"
        />
        <ElForm
          id="account-editor-form"
          :model="form"
          :disabled="saving"
          label-position="top"
          @submit.prevent="save"
          class="account-form"
        >
          <ElFormItem label="用户名">
            <ElInput
              name="username"
              v-model="form.username"
              :readonly="!!form.rev"
              :disabled="saving"
              required
              pattern="[A-Za-z0-9_]+"
              maxlength="64"
            />
          </ElFormItem>
          <ElFormItem label="显示名称">
            <ElInput
              name="displayName"
              v-model="form.displayName"
              maxlength="100"
              :disabled="saving"
            />
          </ElFormItem>
          <ElFormItem label="角色">
            <ElSelect name="role" aria-label="角色" v-model="form.role">
              <ElOption
                v-for="(label, key) in roleLabels"
                :key="key"
                :value="key"
                :label="label"
              />
            </ElSelect>
          </ElFormItem>
          <p class="muted">{{ roleDescriptions[form.role] }}</p>
          <ElFormItem
            :label="form.rev ? '重置密码（留空保留原密码）' : '初始密码'"
          >
            <ElInput
              name="password"
              v-model="form.password"
              type="password"
              autocomplete="new-password"
              :required="!form.rev"
              :disabled="saving"
            />
          </ElFormItem>
          <ElFormItem v-if="form.rev" label="账号状态">
            <ElSwitch
              name="disabled"
              v-model="form.disabled"
              :disabled="saving || form.username === auth.sessionUser?.username"
              active-text="禁用账号（使已有登录失效）"
            />
          </ElFormItem>
          <p class="muted">
            不能禁用本人，也不能停用或降级最后一个管理员。重置密码后该账号的已有登录全部失效。
          </p>
        </ElForm>
        <template #footer>
          <ElButton @click="closeEditor" :disabled="saving">取消</ElButton>
          <ElButton
            native-type="submit"
            form="account-editor-form"
            type="primary"
            :loading="saving"
            :disabled="saving"
          >
            保存账号
          </ElButton>
        </template>
      </ElDialog>
    </section>
  </Page>
</template>
<style scoped>
.muted {
  color: var(--el-text-color-secondary);
  margin: 10px 0;
}
.account-form {
  width: 100%;
}
</style>
