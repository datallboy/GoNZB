import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { enqueueReleaseToDownloader, getPublicRelease } from '../../shared/api/indexer'
import { formatBytes, formatDateTime, formatNumber, formatRuntime } from '../../shared/lib/format'
import type { PublicReleaseDetail } from '../../shared/types'

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

  return (
    <div className="page-section stack">
      <div className="page-card">
        <div className="page-header">
          <div>
            <p className="eyebrow">Release Detail</p>
            <h1 className="page-title">{release.title}</h1>
            <p className="muted-copy">
              {release.guid} · {release.classification || 'unclassified'}
            </p>
          </div>
          <div className="button-row">
            <Link className="secondary-button" to="/indexer/releases">
              Back to catalog
            </Link>
            {capabilities.can_send_to_downloader ? (
              <button className="primary-button" onClick={handleSendToDownloader} disabled={downloading}>
                {downloading ? 'Sending...' : 'Send to Downloader'}
              </button>
            ) : null}
          </div>
        </div>
        {downloadMessage ? <div className="banner">{downloadMessage}</div> : null}
      </div>

      <div className="dashboard-grid">
        <div className="page-card">
          <h2 className="section-title">Release</h2>
          <dl className="detail-grid">
            <div>
              <dt>Posted</dt>
              <dd>{formatDateTime(release.posted_at)}</dd>
            </div>
            <div>
              <dt>Size</dt>
              <dd>{formatBytes(release.size_bytes)}</dd>
            </div>
            <div>
              <dt>Completion</dt>
              <dd>{formatNumber(release.completion_pct)}%</dd>
            </div>
            <div>
              <dt>Files</dt>
              <dd>{release.file_count}</dd>
            </div>
            <div>
              <dt>PAR2</dt>
              <dd>{release.has_par2 ? 'Yes' : 'No'}</dd>
            </div>
            <div>
              <dt>NFO</dt>
              <dd>{release.has_nfo ? 'Yes' : 'No'}</dd>
            </div>
          </dl>
        </div>

        <div className="page-card">
          <h2 className="section-title">Media</h2>
          <dl className="detail-grid">
            <div>
              <dt>Runtime</dt>
              <dd>{formatRuntime(media.runtime_seconds)}</dd>
            </div>
            <div>
              <dt>Resolution</dt>
              <dd>{media.primary_resolution || 'Unknown'}</dd>
            </div>
            <div>
              <dt>Video Codec</dt>
              <dd>{media.primary_video_codec || 'Unknown'}</dd>
            </div>
            <div>
              <dt>Audio Codec</dt>
              <dd>{media.primary_audio_codec || 'Unknown'}</dd>
            </div>
            <div>
              <dt>Subtitles</dt>
              <dd>{media.subtitle_languages.join(', ') || 'None'}</dd>
            </div>
            <div>
              <dt>Sample Present</dt>
              <dd>{media.sample_present ? 'Yes' : 'No'}</dd>
            </div>
          </dl>
        </div>

        <div className="page-card">
          <h2 className="section-title">External</h2>
          <dl className="detail-grid">
            <div>
              <dt>Title</dt>
              <dd>{external.external_title || 'Unknown'}</dd>
            </div>
            <div>
              <dt>Media Type</dt>
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

      <div className="page-card">
        <h2 className="section-title">Files</h2>
        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <th>Index</th>
                <th>File</th>
                <th>Size</th>
                <th>Parts</th>
                <th>Posted</th>
              </tr>
            </thead>
            <tbody>
              {files.map((file) => (
                <tr key={`${file.file_index}-${file.file_name}`}>
                  <td>{file.file_index}</td>
                  <td>
                    {file.file_name}
                    {file.is_pars ? <span className="pill tone-fair">PAR2</span> : null}
                  </td>
                  <td>{formatBytes(file.size_bytes)}</td>
                  <td>
                    {file.observed_parts} / {file.total_parts}
                  </td>
                  <td>{formatDateTime(file.posted_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
