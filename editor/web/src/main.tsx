import React, { useEffect, useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import {
  BlockTypeSelect,
  BoldItalicUnderlineToggles,
  CreateLink,
  headingsPlugin,
  InsertTable,
  InsertThematicBreak,
  linkDialogPlugin,
  linkPlugin,
  listsPlugin,
  ListsToggle,
  markdownShortcutPlugin,
  MDXEditor,
  quotePlugin,
  tablePlugin,
  thematicBreakPlugin,
  toolbarPlugin,
  UndoRedo
} from '@mdxeditor/editor'
import '@mdxeditor/editor/style.css'
import './styles.css'

type PageSummary = {
  path: string
  title: string
  url: string
}

type Page = {
  path: string
  title: string
  url: string
  delimiter: string
  frontMatter: string
  body: string
}

type Config = {
  siteDir: string
  previewURL: string
  hugoLog: string
  hugoRunning: boolean
}

type SaveResult = {
  ok: boolean
  output?: string
  error?: string
  url: string
}

const api = {
  async getConfig(): Promise<Config> {
    return request('/api/config')
  },
  async listPages(): Promise<PageSummary[]> {
    return request('/api/pages')
  },
  async getPage(path: string): Promise<Page> {
    return request(`/api/page?path=${encodeURIComponent(path)}`)
  },
  async savePage(page: Page): Promise<SaveResult> {
    return request('/api/page', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(page)
    })
  },
  async createPage(path: string, title: string): Promise<{ path: string; url: string }> {
    return request('/api/page', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, title })
    })
  },
  async startPreview(): Promise<{ previewURL: string }> {
    return request('/api/preview/start', { method: 'POST' })
  }
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, init)
  const data = await response.json().catch(() => ({}))
  if (!response.ok) {
    throw new Error(data.error || response.statusText)
  }
  return data
}

