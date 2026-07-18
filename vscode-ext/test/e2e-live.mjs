/**
 * Live e2e: extension AcpClient → real maclaw-acp-bridge → running MaClaw GUI
 * (Mode B). Sends one trivial prompt and prints the streamed updates.
 */
import * as path from "path";
import { fileURLToPath } from "url";
import { AcpClient } from "./out/acpClient.cjs";

const root = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const bridge = path.resolve(root, "..", "dist", "maclaw-acp-bridge.exe");

const client = new AcpClient();
client.on("log", (l) => console.error("[bridge]", l));
client.on("update", (p) => {
  const u = p.update;
  if (u.sessionUpdate === "agent_message_chunk") {
    process.stdout.write(u.content?.text ?? "");
  } else if (u.sessionUpdate === "agent_thought_chunk") {
    /* quiet */
  } else {
    console.log(`\n[${u.sessionUpdate}] ${u.title ?? ""} ${u.status ?? ""}`);
  }
});
client.on("permission", (perm) => {
  console.log("\n[permission]", perm.params?.toolCall?.title, "→ auto allow_once");
  client.resolvePermission(perm.rpcId, "allow_once");
});

const init = await client.start(bridge, "0.1.0-e2e");
console.log("init ok:", init.agentInfo?.name ?? JSON.stringify(init.agentInfo));

const sessionId = await client.newSession(process.cwd());
console.log("session:", sessionId);

const res = await client.prompt(sessionId, "只回复两个字：收到。不要调用任何工具。");
console.log("\nstopReason:", res.stopReason);
client.stop();
process.exit(0);
