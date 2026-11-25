import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router";
import { expect, test } from "vitest";
import type { Node, Service, Volume } from "../api/generated";
import { VolumeDetailView, VolumesView, deleteBlockReason, filterVolumes } from "./Volumes";

const nodesFixture: Node[] = [
  { id: "node1", hostname: "swarm-mgr", role: "manager", status: "ready", availability: "active", leader: true, firewall: "applied", engine: "28.5.2", os: "linux" },
  { id: "node2", hostname: "swarm-w1", role: "worker", status: "ready", availability: "active", leader: false, firewall: "applied", engine: "28.5.2", os: "linux" },
];

const servicesFixture: Service[] = [
  { id: "svc-db", name: "app_db", image: "postgres:17", mode: "replicated", desired: 1, running: 1, healthy: true },
  { id: "svc-global", name: "node_exporter", image: "prom/node-exporter", mode: "global", desired: 2, running: 2, healthy: true },
];

const volumesFixture: Volume[] = [
  { name: "db_data", driver: "local", node: "node1", services: ["app_db"], created_at: "2026-11-26T00:00:00Z" },
  { name: "scratch", driver: "local", node: "node2", services: [] },
];

test("volumes view renders name, driver, node, consumers, delete block reason, and create dialog", () => {
  const html = renderToStaticMarkup(
    <MemoryRouter>
      <VolumesView
        volumes={volumesFixture}
        nodes={nodesFixture}
        services={servicesFixture}
        unavailableNodes={["swarm-w2"]}
        admin
        loading={false}
        search=""
        onSearch={() => {}}
        nodeFilter=""
        onNodeFilter={() => {}}
        createOpen
        onOpenCreate={() => {}}
        onCloseCreate={() => {}}
        createNode="node1"
        onCreateNode={() => {}}
        createName=""
        onCreateName={() => {}}
        creating={false}
        onCreate={() => {}}
        onDelete={() => {}}
      />
    </MemoryRouter>,
  );

  expect(html).toContain("db_data");
  expect(html).toContain("scratch");
  expect(html).toContain("swarm-mgr");
  expect(html).toContain("swarm-w1");
  expect(html).toContain('href="/services/svc-db"');
  expect(html).toContain("In use by app_db");
  expect(html).toContain("Local volumes unavailable on swarm-w2.");
  expect(html).toContain("<md-dialog");
  expect(html).toContain("Create volume");
});

test("filterVolumes filters by node and consumer name", () => {
  const names = new Map(nodesFixture.map((n) => [n.id, n.hostname]));
  expect(filterVolumes(volumesFixture, "node2", "", names).map((v) => v.name)).toEqual(["scratch"]);
  expect(filterVolumes(volumesFixture, "", "app_db", names).map((v) => v.name)).toEqual(["db_data"]);
  expect(deleteBlockReason(volumesFixture[1])).toBeUndefined();
});

test("volume detail excludes global services from attach options and blocks in-use delete", () => {
  const html = renderToStaticMarkup(
    <MemoryRouter>
      <VolumeDetailView
        volume={volumesFixture[0]}
        nodeHostname="swarm-mgr"
        services={servicesFixture}
        admin
        loading={false}
        serviceID=""
        onServiceID={() => {}}
        target=""
        onTarget={() => {}}
        readOnly={false}
        onReadOnly={() => {}}
        attaching={false}
        onAttach={() => {}}
        deleting={false}
        onDelete={() => {}}
      />
    </MemoryRouter>,
  );

  expect(html).toContain("app_db");
  expect(html).not.toContain("node_exporter");
  expect(html).toContain("In use by app_db. Remove or update those services before deleting.");
});
