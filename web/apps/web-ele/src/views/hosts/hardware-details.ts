function formatCapacity(bytes: bigint) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB'];
  let unit = 0;
  let scale = 1n;
  while (bytes >= scale * 1000n && unit < units.length - 1) {
    scale *= 1000n;
    unit++;
  }
  const rounded = (bytes * 100n + scale / 2n) / scale;
  const decimals = String(rounded % 100n)
    .padStart(2, '0')
    .replace(/0+$/, '');
  return `${rounded / 100n}${decimals ? '.' + decimals : ''} ${units[unit]}`;
}

export function diskRows(value?: string) {
  return (value ?? '').split(/\r?\n/).flatMap((line) => {
    const match = /^([^:]+):([0-9]{1,19}):(.*)$/.exec(line.trim());
    if (!match) return [];
    const [, device, bytes, model] = match;
    if (!device || !bytes) return [];
    return [
      {
        device,
        model: model ?? '',
        bytes,
        capacity: formatCapacity(BigInt(bytes)),
      },
    ];
  });
}

export function networkRows(value?: string) {
  if (!value) return [];
  try {
    const items: unknown = JSON.parse(value);
    if (!Array.isArray(items)) return [];
    return items.flatMap((entry: unknown) => {
      if (!entry || typeof entry !== 'object') return [];
      const { device, model, mac } = entry as Record<string, unknown>;
      if (typeof device !== 'string' || !device.trim()) return [];
      return [
        {
          device: device.trim(),
          model: typeof model === 'string' ? model.trim() : '',
          mac: typeof mac === 'string' ? mac.trim() : '',
        },
      ];
    });
  } catch {
    return [];
  }
}
