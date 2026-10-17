import type { Schemas } from '#/api/console/generated';

export function missingChunks(
  total: number,
  ranges: Schemas['SourceRange'][],
  chunkSize = 4 * 1024 * 1024,
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
  return result;
}

// Hash every byte in bounded memory. Persist this identity before uploading so a
// file selected after a page reload cannot silently mix different source bytes.
export async function verifyReselectedFile(
  file: Blob,
  total: number,
  expected?: string,
): Promise<string> {
  if (file.size !== total) throw new Error('所选文件大小与登记来源不一致');
  const digests: Uint8Array[] = [];
  for (let start = 0; start < file.size; start += 4 * 1024 * 1024) {
    const data = await file.slice(start, start + 4 * 1024 * 1024).arrayBuffer();
    digests.push(new Uint8Array(await crypto.subtle.digest('SHA-256', data)));
  }
  const combined = new Uint8Array(digests.length * 32);
  digests.forEach((digest, index) => combined.set(digest, index * 32));
  const identity = [
    ...new Uint8Array(await crypto.subtle.digest('SHA-256', combined)),
  ]
    .map((byte) => byte.toString(16).padStart(2, '0'))
    .join('');
  if (expected && expected !== identity)
    throw new Error('所选文件内容与原上传文件不一致，请重新选择原文件');
  return identity;
}
