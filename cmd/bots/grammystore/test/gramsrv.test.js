import test from "node:test";
import assert from "node:assert/strict";
import { GramsrvClient } from "../src/gramsrv.js";

test("Admin API retries reuse a deterministic command id", () => {
  const client = new GramsrvClient({ gramsrvActor: "test" });
  const first = client.command("purchase", { user_id: 1 }, "payment:charge-1");
  const retry = client.command("purchase", { user_id: 1 }, "payment:charge-1");
  const other = client.command("purchase", { user_id: 1 }, "payment:charge-2");
  assert.equal(first.command_id, retry.command_id);
  assert.notEqual(first.command_id, other.command_id);
});
