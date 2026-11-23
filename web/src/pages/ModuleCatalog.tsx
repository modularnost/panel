import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Navigate } from "react-router";
import { atLeast, useMe } from "../auth";
import { api, call } from "../api/client";
import { Banner } from "../components/Banner";
import { Button } from "../components/Button";
import { formatTime } from "../format";
import { useModules } from "../modules";

const catalogKey = ["module-catalog"] as const;

export function ModuleCatalog() {
  const me = useMe();
  const admin = atLeast(me.data?.role, "admin");
  const client = useQueryClient();
  const query = useQuery({ queryKey: catalogKey, queryFn: () => call(api.getModuleCatalog()), enabled: admin });
  const installed = useModules(admin);
  const refresh = useMutation({
    mutationFn: () => call(api.refreshModuleCatalog()),
    onSuccess: (data) => client.setQueryData(catalogKey, data),
  });
  if (!admin) return <Navigate to="/modules" replace />;

  const catalog = query.data;
  return (
    <>
      <div className="toolbar">
        <Button type="button" variant="tonal" disabled={refresh.isPending} onClick={() => refresh.mutate()}>
          {refresh.isPending ? "Refreshing…" : "Refresh catalog"}
        </Button>
        {catalog?.checked_at && (
          <span className="muted">Last checked <time dateTime={catalog.checked_at}>{formatTime(catalog.checked_at)}</time></span>
        )}
      </div>
      {query.isPending && <p role="status">Loading catalog…</p>}
      {(refresh.error || query.error) && (
        <p className="error" role="alert">{(refresh.error || query.error)?.message}</p>
      )}
      {catalog?.state === "unavailable" && <Banner>Catalog unavailable. Try refreshing it.</Banner>}
      {catalog?.state === "stale" && <Banner>Showing the last available catalog. Refresh it before installing modules.</Banner>}
      {catalog?.state === "ready" && catalog.modules.length === 0 && <p>No modules in the catalog.</p>}
      {catalog?.modules.map((module) => {
        const isInstalled = installed.data?.some((item) => item.name.toLowerCase() === module.id.toLowerCase());
        return (
          <article className="module-catalog-card" key={module.id}>
            <h2>{module.title}</h2>
            <p>{module.summary}</p>
            <p className="muted">{module.maintainer}{module.version && ` · ${module.version}`}</p>
            <p>Permissions: {module.permissions.map(permissionDescription).join(", ") || "none"}</p>
            {module.screenshots.length > 0 && (
              <div className="module-catalog-images">
                {module.screenshots.map((shot) => <img key={shot.url} src={shot.url} alt={shot.alt} loading="lazy" />)}
              </div>
            )}
            {module.incompatibility && <p>{module.incompatibility}</p>}
            <div className="toolbar">
              {isInstalled && <Link to="/modules">Installed</Link>}
              {module.instructions_url && <a href={module.instructions_url}>Installation guide</a>}
              <a href={module.source_url}>Source</a>
              {module.docs_url && <a href={module.docs_url}>Documentation</a>}
            </div>
          </article>
        );
      })}
    </>
  );
}

function permissionDescription(permission: string) {
  switch (permission) {
    case "stats:read": return "Read resource statistics (stats:read)";
    case "service:read": return "Read Swarm services (service:read)";
    case "service:update": return "Redeploy Swarm services (service:update)";
    case "service:create": return "Create Swarm services (service:create)";
    default: return permission;
  }
}
