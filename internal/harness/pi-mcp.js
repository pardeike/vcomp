// pi has no native MCP client. This optional extension exposes the same tools
// as the other harnesses through a company-bound stdio child, without dependencies.
import { spawn } from "node:child_process";
import { createInterface } from "node:readline";

export default async function (pi) {
  const child = spawn(options.command, options.args, {
    cwd: options.cwd, stdio: ["pipe", "pipe", "ignore"],
  });
  const pending = new Map();
  let serial = 0;
  let stopped;
  const stop = (error) => {
    if (stopped) return;
    stopped = error;
    for (const { reject, timer } of pending.values()) {
      clearTimeout(timer);
      reject(error);
    }
    pending.clear();
    child.kill();
  };
  child.on("error", stop);
  child.stdin.on("error", stop);
  child.on("exit", () => stop(new Error("vcomp MCP server stopped")));
  const lines = createInterface({ input: child.stdout });
  lines.on("line", (line) => {
    let reply;
    try { reply = JSON.parse(line); }
    catch { stop(new Error("Invalid vcomp MCP response")); return; }
    const waiter = pending.get(reply.id);
    if (!waiter) return;
    pending.delete(reply.id);
    clearTimeout(waiter.timer);
    if (reply.error) waiter.reject(new Error(reply.error.message));
    else waiter.resolve(reply.result);
  });
  const request = (method, params) => new Promise((resolve, reject) => {
    if (stopped) { reject(stopped); return; }
    const id = ++serial;
    const timer = setTimeout(() => stop(new Error("vcomp MCP request timed out")), options.timeout);
    pending.set(id, { resolve, reject, timer });
    child.stdin.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
  });
  pi.on("session_shutdown", async () => {
    stop(new Error("pi session closed"));
    lines.close();
  });
  try {
    await request("initialize", {
      protocolVersion: "2025-11-25", capabilities: {},
      clientInfo: { name: "vcomp-pi", version: "1" },
    });
    child.stdin.write(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }) + "\n");
    const { tools } = await request("tools/list", {});
    for (const tool of tools) {
      pi.registerTool({
        name: "vcomp_" + tool.name,
        label: tool.name,
        description: "Optional company convenience. " + tool.description,
        parameters: tool.inputSchema,
        async execute(_id, args) {
          const result = await request("tools/call", { name: tool.name, arguments: args });
          if (result.isError) throw new Error(result.content.map(c => c.text || "").join("\n"));
          return { content: result.content, details: {} };
        },
      });
    }
  } catch (error) {
    stop(error);
    console.error("Optional vcomp company tools unavailable:", error.message);
  }
}
