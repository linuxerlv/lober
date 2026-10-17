import type { CSVPlan } from '#/api/console/host';

export interface AttributeRow {
  key: string;
  value: string;
}
export function attributesOf(rows: AttributeRow[]): Record<string, string> {
  const result: Record<string, string> = Object.create(null);
  for (const row of rows) {
    const key = row.key.trim();
    if (!key) throw new Error('属性名不能为空');
    if (Object.hasOwn(result, key)) throw new Error(`属性名重复：${key}`);
    result[key] = row.value;
  }
  return result;
}

export class CsvImportSession {
  bytes: Uint8Array | undefined;
  plan: CSVPlan | undefined;
  version = 0;
  async commit<T>(run: (bytes: Uint8Array) => Promise<T>, confirmed: boolean) {
    if (!this.plan || !this.bytes) throw new Error('请先预览当前 CSV');
    if (!confirmed) throw new Error('请明确确认导入');
    if ((this.plan.rows ?? []).some((row) => row.action === 'error'))
      throw new Error('请修正所有错误后重新预览');
    const result = await run(this.bytes);
    this.plan = undefined;
    return result;
  }
  async preview(run: (bytes: Uint8Array) => Promise<CSVPlan>) {
    if (!this.bytes?.length) throw new Error('请选择 CSV 文件或输入 CSV 文本');
    const version = this.version;
    const plan = await run(this.bytes);
    if (version === this.version) this.plan = plan;
  }
  select(bytes: Uint8Array) {
    this.version++;
    this.bytes = bytes.slice();
    this.plan = undefined;
  }
}

export class CursorPage<T> {
  cursor = '';
  generation = 0;
  history: string[] = [];
  items: T[] = [];
  next = '';
  advance() {
    if (!this.next) return;
    this.generation++;
    this.history.push(this.cursor);
    this.cursor = this.next;
    this.next = '';
  }
  back() {
    if (!this.history.length) return;
    this.generation++;
    this.cursor = this.history.pop() ?? '';
    this.next = '';
  }
  async load(
    run: (cursor: string) => Promise<{ items: null | T[]; next: string }>,
  ) {
    const generation = ++this.generation;
    try {
      const result = await run(this.cursor);
      if (generation === this.generation) {
        this.items = result.items ?? [];
        this.next = result.next;
      }
    } catch (error) {
      if (generation === this.generation) throw error;
    }
    return { items: this.items };
  }
  reset() {
    this.generation++;
    this.cursor = '';
    this.next = '';
    this.history = [];
    this.items = [];
  }
}
