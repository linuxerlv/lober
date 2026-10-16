import { onMounted, ref, shallowRef } from 'vue';
import type { Page } from '#/api/console/operations';
export function errorMessage(error: any) {
  return error?.response?.data?.message || error?.message || '操作失败，请重试';
}
export function useConsoleList<T>(fetch: (cursor: string) => Promise<Page<T>>) {
  const items = shallowRef<T[]>([]),
    next = ref(''),
    loading = ref(false),
    error = ref('');
  async function load(append = false) {
    if (loading.value) return;
    loading.value = true;
    error.value = '';
    try {
      const result = await fetch(append ? next.value : '');
      items.value = append ? [...items.value, ...result.items] : result.items;
      next.value = result.next || '';
    } catch (e) {
      error.value = errorMessage(e);
    } finally {
      loading.value = false;
    }
  }
  onMounted(() => load());
  return { items, next, loading, error, load };
}
