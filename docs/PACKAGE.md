# Packaging Uvoo Hugo Editor

The editor can be built as a single Linux binary with the React UI embedded.
GoReleaser is configured to produce `.deb`, `.rpm`, and `.tar.gz` artifacts.

## Compile without packages

To build and run a plain binary from a checkout:

```bash
cd editor/web
npm install
npm run build
cd ../..
mkdir -p bin
go build -o ./bin/uvoo-hugo-editor ./editor
```

Serve the included demo site:

```bash
export UVOO_HUGO_EDITOR_AUTH_USER=admin
export UVOO_HUGO_EDITOR_AUTH_PASSWORD="$(openssl rand -base64 32)"

./bin/uvoo-hugo-editor \
  -site ./hugo_website_demo \
  -addr 127.0.0.1:1314 \
  -hugo-addr 127.0.0.1:1313
```

Open `http://127.0.0.1:1314`. The Hugo preview is available through the same
authenticated service at `http://127.0.0.1:1314/preview/`.

## Build a snapshot package

Install Go, Node.js, npm, Hugo, and GoReleaser, then run from the repo root:

```bash
goreleaser release --snapshot --clean
```

Packages are written to `dist/`.

## Install

Install the package for your distro:

```bash
sudo apt install ./dist/uvoo-hugo-editor_*_linux_amd64.deb
```

or:

```bash
sudo rpm -Uvh ./dist/uvoo-hugo-editor-*.x86_64.rpm
```

## Configure a user service

The package installs a systemd user unit:

```bash
/usr/lib/systemd/user/uvoo-hugo-editor.service
```

Create a per-user env file:

```bash
mkdir -p ~/.config/uvoo-hugo-editor
cp /usr/share/doc/uvoo-hugo-editor/editor.env.example ~/.config/uvoo-hugo-editor/editor.env
chmod 600 ~/.config/uvoo-hugo-editor/editor.env
```

Edit the env file and set:

```bash
UVOO_HUGO_EDITOR_AUTH_USER=admin
UVOO_HUGO_EDITOR_AUTH_PASSWORD=<long-random-password>
UVOO_HUGO_EDITOR_SITE=/home/user1/hugo_website_demo
UVOO_HUGO_EDITOR_ADDR=127.0.0.1:1314
# Optional behind a public reverse proxy:
# UVOO_HUGO_EDITOR_PUBLIC_URL=https://editor.example.com
```

The packaged user service grants write access to `%h/hugo_website_demo` and
`%h/hugo-server.log`. If your site lives somewhere else, create a systemd user
override:

```bash
systemctl --user edit uvoo-hugo-editor.service
```

Then set the matching writable paths:

```ini
[Service]
ReadWritePaths=
ReadWritePaths=/path/to/hugo_website_demo /path/to/hugo-server.log
```

Start the service:

```bash
systemctl --user daemon-reload
systemctl --user enable --now uvoo-hugo-editor.service
```

Open:

```text
http://127.0.0.1:1314
```

## Internet exposure

HTTP Basic Auth protects the editor and the proxied Hugo preview, but Basic Auth
credentials are only confidential when transported over HTTPS. If binding to
`0.0.0.0`, put the service behind TLS, a VPN, or a reverse proxy such as Caddy,
nginx, or Traefik.

The Go editor exposes one public HTTP service. Hugo preview is bound separately
to localhost and proxied under `/preview/`, so users do not need to expose the
Hugo server port.

## Environment variables

- `UVOO_HUGO_EDITOR_AUTH_USER`: required Basic Auth username.
- `UVOO_HUGO_EDITOR_AUTH_PASSWORD`: required Basic Auth password.
- `UVOO_HUGO_EDITOR_AUTH_PASSWORD_FILE`: optional file containing the password.
- `UVOO_HUGO_EDITOR_ADDR`: editor listen address, default `127.0.0.1:1314`.
- `UVOO_HUGO_EDITOR_PUBLIC_URL`: optional public editor URL for Hugo preview links.
- `UVOO_HUGO_EDITOR_SITE`: Hugo site path, default `hugo_website_demo`.
- `UVOO_HUGO_EDITOR_HUGO_ADDR`: local Hugo preview address, default `127.0.0.1:1313`.
- `UVOO_HUGO_EDITOR_START_HUGO`: whether to start Hugo on launch, default `true`.
