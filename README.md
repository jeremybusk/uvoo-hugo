# uvoo-hugo

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
go build -o ./bin/uvoo-hugo-editor ./editor
```

Run the editor against the included Hugo demo site:

```bash
export UVOO_HUGO_EDITOR_AUTH_USER=admin
export UVOO_HUGO_EDITOR_AUTH_PASSWORD="$(openssl rand -base64 32)"

./bin/uvoo-hugo-editor \
  -site ./hugo_website_demo \
  -addr 127.0.0.1:1314 \
  -hugo-addr 127.0.0.1:1313
```

Open:

```text
http://127.0.0.1:1314
```

Sign in with the username from `UVOO_HUGO_EDITOR_AUTH_USER` and the generated
password printed in your shell environment. The editor serves the Hugo preview
through the authenticated `/preview/` route, so the demo site preview is
available at:

```text
http://127.0.0.1:1314/preview/
```

The editor opens on the `Content` tab by default. Use the `Config` tab to edit
`hugo.yaml`, including site params, menu items, markup settings, and other Hugo
configuration. Use `Hide Preview` for a full-width editing area, or `Live Site`
to open the current preview in another tab.

Use the `Media` tab to upload and manage reusable site media:

- Images go to `assets/images/` and copy as Hugo image shortcodes.
- PDFs go to `static/media/docs/` and copy as Markdown links.
- Local videos go to `static/media/video/` and copy as Hugo video shortcodes.
- YouTube videos can be added by copying a Hugo YouTube shortcode from a video ID.

For most public website videos, prefer YouTube or Vimeo embeds instead of
hosting video files directly. Local video uploads are best for short clips where
you accept the bandwidth and browser-format tradeoffs.

## Development Run

For frontend development, run the Go API and Vite separately:

```bash
export UVOO_HUGO_EDITOR_AUTH_USER=admin
export UVOO_HUGO_EDITOR_AUTH_PASSWORD=dev-password
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

Run the local CI target before publishing changes:

```bash
make ci
```

Create a release tag and let GitHub Actions publish the release:

```bash
VERSION=v0.1.0 make release
```

Successful CI runs build and push the container image to GHCR as
`ghcr.io/uvoo/uvoo-hugo`.

## License

Uvoo Hugo Editor is licensed under the Apache License, Version 2.0. See
[LICENSE](LICENSE) and [NOTICE](NOTICE).
