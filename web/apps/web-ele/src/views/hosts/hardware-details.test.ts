import { expect, it } from 'vitest';

import { diskRows, networkRows } from './hardware-details';

it('reads the collector format without rounding large byte counts', () => {
  expect(
    diskRows(
      'sda:480000000000:Intel SSD\r\nnvme0n1:9007199254740993:\ninvalid',
    ),
  ).toEqual([
    {
      device: 'sda',
      model: 'Intel SSD',
      capacity: '480 GB',
      bytes: '480000000000',
    },
    {
      device: 'nvme0n1',
      model: '',
      capacity: '9.01 PB',
      bytes: '9007199254740993',
    },
  ]);
  expect(diskRows(undefined)).toEqual([]);
});

it('shows recorded network interfaces and leaves missing data empty', () => {
  expect(
    networkRows(
      '[{"device":"eno1","model":"Intel I350","mac":"02:00:00:00:00:01"},{"device":"eno2"},{"model":"invalid"}]',
    ),
  ).toEqual([
    { device: 'eno1', model: 'Intel I350', mac: '02:00:00:00:00:01' },
    { device: 'eno2', model: '', mac: '' },
  ]);
  expect(networkRows('bad json')).toEqual([]);
  expect(networkRows(undefined)).toEqual([]);
});
