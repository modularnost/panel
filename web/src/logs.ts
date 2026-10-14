export const MAX_LINES = 5000;

// parseLine reads one "line" event. The server sends {"text": "..."}; anything
// else is shown as it arrived rather than dropped.
export function parseLine(data: string): string {
  try {
    const parsed = JSON.parse(data) as { text?: unknown };
    return typeof parsed.text === "string" ? parsed.text : data;
  } catch {
    return data;
  }
}

// capLines keeps the newest lines: a log that runs for days must not grow without bound.
export function capLines(lines: string[], max = MAX_LINES): string[] {
  return lines.length > max ? lines.slice(lines.length - max) : lines;
}
