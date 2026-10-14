import { useParams } from "react-router";
import { moduleURL, serviceContext, uiContribution, useModules } from "../../modules";
import { UiPage } from "../../ui/UiPage";
import { useService } from "./Layout";

// A tab a module put on this service, told which service it is on: a document
// the panel draws, or the module's own page in a frame.
export function ModuleTab() {
  const s = useService();
  const { name = "", "*": path = "" } = useParams();
  const modules = useModules();
  if (modules.isPending) return <p>Loading…</p>;

  const page = uiContribution(modules.data, name, path);
  if (page) return <UiPage module={name} path={page.path} context={serviceContext(s)} />;
  return <iframe className="module-frame tab" title={name} src={moduleURL(name, path) + serviceContext(s)} />;
}
