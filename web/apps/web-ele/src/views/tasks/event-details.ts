function logText(encoded: string): string {
  try {
    const bytes = Uint8Array.from(atob(encoded), (char) => char.charCodeAt(0));
    return Array.from(new TextDecoder().decode(bytes), (char) => {
      const code = char.charCodeAt(0);
      return code === 127 || (code < 32 && ![9, 10, 13].includes(code))
        ? `\\x${code.toString(16).padStart(2, '0')}`
        : char;
    }).join('');
  } catch {
    return '[无法解码]';
  }
}

export function eventDetails(fields?: null | Record<string, string>): string {
  return Object.entries(fields ?? {})
    .map(([key, value]) =>
      key === 'log_base64' ? `日志=${logText(value)}` : `${key}=${value}`,
    )
    .join('；');
}
