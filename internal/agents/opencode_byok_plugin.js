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
  const ownedHeaders = new WeakMap();

  return {
    ...native,
    "chat.headers": async (hookInput, output) => {
      if (output?.headers) {
        const owned = ownedHeaders.get(output.headers);
        deleteHeaderCaseInsensitive(output.headers, routeHeader);
        if (owned?.baseURL !== undefined && headerValue(output.headers, "x-headroom-base-url") === owned.baseURL) {
          deleteHeaderCaseInsensitive(output.headers, "x-headroom-base-url");
        }
        if (owned?.originalPath !== undefined && headerValue(output.headers, "x-headroom-original-path") === owned.originalPath) {
          deleteHeaderCaseInsensitive(output.headers, "x-headroom-original-path");
        }
        ownedHeaders.delete(output.headers);
      }
      if (headers) await headers(hookInput, output);
      if (!output || !output.headers) return;
      const providerID = hookInput?.model?.providerID;
      const route = typeof providerID === "string" && Object.prototype.hasOwnProperty.call(routes, providerID)
        ? routes[providerID]
        : undefined;
      if (route) {
        deleteHeaderCaseInsensitive(output.headers, "x-headroom-base-url");
        deleteHeaderCaseInsensitive(output.headers, "x-headroom-original-path");

        output.headers[routeHeader] = String(route);
        const upstream = upstreams[providerID];
        let ownedBaseURL;
        let ownedOriginalPath;
        if (upstream) {
          try {
            const u = new URL(upstream);
            ownedBaseURL = u.origin;
            output.headers["x-headroom-base-url"] = ownedBaseURL;
            if (u.pathname && u.pathname !== "/") {
              const cleanPath = u.pathname.replace(/\/$/, "");
              ownedOriginalPath = cleanPath + "/chat/completions";
              output.headers["x-headroom-original-path"] = ownedOriginalPath;
            }
          } catch {
            ownedBaseURL = upstream;
            output.headers["x-headroom-base-url"] = ownedBaseURL;
          }
        } else {
          ownedBaseURL = byokGatewayUrl;
          output.headers["x-headroom-base-url"] = ownedBaseURL;
        }
        ownedHeaders.set(output.headers, { baseURL: ownedBaseURL, originalPath: ownedOriginalPath });
      }
    }
  };
}

function headerValue(headers, targetName) {
  if (!headers || typeof headers !== "object") return undefined;
  const lower = targetName.toLowerCase();
  for (const key of Object.keys(headers)) {
    if (key.toLowerCase() === lower) return headers[key];
  }
  return undefined;
}
