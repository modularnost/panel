import type { Service } from "./api/generated";

export const resourceNamePattern = "[A-Za-z0-9][A-Za-z0-9_.\\-]{0,62}";
export const resourceMaxBytes = 500 * 1024;

export function isStandaloneService(service?: Pick<Service, "stack">): boolean {
  return !!service && !service.stack;
}

export function encodeResourceData(bytes: Uint8Array): string {
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 8192) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192));
  }
  return btoa(binary);
}

// The generator maps format: byte to Blob, but the JSON wire value is base64.
export function resourceJSONBytes(bytes: Uint8Array): Blob {
  return encodeResourceData(bytes) as unknown as Blob;
}
