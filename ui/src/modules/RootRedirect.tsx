import { useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import { getCapabilities } from '../shared/api/settings'
import { apiRequest } from '../shared/api/http'
import { useAuth } from '../shared/auth/useAuth'
import type { ControlPlaneCapabilities, HealthResponse } from '../shared/types'

export function RootRedirect() {
  const { hasPermission } = useAuth()
  const canReadSettings = hasPermission('admin.settings.read')
  const [target, setTarget] = useState<string | null>(null)

  useEffect(() => {
    const uploaderFallback = () => apiRequest<HealthResponse>('/healthz')
      .then((health) => {
        if (health.modules.uploader?.enabled && hasPermission('uploader.submissions.read')) {
          setTarget('/uploader')
          return
        }
        setTarget(hasPermission('indexer.releases.read') ? '/indexer/releases' : '/admin')
      })
      .catch(() => setTarget(hasPermission('indexer.releases.read') ? '/indexer/releases' : '/admin'))

    if (!canReadSettings) {
      void uploaderFallback()
      return
    }
    void getCapabilities()
      .then((response) => {
        const caps = response as ControlPlaneCapabilities
        if (caps.modules.usenet_indexer?.ready) {
          setTarget('/indexer/releases')
        } else if (caps.modules.uploader?.ready && hasPermission('uploader.submissions.read')) {
          setTarget('/uploader')
        } else if (caps.modules.aggregator?.visible || caps.modules.gonzbnet?.visible) {
          setTarget('/admin')
        } else {
          setTarget('/admin')
        }
      })
      .catch(() => void uploaderFallback())
  }, [canReadSettings, hasPermission])

  if (!target) return null
  return <Navigate to={target} replace />
}
