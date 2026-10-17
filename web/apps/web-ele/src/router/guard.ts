import type { Router } from 'vue-router';
import { useAccessStore, useTabbarStore } from '@vben/stores';
import { generateMenus, startProgress, stopProgress } from '@vben/utils';
import { ElMessage } from 'element-plus';
import { useAuthStore } from '#/store';
import { accessRoutes, routes } from './routes';
import { createConsoleGuard } from './console-access';
import { registerAccessRefresh } from '#/access/refresh';
export function createRouterGuard(router: Router) {
  router.beforeEach(() => {
    startProgress();
  });
  router.afterEach(() => stopProgress());
  router.onError(() => stopProgress());
  // Stores are resolved inside navigation, after Pinia has been installed.
  let controller: ReturnType<typeof createConsoleGuard> | undefined;
  function getController() {
    if (!controller) {
      controller = createConsoleGuard({
        router,
        baseRoutes: routes,
        routes: accessRoutes,
        getToken: () => useAccessStore().accessToken,
        getUser: () => useAuthStore().sessionUser,
        refreshUser: () => useAuthStore().refreshSessionUser(),
        clearCaches: () => useTabbarStore().$reset(),
        onError: () => ElMessage.error('读取权限失败，请检查网络后重试'),
        onRoutes: (allowed) => {
          const access = useAccessStore();
          access.setAccessMenus(generateMenus(allowed, router));
          access.setAccessRoutes(allowed);
          access.setIsAccessChecked(true);
        },
      });
    }
    return controller;
  }
  router.beforeEach((to, from) => getController()(to, from));
  registerAccessRefresh(() => getController().refresh());
}
