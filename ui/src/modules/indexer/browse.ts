import type { PublicReleaseSummary } from '../../shared/types'

export type BrowseCategory = {
  slug: string
  label: string
  description: string
  subcategories: Array<{ slug: string; label: string }>
}

export const browseCategories: BrowseCategory[] = [
  {
    slug: 'movies',
    label: 'Movies',
    description: 'Feature films and movie releases.',
    subcategories: [
      { slug: 'all', label: 'All Movies' },
      { slug: 'sd', label: 'SD' },
      { slug: 'hd', label: 'HD' },
      { slug: 'uhd', label: 'UHD' },
    ],
  },
  {
    slug: 'tv',
    label: 'TV',
    description: 'Television episodes and season packs.',
    subcategories: [
      { slug: 'all', label: 'All TV' },
      { slug: 'sd', label: 'SD' },
      { slug: 'hd', label: 'HD' },
      { slug: 'uhd', label: 'UHD' },
      { slug: 'anime', label: 'Anime' },
    ],
  },
  {
    slug: 'console',
    label: 'Console',
    description: 'Console game releases.',
    subcategories: [
      { slug: 'all', label: 'All Console' },
      { slug: 'playstation', label: 'PlayStation' },
      { slug: 'xbox', label: 'Xbox' },
      { slug: 'nintendo', label: 'Nintendo' },
    ],
  },
  {
    slug: 'audio',
    label: 'Audio',
    description: 'Music and other audio releases.',
    subcategories: [
      { slug: 'all', label: 'All Audio' },
      { slug: 'mp3', label: 'MP3' },
      { slug: 'flac', label: 'FLAC' },
    ],
  },
  {
    slug: 'pc',
    label: 'PC',
    description: 'PC games and software.',
    subcategories: [
      { slug: 'all', label: 'All PC' },
      { slug: 'games', label: 'Games' },
      { slug: 'apps', label: 'Apps' },
    ],
  },
  {
    slug: 'books',
    label: 'Books',
    description: 'Ebooks, comics, and audiobooks.',
    subcategories: [
      { slug: 'all', label: 'All Books' },
      { slug: 'comics', label: 'Comics' },
      { slug: 'audiobook', label: 'Audiobooks' },
    ],
  },
  {
    slug: 'xxx',
    label: 'XXX',
    description: 'Adult releases.',
    subcategories: [
      { slug: 'all', label: 'All XXX' },
      { slug: 'video', label: 'Video' },
      { slug: 'images', label: 'Images' },
    ],
  },
]

export function findBrowseCategory(slug?: string) {
  return browseCategories.find((category) => category.slug === slug)
}

export function releaseCategoryLabel(release: Pick<PublicReleaseSummary, 'category' | 'classification' | 'external_media_type'>) {
  const raw = (release.category || '').trim()
  if (raw && raw.toLowerCase() !== 'usenet') {
    return raw.replaceAll('_', ' ')
  }
  if (release.external_media_type) {
    return release.external_media_type
  }
  if (release.classification) {
    return release.classification.replaceAll('_', ' ')
  }
  return 'uncategorized'
}

export function simpleSceneName(title: string) {
  return title
    .trim()
    .replace(/[^\w.+-]+/g, '.')
    .replace(/\.{2,}/g, '.')
}
