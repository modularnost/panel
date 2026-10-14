import { useQuery } from "@tanstack/react-query";
import { api, call } from "./api/client";

const RANK: Record<string, number> = { viewer: 1, operator: 2, admin: 3 };

// atLeast mirrors the server's role order. The server is what enforces it; this
// only decides which buttons are worth showing.
export function atLeast(role: string | undefined, need: "viewer" | "operator" | "admin"): boolean {
  return (RANK[role ?? ""] ?? 0) >= RANK[need];
}

export function useMe() {
  return useQuery({
    queryKey: ["me"],
    queryFn: () => call(api.getMe()),
  });
}
