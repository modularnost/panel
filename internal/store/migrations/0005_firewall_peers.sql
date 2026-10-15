-- Addresses of servers that are about to join the cluster. The swarm's own
-- ports are open to the nodes Swarm already knows and to these, until the node
-- shows up there or a day passes.
CREATE TABLE firewall_peers (
  address    TEXT PRIMARY KEY,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
