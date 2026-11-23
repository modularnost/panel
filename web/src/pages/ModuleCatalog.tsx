import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, useNavigate } from "react-router";
import { atLeast, useMe } from "../auth";
import { api, call } from "../api/client";
import { Banner } from "../components/Banner";
import { Button } from "../components/Button";
import { useDialogs } from "../components/Dialogs";
import { formatTime } from "../format";
import { useModules } from "../modules";

const catalogKey = ["module-catalog"] as const;

export function ModuleCatalog() {
  const me = useMe();
  const admin = atLeast(me.data?.role, "admin");
  const client = useQueryClient();
  const navigate = useNavigate();
  const { confirm } = useDialogs();
  const query = useQuery({ queryKey: catalogKey, queryFn: () => call(api.getModuleCatalog()), enabled: admin });
  const installed = useModules(admin);
  const refresh = useMutation({
    mutationFn: () => call(api.refreshModuleCatalog()),
    onSuccess: (data) => client.setQueryData(catalogKey, data),
  });
  const install = useMutation({
    mutationFn: ({ id, version, revision }: { id: string; version: string; revision: string }) =>
      call(api.installCatalogModule(id, { version, revision })),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ["modules"] });
      navigate("/modules");
    },
    onError: () => {
      void client.invalidateQueries({ queryKey: catalogKey });
      void client.invalidateQueries({ queryKey: ["modules"] });
    },
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
      {(install.error || refresh.error || query.error) && (
        <p className="error" role="alert">{(install.error || refresh.error || query.error)?.message}</p>
      )}
      {catalog?.state === "unavailable" && <Banner>Catalog unavailable. Try refreshing it.</Banner>}
      {catalog?.state === "stale" && <Banner>Showing the last available catalog. Refresh it before installing modules.</Banner>}
      {catalog?.state === "ready" && catalog.modules.length === 0 && <p>No modules in the catalog.</p>}
      {catalog?.modules.map((module) => {
        const isInstalled = installed.data?.some((item) => item.name.toLowerCase() === module.id.toLowerCase());
        const version = module.version;
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
              {!isInstalled && version && (
                <Button type="button" disabled={catalog.state !== "ready" || !!module.incompatibility || install.isPending}
                  onClick={async () => {
                    const permissions = module.permissions.map(permissionDescription).join(", ") || "none";
                    const storage = module.mount_path ? ` Data at ${module.mount_path} stays on the same Swarm node.` : "";
                    if (await confirm({
                      title: `Install ${module.title}?`,
                      message: `Install version ${version} from ${module.maintainer}. Permissions: ${permissions}.${storage}`,
                      action: "Install",
                    })) {
                      install.mutate({ id: module.id, version, revision: module.revision });
                    }
                  }}
                >
                  {install.isPending && install.variables.id === module.id ? "Installing…" : "Install"}
                </Button>
              )}
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
