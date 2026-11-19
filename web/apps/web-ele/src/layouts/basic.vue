<script lang="ts" setup>
import type { NotificationItem } from '@vben/layouts';

import { computed, ref, watch } from 'vue';
import { useRouter } from 'vue-router';

import { AuthenticationLoginExpiredModal } from '@vben/common-ui';
import { useWatermark } from '@vben/hooks';
import { BookOpenText } from '@vben/icons';
import {
  BasicLayout,
  LockScreen,
  Notification,
  UserDropdown,
} from '@vben/layouts';
import { preferences, usePreferences } from '@vben/preferences';
import { useAccessStore, useUserStore } from '@vben/stores';

import { ElMessage, ElOption, ElSelect, ElTag } from 'element-plus';

import { roleLabels } from '#/api/console/accounts';
import { requestClient } from '#/api/request';
import { useConsoleAccess } from '#/composables/use-console-access';
import { useAuthStore } from '#/store';
import LoginForm from '#/views/_core/authentication/login.vue';

const notifications = ref<NotificationItem[]>([]);

const router = useRouter();
const userStore = useUserStore();
const authStore = useAuthStore();
const accessStore = useAccessStore();
const consoleAccess = useConsoleAccess();
const isMockMode = import.meta.env.VITE_USE_MOCK === 'true';

async function switchMockRole(role: string) {
  try {
    const result = await requestClient.post<{
      token: string;
      user: NonNullable<typeof authStore.sessionUser>;
    }>('/session/switch-role', { role });
    accessStore.setAccessToken(result.token);
    authStore.applySessionUser(result.user);
    await consoleAccess.refresh();
    ElMessage.success(`已切换演示视角：${roleLabels[result.user.role]}`);
  } catch {
    ElMessage.error('切换演示角色失败');
  }
}
const { destroyWatermark, updateWatermark } = useWatermark();
const { isDark } = usePreferences();
const showDot = computed(() =>
  notifications.value.some((item) => !item.isRead),
);

const menus = computed(() => [
  {
    handler: () => router.push('/account/profile'),
    icon: BookOpenText,
    text: '个人设置',
  },
  {
    handler: () => router.push('/account/password'),
    icon: BookOpenText,
    text: '修改密码',
  },
  {
    handler: handleLogout,
    icon: BookOpenText,
    text: '退出登录',
  },
]);

const avatar = computed(() => {
  return userStore.userInfo?.avatar ?? preferences.app.defaultAvatar;
});

async function handleLogout() {
  try {
    await authStore.logout(false);
  } catch {
    ElMessage.error('退出失败，请检查网络后重试');
  }
}

function handleNoticeClear() {
  notifications.value = [];
}

function markRead(id: number | string) {
  const item = notifications.value.find((item) => item.id === id);
  if (item) {
    item.isRead = true;
  }
}

function remove(id: number | string) {
  notifications.value = notifications.value.filter((item) => item.id !== id);
}

function handleMakeAll() {
  notifications.value.forEach((item) => (item.isRead = true));
}

const viewAll = () => {};

const handleClick = (item: NotificationItem) => {
  // 如果通知项有链接，点击时跳转
  if (item.link) {
    navigateTo(item.link, item.query, item.state);
  }
};

function navigateTo(
  link: string,
  query?: Record<string, any>,
  state?: Record<string, any>,
) {
  if (link.startsWith('http://') || link.startsWith('https://')) {
    // 外部链接，在新标签页打开
    window.open(link, '_blank');
  } else {
    // 内部路由链接，支持 query 参数和 state
    router.push({
      path: link,
      query: query || {},
      state,
    });
  }
}

watch(
  () => ({
    enable: preferences.app.watermark,
    content: preferences.app.watermarkContent,
    isDark: isDark.value,
    username: userStore.userInfo?.username,
  }),
  async ({ enable, content, isDark: isDarkValue, username }) => {
    if (enable) {
      const watermarkColor = isDarkValue
        ? 'rgba(255, 255, 255, 0.12)'
        : 'rgba(0, 0, 0, 0.12)';

      await updateWatermark({
        advancedStyle: {
          colorStops: [
            {
              color: watermarkColor,
              offset: 0,
            },
            {
              color: watermarkColor,
              offset: 1,
            },
          ],
          type: 'linear',
        },
        content: content || username || '',
      });
    } else {
      destroyWatermark();
    }
  },
  {
    immediate: true,
  },
);
</script>

<template>
  <BasicLayout
    :avatar
    :text="userStore.userInfo?.realName"
    @clear-preferences-and-logout="handleLogout"
    @logout="handleLogout"
  >
    <template #user-dropdown>
      <div v-if="isMockMode && authStore.sessionUser" class="mr-2 hidden items-center gap-2 sm:flex">
        <ElTag size="small" type="warning" effect="plain">Mock 演示</ElTag>
        <ElSelect
          :model-value="authStore.sessionUser.role"
          size="small"
          class="!w-36"
          aria-label="切换演示角色"
          @update:model-value="switchMockRole(String($event))"
        >
          <ElOption
            v-for="(label, roleKey) in roleLabels"
            :key="roleKey"
            :label="label"
            :value="roleKey"
          />
        </ElSelect>
      </div>
      <UserDropdown
        :avatar
        :menus
        :text="userStore.userInfo?.realName"
        :description="`用户名：${userStore.userInfo?.username || ''}`"
        @clear-preferences-and-logout="handleLogout"
        @logout="handleLogout"
      >
        <template #tagText>
          <ElTag
            v-if="authStore.sessionUser"
            class="ml-2 shrink-0"
            size="small"
            type="success"
            :aria-label="`角色：${roleLabels[authStore.sessionUser.role]}`"
            :title="`角色：${roleLabels[authStore.sessionUser.role]}`"
          >
            {{ roleLabels[authStore.sessionUser.role] }}
          </ElTag>
        </template>
      </UserDropdown>
    </template>
    <template #notification>
      <Notification
        :dot="showDot"
        :notifications="notifications"
        @clear="handleNoticeClear"
        @read="(item) => item.id && markRead(item.id)"
        @remove="(item) => item.id && remove(item.id)"
        @make-all="handleMakeAll"
        @on-click="handleClick"
        @view-all="viewAll"
      />
    </template>
    <template #extra>
      <AuthenticationLoginExpiredModal
        v-model:open="accessStore.loginExpired"
        :avatar
      >
        <LoginForm />
      </AuthenticationLoginExpiredModal>
    </template>
    <template #lock-screen>
      <LockScreen :avatar @to-login="handleLogout" />
    </template>
  </BasicLayout>
</template>
