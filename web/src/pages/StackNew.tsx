import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { ComposeEditor } from "../components/ComposeEditor";
import { SAMPLE_COMPOSE, validStackName } from "../stacks";

export function StackNew() {
  const [name, setName] = useState("");
  const navigate = useNavigate();
  const bad = name !== "" && !validStackName(name);

  return (
    <>
      <p className="muted">
        <Link to="/stacks">Stacks</Link>
      </p>
      <h1>New stack</h1>
      <div className="form">
        <label>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="shop" autoFocus />
        </label>
        {bad && <p className="error">Letters, digits, dot, dash and underscore; up to 63 characters.</p>}
      </div>
      <ComposeEditor
        name={validStackName(name) ? name : ""}
        initial={SAMPLE_COMPOSE}
        onDeployed={(n) => navigate(`/stacks/${n}`)}
      />
    </>
  );
}
