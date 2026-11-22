import { useQuery } from "@tanstack/react-query";
import { api, call } from "./api/client";

// The server caches the check for hours; asking every few minutes only notices
// a manual "check now" done from another tab.
export function updateInProgress(state: string | undefined) {
  return state === "requested" || state === "updating" || state === "rolling_back";
}

export function useUpdates(enabled: boolean, watch = false) {
  return useQuery({
    queryKey: ["updates"],
    queryFn: () => call(api.getUpdates()),
    refetchInterval: ({ state }) => {
      const update = state.data?.last_update?.state;
      if (watch && updateInProgress(update)) return 3000;
      return watch && update === "unknown" ? 15_000 : 5 * 60_000;
    },
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
