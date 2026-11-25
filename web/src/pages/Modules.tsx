import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, Outlet } from "react-router";
import { api, call, retryableQueryError } from "../api/client";
import { RefreshWarning } from "../components/RefreshWarning";
import { atLeast, useMe } from "../auth";
import { moduleCatalogQuery, modulePath, permissionDescription, slot } from "../modules";
import { Button, LinkButton } from "../components/Button";
import { useDialogs } from "../components/Dialogs";
import { Icon } from "../components/Icon";
import { Tab, Tabs } from "../components/Tabs";

export function Modules() {
  const me = useMe();
  const admin = atLeast(me.data?.role, "admin");
  const [tabActions, setTabActions] = useState<HTMLDivElement | null>(null);
  return (
    <div className="modules-page">
      <header className="modules-heading">
        <h1>Modules</h1>
        {admin && (
          <LinkButton variant="outlined" to="/install-module">Install from image</LinkButton>
        )}
      </header>
      {admin && (
        <div className="module-tabs-row">
          <Tabs label="Modules">
            <Tab to="/modules/catalog" end>Catalog</Tab>
            <Tab to="/modules" end>Installed</Tab>
          </Tabs>
          <div ref={setTabActions} />
        </div>
      )}
      <Outlet context={tabActions} />
    </div>
  );
}

export function InstalledModules() {
  const me = useMe();
  const queryClient = useQueryClient();
  const admin = atLeast(me.data?.role, "admin");
  const { confirm } = useDialogs();
  const catalog = useQuery({ ...moduleCatalogQuery, enabled: admin });
  const entries = admin ? catalog.data?.modules : undefined;

  const modules = useQuery({
    queryKey: ["modules"],
    queryFn: () => call(api.listModules()),
  });
  const uninstall = useMutation({
    mutationFn: (name: string) => call(api.uninstallModule(name)),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["modules"] }),
  });

  if (modules.isPending) return <p role="status">Loading installed modules…</p>;
  if (modules.isError && (modules.data === undefined || !retryableQueryError(modules.error))) {
    return <p className="error" role="alert">{modules.error.message}</p>;
  }

  return (
    <>
      <RefreshWarning error={modules.error} />
      {uninstall.isError && <p className="error" role="alert">{uninstall.error.message}</p>}
      {modules.data.length === 0 ? (
        <div className="module-card module-empty">
          <h2>No installed modules</h2>
          {admin && <LinkButton variant="tonal" to="/modules/catalog">Browse catalog</LinkButton>}
        </div>
      ) : (
        <div className="module-grid">
          {modules.data.map((m) => {
            const nav = slot([m], "nav", me.data?.role)[0]?.contribution;
            const serviceTabs = slot([m], "service.tab", me.data?.role)
              .map(({ contribution }) => contribution.title).join(", ");
            const summary = entries?.find((entry) => entry.id.toLowerCase() === m.name.toLowerCase())?.summary;
            const title = nav?.title || m.name;
            const issue = m.error || (m.state === "starting" ? "Waiting for Swarm replicas" : "Module is not responding");
            const service = `/services/${encodeURIComponent(m.service)}`;
            return (
              <article className="module-card" key={m.name}>
                <header className="module-card-heading">
                  <span className="module-icon"><Icon name="extension" /></span>
                  <h2>{title}</h2>
                </header>
                {!m.ok && <p className={m.state === "starting" ? "muted" : "error"}>{issue}</p>}
                {summary ? <p>{summary}</p> : serviceTabs && <p>Service tabs: {serviceTabs}</p>}
                <details className="module-details">
                  <summary><Icon name="chevron_right" size={18} />Module details</summary>
                  <dl>
                    {m.version && <>
                      <dt>Installed version</dt>
                      <dd>{m.version}</dd>
                    </>}
                    <dt>Swarm service</dt>
                    <dd><Link to={service}><code>{m.service}</code></Link></dd>
                    <dt>Permissions</dt>
                    <dd>{m.permissions.map(permissionDescription).join(", ") || "None"}</dd>
                    {summary && serviceTabs && <>
                      <dt>Service tabs</dt>
                      <dd>{serviceTabs}</dd>
                    </>}
                  </dl>
                </details>
                {(nav || !m.ok || admin) && (
                  <footer className="module-actions">
                    {nav && <LinkButton to={modulePath(m.name, nav.path)}>Open {nav.title}</LinkButton>}
                    {!m.ok && <LinkButton variant="tonal" to={`${service}/logs`}>View service logs</LinkButton>}
                    {admin && (
                      <Button
                        variant="text"
                        aria-label={`Remove ${title}`}
                        disabled={uninstall.isPending}
                        onClick={async () =>
                          (await confirm({
                            title: `Remove ${title}?`,
                            message: "Its service is stopped; the image and its data stay.",
                            action: "Remove",
                            danger: true,
                          })) && uninstall.mutate(m.name)
                        }
                      >
                        Remove
                      </Button>
                    )}
                  </footer>
                )}
              </article>
            );
          })}
        </div>
      )}
    </>
  );
}
