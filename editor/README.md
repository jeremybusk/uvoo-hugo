# Hugo Local Editor

This is a local editor for the Hugo site in `../hugo_website_demo`.

## Run

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

The Go server listens on `http://127.0.0.1:1314` and starts Hugo preview at
`http://127.0.0.1:1313`. Hugo output is written to `hugo-server.log`.

## Production-style run

Build the React UI, then let the Go server serve it:

```bash
cd editor/web
npm run build
cd ../..
go run ./editor
```

Open `http://127.0.0.1:1314`.

## Notes

- Only Markdown files under the Hugo `content/` directory are editable.
- Front matter is edited separately from the Markdown body to avoid corrupting
  Hugo-specific fields.
- Saving writes the file, runs `hugo --source <site> --quiet`, and reports any
  build error in the status bar.
