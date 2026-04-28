export function formatBytes(value: number) {
  if (!value) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = value
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex += 1
  }
  return `${size.toFixed(size >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`
}

export function formatDateTime(value?: string | null) {
  if (!value) return 'Unknown'
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value))
}

export function formatPercent(value: number) {
  return `${Math.floor(value)}%`
}

export function formatRuntime(seconds: number) {
  if (!seconds) return 'Unknown'
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return `${hours}h ${minutes}m`
}

export function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value)
}

export function formatRelativeAge(value?: string | null) {
  if (!value) return 'Unknown'
  const then = new Date(value).getTime()
  const now = Date.now()
  const delta = Math.max(0, now - then)
  const minute = 60 * 1000
  const hour = 60 * minute
  const day = 24 * hour

  if (delta < hour) {
    return `${Math.max(1, Math.floor(delta / minute))}m`
  }
  if (delta < day) {
    return `${Math.floor(delta / hour)}h`
  }
  if (delta < 30 * day) {
    return `${Math.floor(delta / day)}d`
  }
  return `${Math.floor(delta / (30 * day))}mo`
}
