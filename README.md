# uvoohugo

This repo contains a Hugo demo site and a small authenticated local editor for
editing the site's Markdown content.

## Requirements

- Go 1.22 or newer
- Node.js and npm
- Hugo Extended

## Compile And Run

Build the React editor UI, then compile the Go editor binary:

```bash
cd editor/web
npm install
npm run build
cd ../..
go build -o ./bin/uvoohugo-editor ./editor
```

Run the editor against the included Hugo demo site:

```bash
export UVOOHUGO_EDITOR_AUTH_USER=admin
export UVOOHUGO_EDITOR_AUTH_PASSWORD="$(openssl rand -base64 32)"

./bin/uvoohugo-editor \
  -site ./hugo_website_demo \
  -addr 127.0.0.1:1314 \
  -hugo-addr 127.0.0.1:1313
```

Open:

```text
http://127.0.0.1:1314
```

Sign in with the username from `UVOOHUGO_EDITOR_AUTH_USER` and the generated
password printed in your shell environment. The editor serves the Hugo preview
through the authenticated `/preview/` route, so the demo site preview is
available at:

```text
http://127.0.0.1:1314/preview/
```

The editor opens on the `Content` tab by default. Use the `Config` tab to edit
`hugo.yaml`, including site params, menu items, markup settings, and other Hugo
configuration. Use `Hide Preview` for a full-width editing area, or `Live Site`
to open the current preview in another tab. In `Content`, use `Raw Markdown` to
switch from the rich text editor to direct Markdown editing, then `Rich Text` to
switch back.

## Development Run

For frontend development, run the Go API and Vite separately:

```bash
export UVOOHUGO_EDITOR_AUTH_USER=admin
export UVOOHUGO_EDITOR_AUTH_PASSWORD=dev-password
go run ./editor -site ./hugo_website_demo
```

In another terminal:

```bash
cd editor/web
npm install
npm run dev
```

Open:

```text
http://127.0.0.1:5173
```

## Packaging

Package builds are documented in [docs/PACKAGE.md](docs/PACKAGE.md).
