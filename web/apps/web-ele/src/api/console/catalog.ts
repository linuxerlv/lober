import type { Schemas } from './generated';

export const seriesByFamily = {
  linux: ['rhel', 'rocky', 'almalinux', 'debian', 'ubuntu'],
  windows: ['windows-server', 'windows-desktop'],
  esxi: ['esxi'],
} as const satisfies Record<Schemas['CatalogFamily'], readonly Schemas['CatalogSeries'][]>;

export const allSeries = Object.values(seriesByFamily).flat();
const selectable = new Set<string>(allSeries);

function isSupportedSeries(value: string): value is Schemas['CatalogSeries'] {
  return selectable.has(value);
}

// 历史值可以显示；保存前须明确移除，不能悄悄丢掉用户的配置。
export function currentSeries(values: null | readonly string[]): null | Schemas['CatalogSeries'][] {
  return values?.map((value) => {
    if (!isSupportedSeries(value)) {
      throw new Error(`当前不支持系统：${value}，旧配置仅供查看，请新建受支持的配置。`);
    }
    return value;
  }) ?? null;
}
