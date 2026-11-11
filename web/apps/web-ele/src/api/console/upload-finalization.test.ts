import type { Socket } from 'node:net';

import { Buffer, Blob as NodeBlob } from 'node:buffer';
import { createServer } from 'node:http';

import { expect, it, vi } from 'vitest';

// 使用共享客户端和它已有的 Axios 依赖；只有应用装配层被替换。
// 显式选择 Node 传输，避免 happy-dom 的浏览器传输掩盖真实超时。
vi.mock('#/api/request', async () => {
  const { createRequire } = await import('node:module');
  const { RequestClient } = await import('@vben/request');
  const fromApp = createRequire(import.meta.url);
  const fromRequest = createRequire(fromApp.resolve('@vben/request'));
  const axios = fromRequest('axios');
  return {
    requestClient: new RequestClient({
      adapter: axios.getAdapter('http'),
      proxy: false,
    }),
  };
});

import { requestClient } from '#/api/request';

import { getISOProgressApi, uploadISOChunkApi } from './artifact';

it('waits for a real 12-second final PUT while ordinary requests retain their 10-second limit', async () => {
  const sockets = new Set<Socket>();
  const timers = new Set<ReturnType<typeof setTimeout>>();
  const received: { body: Buffer; range: string | undefined; url: string | undefined }[] = [];
  const timeouts: { method: string | undefined; timeout: number | undefined }[] = [];
  const interceptor = requestClient.instance.interceptors.request.use((config) => {
    timeouts.push({ method: config.method, timeout: config.timeout });
    return config;
  });
  const server = createServer((request, response) => {
    const chunks: Buffer[] = [];
    request.on('data', (bytes: Buffer) => chunks.push(bytes));
    request.on('end', () => {
      received.push({
        body: Buffer.concat(chunks),
        range: request.headers['content-range'],
        url: request.url,
      });
      const timer = setTimeout(() => {
        timers.delete(timer);
        if (!response.destroyed) {
          response.writeHead(200, { 'Content-Type': 'application/json' });
          response.end('null');
        }
      }, 12_000);
      timers.add(timer);
    });
  });
  server.on('connection', (socket) => {
    sockets.add(socket);
    socket.on('close', () => sockets.delete(socket));
  });

  try {
    await new Promise<void>((resolve, reject) => {
      server.once('error', reject);
      server.listen(0, '127.0.0.1', resolve);
    });
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('测试服务未监听端口');
    requestClient.instance.defaults.baseURL = `http://127.0.0.1:${address.port}`;
    const bytes = new NodeBlob([new Uint8Array([7, 8, 9])]);
    const [upload, ordinary] = await Promise.allSettled([
      uploadISOChunkApi('id?x', 'netboot', bytes as Blob, 6, 9),
      getISOProgressApi('id?x'),
    ]);

    // 服务确实收到了原末片，不能把适配器或 Blob 错误算作预期失败。
    expect(received).toContainEqual({
      body: Buffer.from([7, 8, 9]),
      range: 'bytes 6-8/9',
      url: '/isos/id%3Fx/content?role=netboot',
    });
    expect.soft(upload.status).toBe('fulfilled');
    if (upload.status === 'rejected') {
      expect(upload.reason).toMatchObject({ code: 'ECONNABORTED' });
      expect(upload.reason.message).toContain('10000');
    }
    expect(ordinary.status).toBe('rejected');
    if (ordinary.status === 'rejected') {
      expect(ordinary.reason).toMatchObject({ code: 'ECONNABORTED' });
      expect(ordinary.reason.message).toContain('10000');
    }
    expect(timeouts.find((item) => item.method === 'get')?.timeout).toBe(10_000);
    const uploadTimeout = timeouts.find((item) => item.method === 'put')?.timeout;
    expect.soft(uploadTimeout).toBeGreaterThan(12_000);
    expect(uploadTimeout).toBeLessThanOrEqual(15 * 60_000);
    expect(requestClient.instance.defaults.timeout).toBe(10_000);
  } finally {
    requestClient.instance.interceptors.request.eject(interceptor);
    timers.forEach((timer) => clearTimeout(timer));
    sockets.forEach((socket) => socket.destroy());
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
}, 30_000);