function App() {
  const [config, setConfig] = useState<Config | null>(null)
  const [pages, setPages] = useState<PageSummary[]>([])
  const [selectedPath, setSelectedPath] = useState('')
  const [page, setPage] = useState<Page | null>(null)
  const [body, setBody] = useState('')
  const [frontMatter, setFrontMatter] = useState('')
  const [status, setStatus] = useState('Loading editor...')
  const [saving, setSaving] = useState(false)
  const [previewTick, setPreviewTick] = useState(0)
  const [showFrontMatter, setShowFrontMatter] = useState(true)
  const [query, setQuery] = useState('')

  const plugins = useMemo(
    () => [
      headingsPlugin(),
      listsPlugin(),
      quotePlugin(),
      thematicBreakPlugin(),
      linkPlugin(),
      linkDialogPlugin(),
      tablePlugin(),
      markdownShortcutPlugin(),
      toolbarPlugin({
        toolbarContents: () => (
          <>
            <UndoRedo />
            <BlockTypeSelect />
            <BoldItalicUnderlineToggles />
            <ListsToggle />
            <CreateLink />
            <InsertTable />
            <InsertThematicBreak />
          </>
        )
      })
    ],
    []
  )

  useEffect(() => {
    Promise.all([api.getConfig(), api.listPages()])
      .then(([nextConfig, nextPages]) => {
        setConfig(nextConfig)
        setPages(nextPages)
        setStatus(nextConfig.hugoRunning ? 'Preview is running.' : 'Preview is not running.')
        if (nextPages.length > 0) {
          setSelectedPath(nextPages[0].path)
        }
      })
      .catch((error) => setStatus(error.message))
  }, [])

  useEffect(() => {
    if (!selectedPath) return
    api
      .getPage(selectedPath)
      .then((nextPage) => {
        setPage(nextPage)
        setBody(nextPage.body)
        setFrontMatter(nextPage.frontMatter)
        setStatus(`Editing ${nextPage.path}`)
      })
      .catch((error) => setStatus(error.message))
  }, [selectedPath])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
        event.preventDefault()
        void save()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  })

  const filteredPages = pages.filter((item) => {
    const haystack = `${item.title} ${item.path}`.toLowerCase()
    return haystack.includes(query.toLowerCase())
  })

  const previewURL = page && config ? `${config.previewURL.replace(/\/$/, '')}${page.url}?editor=${previewTick}` : ''

  async function refreshPages(pathToSelect = selectedPath) {
    const nextPages = await api.listPages()
    setPages(nextPages)
    if (pathToSelect) {
      setSelectedPath(pathToSelect)
    }
  }

  async function save() {
    if (!page || saving) return
    setSaving(true)
    setStatus('Saving...')
    try {
      const result = await api.savePage({
        ...page,
        frontMatter,
        body
      })
      setPreviewTick((tick) => tick + 1)
      await refreshPages(page.path)
      setStatus(result.ok ? 'Saved and Hugo build passed.' : `Saved, but Hugo reported: ${result.error}`)
    } catch (error) {
      setStatus(error instanceof Error ? error.message : 'Save failed.')
    } finally {
      setSaving(false)
    }
  }

  async function createPage() {
    const path = window.prompt('New content path, for example news/update/index.md')
    if (!path) return
    const title = window.prompt('Page title') || 'Untitled'
    setStatus('Creating page...')
    try {
      const created = await api.createPage(path, title)
      await refreshPages(created.path)
      setSelectedPath(created.path)
      setStatus(`Created ${created.path}`)
    } catch (error) {
      setStatus(error instanceof Error ? error.message : 'Create failed.')
    }
  }

  async function startPreview() {
    setStatus('Starting Hugo preview...')
    try {
      const next = await api.startPreview()
      const nextConfig = await api.getConfig()
      setConfig(nextConfig)
      setPreviewTick((tick) => tick + 1)
      setStatus(`Preview is running at ${next.previewURL}`)
    } catch (error) {
      setStatus(error instanceof Error ? error.message : 'Preview failed to start.')
    }
  }

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div>
            <h1>Hugo Editor</h1>
            <p>{config?.siteDir || 'Loading site'}</p>
          </div>
        </div>

        <div className="sidebar-actions">
          <input
            aria-label="Filter pages"
            placeholder="Filter pages"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          <button type="button" onClick={createPage}>New</button>
        </div>

        <nav className="page-list" aria-label="Content pages">
          {filteredPages.map((item) => (
            <button
              type="button"
              className={item.path === selectedPath ? 'selected' : ''}
              key={item.path}
              onClick={() => setSelectedPath(item.path)}
            >
              <span>{item.title}</span>
              <small>{item.path}</small>
            </button>
          ))}
        </nav>
      </aside>

      <section className="workspace">
        <header className="topbar">
          <div>
            <strong>{page?.title || 'Select a page'}</strong>
            {page && <span>{page.path}</span>}
          </div>
          <div className="topbar-actions">
            <button type="button" onClick={() => setShowFrontMatter((value) => !value)}>
              {showFrontMatter ? 'Hide Fields' : 'Show Fields'}
            </button>
            <button type="button" onClick={startPreview}>Preview</button>
            <button type="button" className="primary" onClick={save} disabled={!page || saving}>
              {saving ? 'Saving' : 'Save'}
            </button>
          </div>
        </header>

        <div className="editor-grid">
          <section className="editor-pane">
            {page ? (
              <>
                {showFrontMatter && (
                  <label className="frontmatter-editor">
                    <span>Front matter</span>
                    <textarea
                      spellCheck={false}
                      value={frontMatter}
                      onChange={(event) => setFrontMatter(event.target.value)}
                    />
                  </label>
                )}
                <div className="markdown-editor">
                  <MDXEditor
                    key={page.path}
                    markdown={body}
                    onChange={setBody}
                    plugins={plugins}
                    contentEditableClassName="mdx-content"
                  />
                </div>
              </>
            ) : (
              <div className="empty-state">Choose a Markdown file to start editing.</div>
            )}
          </section>

          <section className="preview-pane">
            <div className="preview-header">
              <span>Hugo preview</span>
              {page && config && (
                <a href={previewURL} target="_blank" rel="noreferrer">
                  Open
                </a>
              )}
            </div>
            {previewURL ? (
              <iframe title="Hugo preview" src={previewURL} />
            ) : (
              <div className="empty-state">Preview appears after selecting a page.</div>
            )}
          </section>
        </div>

        <footer className="statusbar">
          <span>{status}</span>
          {config?.hugoLog && <span>Log: {config.hugoLog}</span>}
        </footer>
      </section>
    </main>
  )
}

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
)
