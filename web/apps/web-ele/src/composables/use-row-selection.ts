import { computed, reactive } from 'vue';

export function useRowSelection<T>(keyOf: (row: T) => string, page: () => T[]) {
  const selected = reactive(new Map<string, T>());
  const rows = computed(() => [...selected.values()] as T[]);
  const count = computed(
    () => page().filter((row) => selected.has(keyOf(row))).length,
  );
  function toggle(row: T, checked: unknown) {
    const key = keyOf(row);
    if (checked === true) (selected as Map<string, T>).set(key, row);
    else selected.delete(key);
  }
  return {
    rows,
    has: (row: T) => selected.has(keyOf(row)),
    all: computed(() => page().length > 0 && count.value === page().length),
    partial: computed(() => count.value > 0 && count.value < page().length),
    toggle,
    togglePage: (checked: unknown) =>
      page().forEach((row) => toggle(row, checked)),
    clear: () => selected.clear(),
    refresh: (items: T[]) =>
      items.forEach((row) => {
        if (selected.has(keyOf(row))) toggle(row, true);
      }),
  };
}
