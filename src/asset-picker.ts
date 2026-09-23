import type {AssetItem, AssetType} from './api'

export type AssetCategory = 'all' | AssetType

const categoryOrder: readonly AssetType[] = ['image', 'icon', 'font', 'video', 'document', 'other']

export function availableAssetCategories(assets: readonly AssetItem[]): AssetType[] {
  const available = new Set(assets.map(asset => asset.type))
  return categoryOrder.filter(category => available.has(category))
}

export function filterAssets(
  assets: readonly AssetItem[],
  query: string,
  category: AssetCategory = 'all',
): AssetItem[] {
  const normalizedQuery = query.trim().toLocaleLowerCase()
  return assets.filter(asset => {
    if (category !== 'all' && asset.type !== category) return false
    if (!normalizedQuery) return true
    return [asset.id, asset.path, asset.mimeType, asset.type]
      .some(value => value.toLocaleLowerCase().includes(normalizedQuery))
  })
}

export function assetCategoryLabel(category: AssetType): string {
  const labels: Record<AssetType, string> = {
    image: 'Images',
    icon: 'Icons',
    font: 'Fonts',
    video: 'Videos',
    document: 'Documents',
    other: 'Other',
  }
  return labels[category]
}
