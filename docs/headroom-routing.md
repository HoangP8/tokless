# Headroom Routing

Tokless should use one boring rule:

> Agent -> local Headroom -> original provider

No shell interception. No global API-key replacement. No custom proxy per agent.

## One Diagram

```text
                              OAuth or API key
                         stays in agent/provider
                                  |
                                  v
+----------------+       +----------------------+       +------------------+
| Claude         |       |                      |       | Anthropic        |
| Codex          |       |                      |       | OpenAI           |
| OpenCode       | ----> | Headroom             | ----> | Gemini           |
| Copilot        |       | 127.0.0.1:8787       |       | user's BYOK      |
| Droid / Pi ... |       |                      |       | gateway          |
+----------------+       +----------------------+       +------------------+
       ^                         |
       |                         v
       |                  cache and route
       |
       +-- Tokless changes only this agent's native endpoint setting

Before enable:
  agent -------------------------------> provider

After `tokless proxy up`:
  agent -------------------------------> Headroom ----------------> provider
                                           127.0.0.1:8787

After `tokless proxy down`:
  agent -------------------------------> provider
```

## User Workflow

```text
1. Sign in to agent, or configure BYOK, normally.
2. Run: tokless proxy up --agents <agent>
3. Use agent normally.
4. Run: tokless proxy status
5. To undo: tokless proxy down --agents <agent>
```

For all selected agents:

```text
tokless proxy up
tokless proxy down
```

`proxy down --agents <list>` restores only those agents and leaves shared
Headroom running. Plain `proxy down` restores the full Tokless-managed set and
stops Headroom after every route is removed.

## OAuth And BYOK

Both follow same diagram:

```text
OAuth:  agent's existing login  -> Headroom -> provider OAuth endpoint
BYOK:   agent's existing API key -> Headroom -> user's provider/gateway
```

Tokless must not copy OAuth tokens or API keys into a central Tokless file.
Credentials stay in the agent's normal credential store or environment.

### OpenCode

OpenCode uses the Tokless transport plugin. Provider definitions stay in
`config.json`; their native `baseURL` values remain the provider upstreams.
`opencode.json` adds the plugin endpoint and opaque route credentials:

```text
plugin proxyUrl: http://127.0.0.1:8787
route map: provider ID -> opencode:<provider>.<opaque-token>
```

The plugin reads the OpenCode `providerID`, adds one `X-Tokless-Route` header
for registered BYOK providers, and sends the request to Headroom on port 8787.
OAuth providers have no route entry and no route header.

```text
OpenCode BYOK:
  provider -> Headroom :8787
           -> Tokless BYOK gateway :18787
           -> registered provider upstream

OpenAI OAuth:
  provider -> Headroom :8787
           -> https://api.provider.example
```

Headroom is the shared first hop. It sends requests with `X-Tokless-Route` to
the Tokless BYOK gateway. Requests without that header stay on Headroom's
normal OAuth/upstream path. The route header is removed before any upstream
request. Tokless never uses an unregistered route or fallback destination.

Current OpenCode BYOK routes use the OpenAI-compatible chat protocol. The
route registry also accepts `openai-responses` and `anthropic-messages` for
providers that expose those APIs.

## Revert Safety

When enabling an agent, Tokless records:

```text
agent config before change
agent config after change
Tokless ownership marker
```

When disabling:

```text
if agent config still equals Tokless version:
    restore the recorded original
else:
    stop and report conflict
    do not overwrite user's newer change
```

This gives one reliable rule:

> Tokless restores only what Tokless still owns.

If user changes provider, login, model, or endpoint while proxy is active,
`proxy down` must refuse that agent and print what to do. Refusing is safer than
destroying a new credential or provider setting.

## Adding New Agent Or Provider

No shell file changes needed.

```text
1. Add one adapter for that agent's native endpoint/config format.
2. Add read-only detection: managed, foreign, conflict, or unsupported.
3. Add ownership marker and exact restore state.
4. Add tests: enable, disable, user edit, malformed config, OAuth, and BYOK.
5. Register adapter.
```

The new adapter owns its config format. Existing agents do not change.

## Deliberate Non-Goals

```text
No ~/.zshrc or ~/.bashrc edits
No universal environment-variable injection
No global credential vault
No automatic takeover when ownership is unclear
No wrapper unless an agent has no native endpoint/config support
```

Native configuration reaches both CLI and GUI launches. Shell startup hacks do
not, so they are not part of the Headroom design.

## Platform Exceptions

```text
Copilot: native routing is unavailable; Tokless uses a managed CLI shim and a
          separate VS Code proxy. Remove only when each still matches Tokless.
          On Windows, a foreign existing Copilot command is never replaced;
          configure fails closed until user moves it or installs Tokless first.
Antigravity: native .env routing is used; Windows also gets marked user-level
             environment values for desktop launches.
Cursor: manual-only. Tokless does not claim observable routing state.
```

These exceptions are why `proxy status` reports capability and ownership rather
than claiming every agent has the same route mechanism.
