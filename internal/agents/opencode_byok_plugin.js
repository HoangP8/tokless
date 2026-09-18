import HeadroomPlugin from "./entry.opencode.js";

const routeHeader = "X-Tokless-Route";

function deleteHeaderCaseInsensitive(headers, targetName) {
  if (!headers || typeof headers !== "object") return;
  const lower = targetName.toLowerCase();
  for (const key of Object.keys(headers)) {
    if (key.toLowerCase() === lower) {
      delete headers[key];
    }
  }
}

export default async function ToklessBYOKPlugin(input, options = {}) {
  const { routes = {}, upstreams = {}, byokGatewayUrl = "http://127.0.0.1:18787", ...pluginOptions } = options;
  const native = await HeadroomPlugin(input, pluginOptions);
  const headers = native["chat.headers"];

  return {
    ...native,
    "chat.headers": async (hookInput, output) => {
      if (headers) await headers(hookInput, output);
      if (!output || !output.headers) return;
      const providerID = hookInput?.model?.providerID;
      const route = providerID && routes[providerID];
      if (route) {
        deleteHeaderCaseInsensitive(output.headers, routeHeader);
        deleteHeaderCaseInsensitive(output.headers, "x-headroom-base-url");
        deleteHeaderCaseInsensitive(output.headers, "x-headroom-original-path");

        output.headers[routeHeader] = String(route);
        const upstream = upstreams[providerID];
        if (upstream) {
          try {
            const u = new URL(upstream);
            output.headers["x-headroom-base-url"] = u.origin;
            if (u.pathname && u.pathname !== "/") {
              const cleanPath = u.pathname.replace(/\/$/, "");
              output.headers["x-headroom-original-path"] = cleanPath + "/chat/completions";
            }
          } catch {
            output.headers["x-headroom-base-url"] = upstream;
          }
        } else {
          output.headers["x-headroom-base-url"] = byokGatewayUrl;
        }
      } else {
        deleteHeaderCaseInsensitive(output.headers, routeHeader);
        deleteHeaderCaseInsensitive(output.headers, "x-headroom-base-url");
        deleteHeaderCaseInsensitive(output.headers, "x-headroom-original-path");
      }
    }
  };
}
