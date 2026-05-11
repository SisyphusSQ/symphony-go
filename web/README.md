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

## Development Proxy

Start the Go operator server on loopback first, then run the Vite dev server:

```bash
# from the repository root
symphony run --workflow WORKFLOW.md --port 4002 --instance dev

# from web/
npm run dev
```

Use `VITE_OPERATOR_PROXY_TARGET` when the operator server is not listening on
`http://127.0.0.1:4002`.

## Production Serving

The Go operator server serves the production dashboard from `web/dist` when
`web/dist/index.html` exists. Build the frontend before starting the operator
server from the repository root:

```bash
cd web
npm run build
cd ..
symphony run --workflow WORKFLOW.md --port 4002 --instance dev
```

Production browser requests to `/` and frontend history routes return the Vite
`index.html`. `/api/v1/*` remains reserved for the Go operator API. Browser
refreshes on `/runs/<run_id>` return the dashboard when the request accepts
`text/html`; non-browser requests keep the existing operator run JSON behavior.

## API Mode

- `VITE_OPERATOR_API_BASE`: optional absolute API base URL. When unset, the app
  uses relative `/api/v1` paths so the Vite dev proxy can handle requests.
- `VITE_OPERATOR_PROXY_TARGET`: optional Vite proxy target for `/api/v1`.

The production app does not fall back to mock data. If the local operator API is
unavailable, the UI shows the API error or an empty state. Mock fixtures are
limited to Storybook stories and component tests.

## Run Detail

- Select a run from the dashboard table, or open `/runs/<run_id>` in the Vite
  app to load a specific run detail.
- The detail pane loads `/api/v1/runs/{run_id}` and
  `/api/v1/runs/{run_id}/events`.
- Timeline category filters map to the backend `category` query parameter.
- Raw payloads are shown as expandable redacted JSON and can be copied to the
  clipboard. The UI intentionally does not provide a JSON download entry.
