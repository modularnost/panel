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

// modulePath is the SPA route of a module's page.
export function modulePath(name: string, path: string): string {
  return `/module/${name}/${path.replace(/^\/+/, "")}`;
}

const trimSlash = (p: string) => (p.length > 1 ? p.replace(/\/+$/, "") : p);

// uiContribution is the contribution a route shows, when the module declared that path.
export function uiContribution(modules: Module[] | undefined, name: string, path: string): Contribution | undefined {
  const want = trimSlash("/" + path.replace(/^\/+/, ""));
  return modules
    ?.find((m) => m.name === name)
    ?.contributions.find((c) => trimSlash(c.path) === want);
}

// envLines turns the textarea into KEY=value entries, ignoring blank lines.
export function envLines(text: string): string[] {
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l !== "");
}
