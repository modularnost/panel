import { useQuery } from "@tanstack/react-query";
import { api, call } from "./api/client";
import type { Contribution, Module, Service } from "./api/generated";
import { atLeast } from "./auth";

export function useModules(enabled = true) {
  return useQuery({
    queryKey: ["modules"],
    queryFn: () => call(api.listModules()),
    refetchInterval: 10_000,
    enabled,
  });
}

export type Slotted = { module: string; contribution: Contribution };

// slot lists what a role may use of one slot, from the modules that are ready.
// The panel hides the rest as a courtesy; the proxy is what refuses it.
export function slot(modules: Module[] | undefined, name: Contribution["slot"], role: string | undefined): Slotted[] {
  return (modules ?? [])
    .filter((m) => m.ok)
    .flatMap((m) => m.contributions.map((contribution) => ({ module: m.name, contribution })))
    .filter(({ contribution: c }) => c.slot === name && atLeast(role, c.role));
}

// matchesLabels: the service carries every label of match, with that value.
export function matchesLabels(match: Record<string, string>, labels: Record<string, string> | undefined): boolean {
  return Object.entries(match).every(([k, v]) => labels?.[k] === v);
}

// serviceTabs are the tabs of one service: the slot, narrowed by the labels.
export function serviceTabs(modules: Module[] | undefined, role: string | undefined, s: Service): Slotted[] {
  return slot(modules, "service.tab", role).filter(({ contribution }) => matchesLabels(contribution.match, s.labels));
}

// serviceContext is what a service tab is told about its service. A module
// takes it as a convenience, not as proof: anybody can write that query.
export function serviceContext(s: Service): string {
  const q = new URLSearchParams({ service: s.name, id: s.id });
  if (s.stack) q.set("stack", s.stack);
  return `?${q}`;
}

// moduleURL is where a module's own page lives. The panel proxies it under
// /modules/<name>/, and the module chooses what follows.
export function moduleURL(name: string, path: string): string {
  return `/modules/${name}/${path.replace(/^\/+/, "")}`;
}

// modulePath is the SPA route that frames the page. It cannot be moduleURL:
// the server answers that one, so a reload there would skip the shell.
export function modulePath(name: string, path: string): string {
  return `/module/${name}/${path.replace(/^\/+/, "")}`;
}

// envLines turns the textarea into KEY=value entries, ignoring blank lines.
export function envLines(text: string): string[] {
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l !== "");
}
