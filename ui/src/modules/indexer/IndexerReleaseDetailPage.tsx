import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { enqueueReleaseToDownloader, getPublicRelease } from '../../shared/api/indexer'
import { formatBytes, formatDateTime, formatRuntime } from '../../shared/lib/format'
import type { PublicReleaseDetail } from '../../shared/types'
import { releaseCategoryLabel, simpleSceneName } from './browse'

export function IndexerReleaseDetailPage() {
  const { id = '' } = useParams()
  const [data, setData] = useState<PublicReleaseDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [downloadMessage, setDownloadMessage] = useState<string | null>(null)
  const [downloading, setDownloading] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    void getPublicRelease(id)
      .then((response) => {
        if (!cancelled) {
          setData(response)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load release')
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false)
        }
      })
    return () => {
      cancelled = true
    }
  }, [id])

  async function handleSendToDownloader() {
    if (!data) return
    setDownloading(true)
    setDownloadMessage(null)
    try {
      await enqueueReleaseToDownloader(data.release.release_id, data.release.title)
      setDownloadMessage('Release sent to downloader queue.')
    } catch (err) {
      setDownloadMessage(err instanceof Error ? err.message : 'Failed to send release')
    } finally {
      setDownloading(false)
    }
  }

  const simpleName = useMemo(() => (data ? simpleSceneName(data.release.title) : ''), [data])

  if (loading) {
    return <div className="banner">Loading release detail...</div>
  }
  if (error) {
    return <div className="banner error">{error}</div>
  }
  if (!data) {
    return <div className="banner error">Release not found.</div>
  }

  const { release, media, external, files, capabilities } = data
  const preview = data.preview?.url ? data.preview : null

  return (
    <div className="page-section public-detail">
      <div className="page-card public-detail__hero">
        <div className="public-detail__hero-copy">
          <p className="eyebrow">Release</p>
          <h1 className="page-title">{release.title}</h1>
          <div className="public-detail__scene">{simpleName}</div>
        </div>
        {preview ? (
          <div className="public-detail__preview">
            <img src={preview.url} alt={`${release.title} preview`} loading="lazy" />
          </div>
        ) : null}
        <div className="button-row">
          <Link className="secondary-button" to="/indexer/releases">
            Back to browse
          </Link>
          {capabilities.can_send_to_downloader ? (
            <button className="primary-button" onClick={handleSendToDownloader} disabled={downloading}>
              {downloading ? 'Sending...' : 'Download NZB'}
            </button>
          ) : null}
        </div>
      </div>

      {downloadMessage ? <div className="banner">{downloadMessage}</div> : null}

      <div className="public-detail__grid">
        <div className="page-card">
          <h2 className="section-title">Release Info</h2>
          <dl className="detail-grid">
            <div>
              <dt>Category</dt>
              <dd>{releaseCategoryLabel(release)}</dd>
            </div>
            <div>
              <dt>Size</dt>
              <dd>{formatBytes(release.size_bytes)}</dd>
            </div>
            <div>
              <dt>Files</dt>
              <dd>
                {release.file_count} · <a className="table-link" href="#release-files">view files</a>
              </dd>
            </div>
            <div>
              <dt>Posted</dt>
              <dd>{formatDateTime(release.posted_at)}</dd>
            </div>
            <div>
              <dt>Added</dt>
              <dd>{formatDateTime(release.added_at)}</dd>
            </div>
            <div>
              <dt>Runtime</dt>
              <dd>{formatRuntime(media.runtime_seconds)}</dd>
            </div>
          </dl>
        </div>

        <div className="page-card">
          <h2 className="section-title">Movie / TV Info</h2>
          <dl className="detail-grid">
            <div>
              <dt>Title</dt>
              <dd>{external.external_title || 'Unknown'}</dd>
            </div>
            <div>
              <dt>Type</dt>
              <dd>{external.external_media_type || 'Unknown'}</dd>
            </div>
            <div>
              <dt>Year</dt>
              <dd>{external.external_year || 'Unknown'}</dd>
            </div>
            <div>
              <dt>IMDb</dt>
              <dd>{external.imdb_id || 'None'}</dd>
            </div>
            <div>
              <dt>TMDb</dt>
              <dd>{external.tmdb_id || 'None'}</dd>
            </div>
            <div>
              <dt>TVDb</dt>
              <dd>{external.tvdb_id || 'None'}</dd>
            </div>
          </dl>
        </div>

      </div>

      <div className="page-card" id="release-files">
        <h2 className="section-title">Files</h2>
        <div className="table-shell">
          <table className="data-table public-data-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th>Size</th>
              </tr>
            </thead>
            <tbody>
              {files.map((file) => (
                <tr key={`${file.file_index}-${file.file_name}`}>
                  <td>{file.file_name}</td>
                  <td>{file.is_pars ? 'PAR2' : 'Payload'}</td>
                  <td>{formatBytes(file.size_bytes)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
