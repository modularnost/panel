import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router";
import { expect, test } from "vitest";
import type { Node, ServiceVolume, Volume } from "../../api/generated";
import { ServiceVolumesView } from "./Volumes";

const nodesFixture: Node[] = [
  { id: "node1", hostname: "swarm-w1", role: "worker", status: "ready", availability: "active", leader: false, firewall: "applied", engine: "28.5.2", os: "linux" },
];

const mountsFixture: ServiceVolume[] = [
  { name: "demo-data", node: "node1", target: "/data", read_only: true },
];

const volumesFixture: Volume[] = [
  { name: "demo-data", driver: "local", node: "node1", services: ["demo-api"] },
];

test("service volumes view renders mounted volume target, access mode, and attach dialog", () => {
  const html = renderToStaticMarkup(
    <MemoryRouter>
      <ServiceVolumesView
        volumes={mountsFixture}
        nodes={nodesFixture}
        nodeVolumes={volumesFixture}
        canAttach
        loading={false}
        attachOpen
        onOpenAttach={() => {}}
        onCloseAttach={() => {}}
        nodeID="node1"
        onNodeID={() => {}}
        volumeName="demo-data"
        onVolumeName={() => {}}
        target="/cache"
        onTarget={() => {}}
        readOnly={false}
        onReadOnly={() => {}}
        nodeVolumesLoading={false}
        attaching={false}
        onAttach={() => {}}
      />
    </MemoryRouter>,
  );

  expect(html).toContain('href="/volumes/node1/demo-data"');
  expect(html).toContain("swarm-w1");
  expect(html).toContain("/data");
  expect(html).toContain("Read-only");
  expect(html).toContain("<md-dialog");
  expect(html).toContain("Attach volume");
});
