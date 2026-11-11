import type { Schemas } from '#/api/console/generated';

import { sha256 } from '@noble/hashes/sha2.js';
import { bytesToHex } from '@noble/hashes/utils.js';

export function missingChunks(
  total: number,
  ranges: Schemas['SourceRange'][],
  chunkSize = 4 * 1024 * 1024,
  finalizeComplete = false,
) {
  if (total <= 0 || chunkSize <= 0) throw new Error('文件大小无效');
  const result: { start: number; end: number }[] = [];
  let position = 0;
  const append = (end: number) => {
    while (position < end) {
      const next = Math.min(position + chunkSize, end);
      result.push({ start: position, end: next });
      position = next;
    }
  };
  for (const range of [...ranges].sort((a, b) => a.start - b.start)) {
    if (range.start > position) append(Math.min(range.start, total));
    position = Math.min(total, Math.max(position, range.start + range.length));
  }
  append(total);
  // 字节已齐但还没完成校验时，只重放原末块，让服务器继续完整校验。
  if (finalizeComplete && result.length === 0) {
    result.push({
      start: Math.floor((total - 1) / chunkSize) * chunkSize,
      end: total,
    });
  }
  return result;
}

async function digestSHA256(data: Uint8Array<ArrayBuffer>) {
  const subtle = globalThis.crypto?.subtle;
  return subtle
    ? new Uint8Array(await subtle.digest('SHA-256', data))
    : sha256(data);
}

// 逐片计算摘要，再对拼接后的摘要计算 SHA-256，以兼容浏览器中已有的记录。
export async function verifyReselectedFile(
  file: Blob,
  total: number,
  expected?: string,
): Promise<string> {
  if (file.size !== total) throw new Error('所选文件大小与登记来源不一致');
  const digests: Uint8Array[] = [];
  for (let start = 0; start < file.size; start += 4 * 1024 * 1024) {
    const data = await file.slice(start, start + 4 * 1024 * 1024).arrayBuffer();
    digests.push(await digestSHA256(new Uint8Array(data)));
  }
  const combined = new Uint8Array(digests.length * 32);
  digests.forEach((digest, index) => combined.set(digest, index * 32));
  const identity = bytesToHex(await digestSHA256(combined));
  if (expected && expected !== identity)
    throw new Error('所选文件内容与原上传文件不一致，请重新选择原文件');
  return identity;
}
