export interface NumberedPageParams {
  page: number;
  page_size: number;
}

export interface NumberedPageResult<T> {
  items: null | T[];
  next?: string;
  total?: null | number;
}

/** Shared server pagination. Wrap in reactive() when consumed by a view. */
export class NumberedPage<T> {
  items: T[] = [];
  loading = false;
  page = 1;
  pageSize = 20;
  total = 0;
  private generation = 0;
  private successfulPage = 1;
  private successfulPageSize = 20;

  async load(
    run: (params: NumberedPageParams) => Promise<NumberedPageResult<T>>,
  ) {
    const generation = ++this.generation;
    let page = this.page;
    const pageSize = this.pageSize;
    this.loading = true;
    const snapshot = () => ({ items: this.items, total: this.total });
    try {
      let result = await run({ page, page_size: pageSize });
      if (generation !== this.generation) return snapshot();
      this.validateTotal(result.total);
      const lastPage = Math.max(1, Math.ceil(result.total! / pageSize));
      if (page > lastPage) {
        page = lastPage;
        result = await run({ page, page_size: pageSize });
        if (generation !== this.generation) return snapshot();
        this.validateTotal(result.total);
      }
      this.page = page;
      this.items = result.items ?? [];
      this.total = result.total!;
      this.successfulPage = page;
      this.successfulPageSize = pageSize;
      return snapshot();
    } catch (error) {
      if (generation !== this.generation) return snapshot();
      this.page = this.successfulPage;
      this.pageSize = this.successfulPageSize;
      throw error;
    } finally {
      if (generation === this.generation) this.loading = false;
    }
  }

  reset() {
    this.generation++;
    this.page = 1;
    this.loading = false;
  }

  setPage(page: number) {
    if (!Number.isSafeInteger(page) || page < 1)
      throw new RangeError('Invalid page');
    this.generation++;
    this.page = page;
    this.loading = false;
  }

  setPageSize(size: number) {
    if (!Number.isSafeInteger(size) || size < 1 || size > 500)
      throw new RangeError('Invalid page size');
    this.pageSize = size;
    this.reset();
  }

  private validateTotal(total: null | number | undefined) {
    if (!Number.isSafeInteger(total) || total! < 0)
      throw new Error('Numbered pagination requires a valid total');
  }
}
