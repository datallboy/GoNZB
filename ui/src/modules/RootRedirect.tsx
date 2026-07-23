import { useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import { getCapabilities } from '../shared/api/settings'
import { useAuth } from '../shared/auth/useAuth'
import type { ControlPlaneCapabilities } from '../shared/types'

export function RootRedirect() {
  const { hasPermission } = useAuth()
  const canReadSettings = hasPermission('admin.settings.read')
  const [target, setTarget] = useState<string | null>(null)

  useEffect(() => {
    if (!canReadSettings) {
      return
    }
    void getCapabilities()
      .then((response) => {
        const caps = response as ControlPlaneCapabilities
        if (caps.modules.usenet_indexer?.ready) {
          setTarget('/indexer/releases')
        } else if (caps.modules.downloader?.visible || caps.modules.aggregator?.visible) {
          setTarget('/admin')
        } else {
          setTarget('/admin')
        }
      })
      .catch(() => setTarget('/admin'))
  }, [canReadSettings])

  if (!canReadSettings) {
    return <Navigate to={hasPermission('indexer.releases.read') ? '/indexer/releases' : '/admin'} replace />
  }
  if (!target) return null
  return <Navigate to={target} replace />
}
