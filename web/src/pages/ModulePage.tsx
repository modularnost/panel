import { useParams } from "react-router";
import { moduleURL, uiContribution, useModules } from "../modules";
import { UiPage } from "../ui/UiPage";

// A page the module declared as a document is drawn by the panel; any other is
// the module's own HTML, framed. The proxy decides what a framed request gets,
// so the module needs no knowledge of this shell.
export function ModulePage() {
  const { name = "", "*": path = "" } = useParams();
  const modules = useModules();
  if (modules.isPending) return <p>Loading…</p>;

  const page = uiContribution(modules.data, name, path);
  if (page) {
    return (
      <>
        <h1>{page.title}</h1>
        <UiPage module={name} path={page.path} />
      </>
    );
  }
  return <iframe className="module-frame" title={name} src={moduleURL(name, path)} />;
}
