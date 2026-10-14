const UNITS = ["B", "KiB", "MiB", "GiB", "TiB"];

export function formatBytes(n: number): string {
  let value = n;
  let unit = 0;
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024;
    unit++;
  }
  // Whole bytes need no decimals; a bigger unit reads best with one.
  return `${unit === 0 ? value : value.toFixed(1)} ${UNITS[unit]}`;
}

// shortDigest keeps what tells two images apart: sha256:abcdef... becomes abcdef123456.
export function shortDigest(digest?: string): string {
  return (digest ?? "").replace(/^sha256:/, "").slice(0, 12);
}

export function formatTime(iso: string): string {
  return new Date(iso).toLocaleString();
}

// memoryUse is "used / limit", or only what is used when no limit is set.
export function memoryUse(mem: number, limit: number): string {
  return limit > 0 ? `${formatBytes(mem)} / ${formatBytes(limit)}` : formatBytes(mem);
}
