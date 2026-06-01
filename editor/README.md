# Hugo Local Editor

This is a local editor for the Hugo site in `../hugo_website_demo`.

## Compile And Run The Demo Site

From the repository root, build the React UI and compile the Go binary:

```bash
cd editor/web
npm install
npm run build
cd ../..
mkdir -p bin
go build -o ./bin/uvoohugo-editor ./editor
```

Set credentials and run the editor against `hugo_website_demo`:

```bash
export UVOOHUGO_EDITOR_AUTH_USER=admin
export UVOOHUGO_EDITOR_AUTH_PASSWORD="$(openssl rand -base64 32)"

./bin/uvoohugo-editor \
  -site ./hugo_website_demo \
  -addr 127.0.0.1:1314 \
  -hugo-addr 127.0.0.1:1313
```

Open `http://127.0.0.1:1314` and sign in with the configured Basic Auth
credentials. The Hugo preview is served through the authenticated editor at
`http://127.0.0.1:1314/preview/`.

## Development Run

Set editor credentials:

```bash
export UVOOHUGO_EDITOR_AUTH_USER=admin
export UVOOHUGO_EDITOR_AUTH_PASSWORD="$(openssl rand -base64 32)"
```

Install the React dependencies once:

```bash
cd editor/web
npm install
```

Start the Go API and Hugo preview server from the repository root:

```bash
go run ./editor
```

In another terminal, start the React dev server:

```bash
cd editor/web
npm run dev
```

Open `http://127.0.0.1:5173`.

The Go server listens on `http://127.0.0.1:1314` and starts Hugo preview on a
local port. Preview is proxied through the authenticated editor at `/preview/`.
Hugo output is written to `hugo-server.log`.

## Production-style run

Build the React UI, then let the Go server serve it:

```bash
cd editor/web
npm run build
cd ../..
UVOOHUGO_EDITOR_AUTH_USER=admin \
UVOOHUGO_EDITOR_AUTH_PASSWORD=change-me \
go run ./editor -site ./hugo_website_demo
```

Open `http://127.0.0.1:1314`.

## Auth

HTTP Basic Auth is required. Set credentials with flags or environment variables:

```bash
UVOOHUGO_EDITOR_AUTH_USER=admin \
UVOOHUGO_EDITOR_AUTH_PASSWORD=change-me \
go run ./editor
```

If you bind to `0.0.0.0`, put the editor behind HTTPS. Basic Auth protects
access, but credentials are only confidential over TLS.

## Notes

- Only Markdown files under the Hugo `content/` directory are editable.
- Front matter is edited separately from the Markdown body to avoid corrupting
  Hugo-specific fields.
- Saving writes the file, runs `hugo --source <site> --quiet`, and reports any
  build error in the status bar.
