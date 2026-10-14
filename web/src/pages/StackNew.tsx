import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { ComposeEditor } from "../components/ComposeEditor";
import { SAMPLE_COMPOSE, validStackName } from "../stacks";
import { TextField } from "../components/TextField";

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
        <TextField
          label="Name"
          value={name}
          onChange={setName}
          placeholder="shop"
          autoFocus
          error={bad ? "Letters, digits, dot, dash and underscore; up to 63 characters." : undefined}
        />
      </div>
      <ComposeEditor
        name={validStackName(name) ? name : ""}
        initial={SAMPLE_COMPOSE}
        onDeployed={(n) => navigate(`/stacks/${n}`)}
      />
    </>
  );
}
