import { useParams } from "react-router";
import { moduleURL } from "../modules";

// A module renders its own page; the panel only frames it. The proxy decides
// what a framed request gets, so the module needs no knowledge of this shell.
export function ModulePage() {
  const { name = "", "*": path = "" } = useParams();
  return <iframe className="module-frame" title={name} src={moduleURL(name, path)} />;
}
