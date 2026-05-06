# Operator Web GUI

This directory contains the read-only Vue 3 + Ant Design Vue dashboard shell for
the local Symphony operator API. It includes the run list, run detail, and turn
timeline views for redacted `/api/v1` payloads.

## Commands

```bash
npm install
npm run dev
npm test
npm run typecheck
npm run build
```

`npm run dev` serves the Vite app on `127.0.0.1:5173` and proxies `/api/v1` to
`http://127.0.0.1:4002` by default.

## API Mode

- `VITE_OPERATOR_API_BASE`: optional absolute API base URL. When unset, the app
  uses relative `/api/v1` paths so the Vite dev proxy can handle requests.
- `VITE_OPERATOR_PROXY_TARGET`: optional Vite proxy target for `/api/v1`.
- `VITE_OPERATOR_MOCK`: `auto`, `always`, or `never`. The default `auto` uses
  mock fixtures when the local operator API is unavailable.

## Run Detail

- Select a run from the dashboard table, or open `/runs/<run_id>` in the Vite
  app to load a specific run detail.
- The detail pane loads `/api/v1/runs/{run_id}` and
  `/api/v1/runs/{run_id}/events`.
- Timeline category filters map to the backend `category` query parameter.
- Raw payloads are shown as expandable redacted JSON and can be copied to the
  clipboard. The UI intentionally does not provide a JSON download entry.

This slice does not add Go static asset serving. Production embedding/serving is
tracked separately.
