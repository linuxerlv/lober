import { fileURLToPath } from 'node:url';
import { mergeConfig, defineConfig } from 'vitest/config';
import shared from '../../vitest.config';

export default mergeConfig(shared, defineConfig({
  resolve: { alias: { '#': fileURLToPath(new URL('./src', import.meta.url)) } },
  test: { include: ['apps/web-ele/src/**/*.test.ts'] },
}));
