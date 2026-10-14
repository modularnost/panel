// moduleURL is where a module's own page lives. The panel proxies it under
// /modules/<name>/, and the module chooses what follows.
export function moduleURL(name: string, path: string): string {
  return `/modules/${name}/${path.replace(/^\/+/, "")}`;
}

// modulePath is the SPA route that frames the page. It cannot be moduleURL:
// the server answers that one, so a reload there would skip the shell.
export function modulePath(name: string, path: string): string {
  return `/module/${name}/${path.replace(/^\/+/, "")}`;
}

// envLines turns the textarea into KEY=value entries, ignoring blank lines.
export function envLines(text: string): string[] {
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l !== "");
}
