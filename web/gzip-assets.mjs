import { readdir, readFile, writeFile } from "node:fs/promises";
import { gzipSync } from "node:zlib";

const assets = new URL("dist/assets/", import.meta.url);
for (const name of await readdir(assets)) {
  if (!name.endsWith(".js") && !name.endsWith(".css")) continue;
  const source = await readFile(new URL(name, assets));
  await writeFile(new URL(`${name}.gz`, assets), gzipSync(source));
}
