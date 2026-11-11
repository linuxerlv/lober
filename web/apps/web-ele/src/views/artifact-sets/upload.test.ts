import { expect, it, vi } from 'vitest';

import { verifyReselectedFile } from './upload';

it('没有浏览器摘要接口时仍能识别之前登记过的多分片文件', async () => {
  const file = new Blob([
    new Uint8Array(4 * 1024 * 1024).fill(0x95),
    new Uint8Array([0x00, 0xff, 0x21]),
  ]);
  // 用 Node 逐片计算 SHA-256，再对拼接后的两段摘要计算 SHA-256。
  const expected =
    '15f8ace467c7cb664b4cb50ba7a3c6edd02a39bf5b50aaa45595da9bd1ea90eb';

  await expect(verifyReselectedFile(file, file.size)).resolves.toBe(expected);
  vi.stubGlobal('crypto', {});
  try {
    await expect(
      verifyReselectedFile(file, file.size, expected),
    ).resolves.toBe(expected);
  } finally {
    vi.unstubAllGlobals();
  }
});
