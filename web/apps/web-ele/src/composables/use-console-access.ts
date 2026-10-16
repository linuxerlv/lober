import { onMounted, onUnmounted } from 'vue';
import { useAccessStore } from '@vben/stores';
import { useAuthStore } from '#/store';
import { canOperate, consoleMenus } from '#/access/policy';
import { refreshAccess } from '#/access/refresh';
export function useConsoleAccess() {
  const auth = useAuthStore(),
    access = useAccessStore();
  return {
    can: (permission: string) =>
      !!access.accessToken && canOperate(auth.sessionUser, permission),
    hasMenu: (id: string) =>
      !!access.accessToken &&
      !!auth.sessionUser?.menuIds.includes(id) &&
      !!consoleMenus[id] &&
      canOperate(auth.sessionUser, consoleMenus[id]!.permission),
    refresh: refreshAccess,
    run: async <T>(
      permission: string,
      action: () => Promise<T>,
    ): Promise<T | undefined> => {
      if (!access.accessToken || !canOperate(auth.sessionUser, permission))
        return undefined;
      return action();
    },
  };
}
// Called once by App, including the empty-menu and password pages.
export function useConsoleSessionRefresh() {
  const access = useAccessStore();
  let pending = false;
  let timer: ReturnType<typeof setInterval> | undefined;
  async function refresh() {
    if (pending || !access.accessToken) return;
    pending = true;
    try {
      await refreshAccess();
    } finally {
      pending = false;
    }
  }
  onMounted(() => {
    window.addEventListener('focus', refresh);
    timer = setInterval(refresh, 30_000);
  });
  onUnmounted(() => {
    window.removeEventListener('focus', refresh);
    if (timer) clearInterval(timer);
  });
}
