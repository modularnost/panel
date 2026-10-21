import { useParams } from "react-router";
import { serviceContext, uiContribution, useModules } from "../../modules";
import { UiPage } from "../../ui/UiPage";
import { useService } from "./Layout";

// A tab a module put on this service, told which service it is on.
export function ModuleTab() {
  const s = useService();
  const { name = "", "*": path = "" } = useParams();
  const modules = useModules();
  if (modules.isPending) return <p>Loading…</p>;

  const page = uiContribution(modules.data, name, path);
  if (!page) return <p className="error">The module {name} has no tab at /{path}.</p>;
  return <UiPage module={name} path={page.path} context={serviceContext(s)} />;
}
