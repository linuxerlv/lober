import { expect, it } from 'vitest';

import { selectionCSV } from './selection-export';

it('quotes commas and quotes and keeps spreadsheet formulas inert', () => {
  expect(
    selectionCSV([
      ['名称', '属性'],
      ['机柜, A', 'a"b'],
      ['=1+1', '\t@cmd'],
    ]),
  ).toBe('\uFEFF"名称","属性"\r\n"机柜, A","a""b"\r\n"\'=1+1","\'\t@cmd"');
});
