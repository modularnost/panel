import { useParams } from "react-router";
import { moduleURL, serviceContext } from "../../modules";
import { useService } from "./Layout";

// A tab a module put on this service: its page, told which service it is on.
export function ModuleTab() {
  const s = useService();
  const { name = "", "*": path = "" } = useParams();
  return <iframe className="module-frame tab" title={name} src={moduleURL(name, path) + serviceContext(s)} />;
}
