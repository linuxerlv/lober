import { shallowRef, watch } from 'vue';

import { useAccessStore } from '@vben/stores';

// 切换账号或收回权限时清除旧数据，并忽略尚未返回的旧请求。
export function useOverviewData<T>(
  allowed: () => boolean,
  read: () => Promise<T>,
) {
  const access = useAccessStore();
  const result = shallowRef<T>();
  watch(
    [() => access.accessToken, allowed],
    async ([, enabled], _, onCleanup) => {
      let current = true;
      onCleanup(() => {
        current = false;
      });
      result.value = undefined;
      if (!enabled) return;
      try {
        const value = await read();
        if (current) result.value = value;
      } catch {
        // 请求层会提示错误，这一块保留空状态，其他块仍可使用。
      }
    },
    { immediate: true },
  );
  return result;
}
