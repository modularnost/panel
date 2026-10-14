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
