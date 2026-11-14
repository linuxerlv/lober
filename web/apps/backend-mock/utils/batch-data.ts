export function summarizeBatch(tasks: { state: string }[]) {
  const counts = { pending: 0, active: 0, done: 0, failed: 0, cancelled: 0 };
  for (const { state } of tasks) {
    switch (state) {
      case 'cancelled':
      case 'done':
      case 'failed':
      case 'pending': {
        counts[state]++;
        break;
      }
      default: {
        counts.active++;
      }
    }
  }
  return counts;
}
