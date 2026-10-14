import { useQuery } from "@tanstack/react-query";
import { api, call } from "./api/client";

// The server caches the check for hours; asking every few minutes only notices
// a manual "check now" done from another tab.
export function useUpdates(enabled: boolean) {
  return useQuery({
    queryKey: ["updates"],
    queryFn: () => call(api.getUpdates()),
    refetchInterval: 5 * 60_000,
    enabled,
  });
}

// installCommand is the manual update: the installer takes the image tag, which
// is the release tag without the "v".
export function installCommand(tag: string): string {
  const version = tag.replace(/^v/, "");
  return `curl -fsSL https://raw.githubusercontent.com/modularnost/panel/main/scripts/install.sh | PANEL_VERSION=${version} sh`;
}

// usePanelVersion is the running version, which /healthz states without a
// sign-in; it only changes with a restart, and the page reloads after one.
export function usePanelVersion() {
  return useQuery({
    queryKey: ["version"],
    queryFn: async () => healthzVersion(await (await fetch("/healthz")).text()),
    staleTime: Infinity,
  });
}

// healthzVersion reads the version out of /healthz ("ok v0.2.0").
export function healthzVersion(body: string): string {
  return body.trim().replace(/^ok\s+/, "");
}

// waitForVersion polls /healthz until the panel answers with version, which is
// the update having finished; the panel is down for a moment while it swaps,
// so a failed request only means "not yet". False after the timeout.
export async function waitForVersion(version: string, timeoutMs = 3 * 60_000): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 2000));
    try {
      const resp = await fetch("/healthz", { cache: "no-store" });
      if (resp.ok && healthzVersion(await resp.text()) === version) return true;
    } catch {
      // The old process is gone and the new one is not up yet.
    }
  }
  return false;
}
