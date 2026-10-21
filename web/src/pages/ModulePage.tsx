import { useParams } from "react-router";
import { uiContribution, useModules } from "../modules";
import { UiPage } from "../ui/UiPage";

// The page a module declared, drawn by the panel from the module's document.
export function ModulePage() {
  const { name = "", "*": path = "" } = useParams();
  const modules = useModules();
  if (modules.isPending) return <p>Loading…</p>;

  const page = uiContribution(modules.data, name, path);
  if (!page) return <p className="error">The module {name} has no page at /{path}.</p>;
  return (
    <>
      <h1>{page.title}</h1>
      <UiPage module={name} path={page.path} />
    </>
  );
}
