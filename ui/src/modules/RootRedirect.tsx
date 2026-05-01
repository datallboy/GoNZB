import { useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import { getCapabilities } from '../shared/api/settings'
import { useAuth } from '../shared/auth/AuthContext'
import type { ControlPlaneCapabilities } from '../shared/types'

export function RootRedirect() {
  const { hasPermission } = useAuth()
  const [target, setTarget] = useState<string | null>(null)

  useEffect(() => {
    if (!hasPermission('admin.settings.read')) {
      setTarget(hasPermission('indexer.releases.read') ? '/indexer/releases' : '/admin')
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
  }, [hasPermission])

  if (!target) return null
  return <Navigate to={target} replace />
}
