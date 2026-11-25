import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { BuilderSetup } from "./BuilderSetup";
import { createPortal } from "react-dom";
import { Navigate, useNavigate, useOutletContext } from "react-router";
import { atLeast, useMe } from "../auth";
import { api, call } from "../api/client";
import { Banner } from "../components/Banner";
import { Button, LinkButton } from "../components/Button";
import { Icon } from "../components/Icon";
import { useDialogs } from "../components/Dialogs";
import { moduleCatalogQuery, modulePath, permissionDescription, slot, useModules } from "../modules";

export function ModuleCatalog() {
  const me = useMe();
  const admin = atLeast(me.data?.role, "admin");
  const client = useQueryClient();
  const navigate = useNavigate();
  const tabActions = useOutletContext<HTMLDivElement | null>();
  const { confirm } = useDialogs();
  const [builderSetup, setBuilderSetup] = useState(false);
  const query = useQuery({ ...moduleCatalogQuery, enabled: admin });
  const installed = useModules(admin);
  const refresh = useMutation({
    mutationFn: () => call(api.refreshModuleCatalog()),
    onSuccess: (data) => client.setQueryData(moduleCatalogQuery.queryKey, data),
  });
  const install = useMutation({
    mutationFn: ({ id, version, revision }: { id: string; version: string; revision: string }) =>
      call(api.installCatalogModule(id, { version, revision })),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ["modules"] });
      navigate("/modules");
    },
    onError: () => {
      void client.invalidateQueries({ queryKey: moduleCatalogQuery.queryKey });
      void client.invalidateQueries({ queryKey: ["modules"] });
    },
  });
  if (!admin) return <Navigate to="/modules" replace />;

  const catalog = query.data;
  const lastChecked = catalog?.checked_at && new Date(catalog.checked_at).toLocaleString(undefined, {
    dateStyle: "medium", timeStyle: "short",
  });
  return (
    <>
      {tabActions && createPortal(
        <Button type="button" variant="icon" disabled={refresh.isPending} aria-busy={refresh.isPending}
          aria-label={refresh.isPending ? "Refreshing catalog" : "Refresh catalog"}
          title={lastChecked ? `Refresh catalog · Last checked ${lastChecked}` : "Refresh catalog"}
          onClick={() => refresh.mutate()}>
          <Icon name="refresh" />
        </Button>,
        tabActions,
      )}
      {query.isPending && <p role="status">Loading catalog…</p>}
      {(install.error || refresh.error || query.error) && (
        <p className="error" role="alert">{(install.error || refresh.error || query.error)?.message}</p>
      )}
      {catalog?.state === "unavailable" && <Banner>Catalog unavailable. Try refreshing it.</Banner>}
      {catalog?.state === "stale" && <Banner>Showing the last available catalog. Refresh it before installing modules.</Banner>}
      {catalog?.state === "ready" && catalog.modules.length === 0 && (
        <div className="module-card module-empty"><h2>No modules in the catalog</h2></div>
      )}
      <div className="module-grid">
        {catalog?.modules.map((module) => {
          const local = installed.data?.find((item) => item.name.toLowerCase() === module.id.toLowerCase());
          const nav = local && slot([local], "nav", me.data?.role)[0]?.contribution;
          const version = module.version;
          return (
            <article className="module-card" key={module.id}>
              <header className="module-card-heading">
                <span className="module-icon"><Icon name="extension" /></span>
                <h2>{module.title}</h2>
              </header>
              <p>{module.summary}</p>
              {module.screenshots.length > 0 && (
                <div className="module-catalog-images">
                  {module.screenshots.map((shot) => <img key={shot.url} src={shot.url} alt={shot.alt} loading="lazy" />)}
                </div>
              )}
              {module.incompatibility && <p className="error">{module.incompatibility}</p>}
              <details className="module-details">
                <summary><Icon name="chevron_right" size={18} />Module details</summary>
                <dl>
                  <dt>Maintainer</dt>
                  <dd>{module.maintainer}</dd>
                  {version && <>
                    <dt>Catalog version</dt>
                    <dd>{version}</dd>
                  </>}
                  <dt>Permissions</dt>
                  <dd>{module.permissions.map(permissionDescription).join(", ") || "None"}</dd>
                </dl>
                <div className="module-links">
                  <a href={module.source_url}>Source</a>
                  {module.docs_url && <a href={module.docs_url}>Documentation</a>}
                </div>
              </details>
              <footer className="module-actions">
                {local && (nav ? (
                  <LinkButton to={modulePath(local.name, nav.path)}>Open {nav.title}</LinkButton>
                ) : (
                  <LinkButton variant="tonal" to="/modules">View installed modules</LinkButton>
                ))}
                {local && <span className="module-install-status muted">Installed</span>}
                {!local && version && (
                  <Button type="button" disabled={catalog.state !== "ready" || !!module.incompatibility || install.isPending}
                    onClick={async () => {
                      if (module.managed_builder) { setBuilderSetup((open) => !open); return; }
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
                    {module.managed_builder ? (builderSetup ? "Hide setup" : "Set up Builder") : install.isPending && install.variables.id === module.id ? "Installing…" : "Install"}
                  </Button>
                )}
                {module.instructions_url && (
                  <a className={`btn ${local || version ? "text" : "tonal"}`} href={module.instructions_url}>Installation guide</a>
                )}
              </footer>
              {!local && builderSetup && module.managed_builder && version && catalog.state === "ready" && !module.incompatibility && (
                <BuilderSetup version={version} revision={module.revision} docs={module.docs_url}
                  permissions={module.permissions.map(permissionDescription)} onInstalled={() => {
                    void client.invalidateQueries({ queryKey: ["modules"] });
                    navigate("/modules");
                  }} />
              )}
            </article>
          );
        })}
      </div>
    </>
  );
}
