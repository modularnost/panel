export function configTarget(target: string): boolean {
  if (target.length < 2 || target.length > 4096) return false;
  if (target[0] !== "/" || target[target.length - 1] === "/") return false;
  if (/[\0\r\n\\:]/.test(target) || target.includes("//")) return false;
  return target
    .slice(1)
    .split("/")
    .every((segment) => segment !== "." && segment !== "..");
}

export function readConfigData(data: unknown): { bytes: Uint8Array<ArrayBuffer>; text: string | undefined } {
  if (typeof data !== "string") {
    throw new TypeError("Config data must be a base64 string");
  }
  const binary = atob(data);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  if (bytes.includes(0)) {
    return { bytes, text: undefined };
  }
  try {
    const text = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes);
    return { bytes, text };
  } catch {
    return { bytes, text: undefined };
  }
}
