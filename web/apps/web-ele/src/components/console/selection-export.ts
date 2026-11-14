export function selectionCSV(rows: (number | string)[][]) {
  return (
    '\uFEFF' +
    rows
      .map((row) =>
        row
          .map((value) => {
            let cell = String(value);
            if (
              typeof value === 'string' &&
              (/^\s*[=+@-]/.test(cell) || /^[\t\r\n]/.test(cell))
            )
              cell = "'" + cell;
            return '"' + cell.replaceAll('"', '""') + '"';
          })
          .join(','),
      )
      .join('\r\n')
  );
}

export function downloadSelection(name: string, rows: (number | string)[][]) {
  const url = URL.createObjectURL(
    new Blob([selectionCSV(rows)], { type: 'text/csv;charset=utf-8' }),
  );
  const link = document.createElement('a');
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
