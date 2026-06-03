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

type SiteConfig = {
  path: string
  body: string
}

type MediaKind = 'all' | 'images' | 'docs' | 'videos'

type MediaItem = {
  path: string
  name: string
  kind: 'images' | 'docs' | 'videos'
  size: number
  modified: string
  publicURL: string
  download: string
  snippet: string
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
  async getSiteConfig(): Promise<SiteConfig> {
    return request('/api/site-config')
  },
  async saveSiteConfig(body: string): Promise<SaveResult> {
    return request('/api/site-config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ body })
    })
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
  async listMedia(kind: MediaKind): Promise<MediaItem[]> {
    const items = await request<MediaItem[] | null>(`/api/media?kind=${encodeURIComponent(kind)}`)
    return items || []
  },
  async uploadMedia(kind: Exclude<MediaKind, 'all'>, file: File): Promise<MediaItem> {
    const form = new FormData()
    form.append('kind', kind)
    form.append('file', file)
    return request('/api/media', {
      method: 'POST',
      body: form
    })
  },
  async deleteMedia(path: string): Promise<{ deleted: boolean; path: string }> {
    return request(`/api/media?path=${encodeURIComponent(path)}`, {
      method: 'DELETE'
    })
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

function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

function App() {
  const [activeTab, setActiveTab] = useState<'content' | 'config' | 'media'>('content')
  const [config, setConfig] = useState<Config | null>(null)
  const [pages, setPages] = useState<PageSummary[]>([])
  const [selectedPath, setSelectedPath] = useState('')
  const [page, setPage] = useState<Page | null>(null)
  const [siteConfig, setSiteConfig] = useState<SiteConfig | null>(null)
  const [siteConfigBody, setSiteConfigBody] = useState('')
  const [mediaItems, setMediaItems] = useState<MediaItem[]>([])
  const [mediaKind, setMediaKind] = useState<MediaKind>('all')
  const [uploadKind, setUploadKind] = useState<Exclude<MediaKind, 'all'>>('images')
  const [uploadFile, setUploadFile] = useState<File | null>(null)
  const [uploading, setUploading] = useState(false)
  const [youtubeId, setYoutubeId] = useState('')
  const [body, setBody] = useState('')
  const [frontMatter, setFrontMatter] = useState('')
  const [status, setStatus] = useState('Loading editor...')
  const [saving, setSaving] = useState(false)
  const [previewTick, setPreviewTick] = useState(0)
  const [showPreview, setShowPreview] = useState(true)
  const [showFrontMatter, setShowFrontMatter] = useState(true)
  const [contentMode, setContentMode] = useState<'rich' | 'markdown'>('rich')
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
    if (activeTab !== 'config' || siteConfig) return
    api
      .getSiteConfig()
      .then((nextConfig) => {
        setSiteConfig(nextConfig)
        setSiteConfigBody(nextConfig.body)
        setStatus(`Editing ${nextConfig.path}`)
      })
      .catch((error) => setStatus(error.message))
  }, [activeTab, siteConfig])

  useEffect(() => {
    if (activeTab !== 'media') return
    void loadMedia()
  }, [activeTab, mediaKind])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
        event.preventDefault()
        void saveCurrent()
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
  const sitePreviewURL = config ? `${config.previewURL}?editor=${previewTick}` : ''
  const activePreviewURL = activeTab === 'content' ? previewURL : sitePreviewURL

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

  async function saveSiteConfig() {
    if (!siteConfig || saving) return
    setSaving(true)
    setStatus('Saving Hugo config...')
    try {
      const result = await api.saveSiteConfig(siteConfigBody)
      setPreviewTick((tick) => tick + 1)
      setSiteConfig({ ...siteConfig, body: siteConfigBody })
      setStatus(result.ok ? `Saved ${siteConfig.path} and Hugo build passed.` : `Saved ${siteConfig.path}, but Hugo reported: ${result.error}`)
    } catch (error) {
      setStatus(error instanceof Error ? error.message : 'Config save failed.')
    } finally {
      setSaving(false)
    }
  }

  async function saveCurrent() {
    if (activeTab === 'config') {
      await saveSiteConfig()
      return
    }
    await save()
  }

  async function loadMedia() {
    try {
      const items = await api.listMedia(mediaKind)
      setMediaItems(items)
      setStatus(`Loaded ${items.length} media item${items.length === 1 ? '' : 's'}.`)
    } catch (error) {
      setStatus(error instanceof Error ? error.message : 'Media load failed.')
    }
  }

  async function uploadMedia() {
    if (!uploadFile || uploading) return
    setUploading(true)
    setStatus('Uploading media...')
    try {
      const item = await api.uploadMedia(uploadKind, uploadFile)
      setUploadFile(null)
      setMediaKind(uploadKind)
      const items = await api.listMedia(uploadKind)
      setMediaItems(items)
      setStatus(`Uploaded ${item.name}`)
    } catch (error) {
      setStatus(error instanceof Error ? error.message : 'Upload failed.')
    } finally {
      setUploading(false)
    }
  }

  async function deleteMedia(path: string) {
    if (!window.confirm(`Delete ${path}?`)) return
    setStatus('Deleting media...')
    try {
      await api.deleteMedia(path)
      await loadMedia()
      setStatus(`Deleted ${path}`)
    } catch (error) {
      setStatus(error instanceof Error ? error.message : 'Delete failed.')
    }
  }

  async function copyText(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text)
      setStatus(`Copied ${label}.`)
    } catch {
      setStatus('Copy failed.')
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

  function openLiveSite() {
    const url = activePreviewURL || config?.previewURL || '/preview/'
    window.open(url, '_blank', 'noopener,noreferrer')
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

        {activeTab === 'content' ? (
          <>
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
          </>
        ) : activeTab === 'media' ? (
          <div className="media-sidebar">
            <label>
              <span>Show</span>
              <select value={mediaKind} onChange={(event) => setMediaKind(event.target.value as MediaKind)}>
                <option value="all">All media</option>
                <option value="images">Images</option>
                <option value="docs">Documents</option>
                <option value="videos">Videos</option>
              </select>
            </label>
            <label>
              <span>Upload To</span>
              <select value={uploadKind} onChange={(event) => setUploadKind(event.target.value as Exclude<MediaKind, 'all'>)}>
                <option value="images">Images</option>
                <option value="docs">Documents</option>
                <option value="videos">Videos</option>
              </select>
            </label>
            <input
              type="file"
              onChange={(event) => setUploadFile(event.target.files?.[0] || null)}
            />
            <button type="button" className="primary" onClick={uploadMedia} disabled={!uploadFile || uploading}>
              {uploading ? 'Uploading' : 'Upload'}
            </button>
            <div className="youtube-tool">
              <label>
                <span>YouTube ID</span>
                <input value={youtubeId} onChange={(event) => setYoutubeId(event.target.value.trim())} />
              </label>
              <button
                type="button"
                disabled={!youtubeId}
                onClick={() => copyText(`{{< youtube ${youtubeId} >}}`, 'YouTube shortcode')}
              >
                Copy Shortcode
              </button>
            </div>
          </div>
        ) : (
          <div className="config-sidebar">
            <span>Config File</span>
            <strong>{siteConfig?.path || 'hugo.yaml'}</strong>
            <p>Edit Hugo site settings, menus, params, markup, imaging, and other top-level configuration.</p>
          </div>
        )}
      </aside>

      <section className="workspace">
        <header className="topbar">
          <div className="topbar-left">
            <div className="tabs" aria-label="Editor sections">
              <button
                type="button"
                className={activeTab === 'content' ? 'active' : ''}
                onClick={() => setActiveTab('content')}
              >
                Content
              </button>
              <button
                type="button"
                className={activeTab === 'config' ? 'active' : ''}
                onClick={() => setActiveTab('config')}
              >
                Config
              </button>
              <button
                type="button"
                className={activeTab === 'media' ? 'active' : ''}
                onClick={() => setActiveTab('media')}
              >
                Media
              </button>
            </div>
            <div>
              <strong>
                {activeTab === 'media' ? 'Media library' : activeTab === 'config' ? 'Site config' : page?.title || 'Select a page'}
              </strong>
              {activeTab === 'media' ? (
                <span>{mediaItems.length} item{mediaItems.length === 1 ? '' : 's'}</span>
              ) : activeTab === 'config' ? (
                <span>{siteConfig?.path || 'hugo.yaml'}</span>
              ) : page && <span>{page.path}</span>}
            </div>
          </div>
          <div className="topbar-actions">
            {activeTab === 'content' && (
              <>
                <button type="button" onClick={() => setContentMode((value) => value === 'rich' ? 'markdown' : 'rich')}>
                  {contentMode === 'rich' ? 'Raw Markdown' : 'Rich Text'}
                </button>
                <button type="button" onClick={() => setShowFrontMatter((value) => !value)}>
                  {showFrontMatter ? 'Hide Fields' : 'Show Fields'}
                </button>
              </>
            )}
            <button type="button" onClick={() => setShowPreview((value) => !value)}>
              {showPreview ? 'Hide Preview' : 'Show Preview'}
            </button>
            <button type="button" onClick={openLiveSite}>Live Site</button>
            {activeTab !== 'media' && (
              <button
                type="button"
                className="primary"
                onClick={saveCurrent}
                disabled={(activeTab === 'content' && !page) || (activeTab === 'config' && !siteConfig) || saving}
              >
                {saving ? 'Saving' : 'Save'}
              </button>
            )}
          </div>
        </header>

        <div className={showPreview ? 'editor-grid' : 'editor-grid preview-hidden'}>
          <section className="editor-pane">
            {activeTab === 'content' ? (
              page ? (
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
                  {contentMode === 'rich' ? (
                    <div className="markdown-editor">
                      <MDXEditor
                        key={`${page.path}:rich`}
                        markdown={body}
                        onChange={setBody}
                        plugins={plugins}
                        contentEditableClassName="mdx-content"
                      />
                    </div>
                  ) : (
                    <label className="raw-markdown-editor">
                      <span>Markdown body</span>
                      <textarea
                        spellCheck={false}
                        value={body}
                        onChange={(event) => setBody(event.target.value)}
                      />
                    </label>
                  )}
                </>
              ) : (
                <div className="empty-state">Choose a Markdown file to start editing.</div>
              )
            ) : activeTab === 'media' ? (
              <section className="media-library">
                {mediaItems.length > 0 ? (
                  mediaItems.map((item) => (
                    <article className="media-card" key={item.path}>
                      <div className="media-thumb">
                        {item.kind === 'images' ? (
                          <img src={item.download} alt="" />
                        ) : item.kind === 'videos' ? (
                          <video src={item.download} preload="metadata" />
                        ) : (
                          <span>PDF</span>
                        )}
                      </div>
                      <div className="media-card-body">
                        <strong>{item.name}</strong>
                        <small>{item.path}</small>
                        <small>{formatBytes(item.size)}</small>
                        <code>{item.snippet}</code>
                        <div className="media-actions">
                          <button type="button" onClick={() => copyText(item.snippet, 'snippet')}>Copy</button>
                          <a href={item.download} download>Download</a>
                          <button type="button" onClick={() => deleteMedia(item.path)}>Delete</button>
                        </div>
                      </div>
                    </article>
                  ))
                ) : (
                  <div className="empty-state">No media files found.</div>
                )}
              </section>
            ) : siteConfig ? (
              <label className="config-editor">
                <span>{siteConfig.path}</span>
                <textarea
                  spellCheck={false}
                  value={siteConfigBody}
                  onChange={(event) => setSiteConfigBody(event.target.value)}
                />
              </label>
            ) : (
              <div className="empty-state">Loading Hugo config...</div>
            )}
          </section>

          {showPreview && (
            <section className="preview-pane">
              <div className="preview-header">
                <span>Hugo preview</span>
                {activePreviewURL && (
                  <a href={activePreviewURL} target="_blank" rel="noreferrer">
                    Open
                  </a>
                )}
              </div>
              {activePreviewURL ? (
                <iframe title="Hugo preview" src={activePreviewURL} />
              ) : (
                <div className="empty-state">Preview appears after selecting a page.</div>
              )}
            </section>
          )}
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
