import {useState} from 'react'
import type {AssetItem} from './api'
import {assetCategoryLabel, availableAssetCategories, filterAssets, type AssetCategory} from './asset-picker'

type AssetReference = {id: string; alt?: string}

type AssetFieldProps = {
  assets: AssetItem[]
  error: string
  fieldId: string
  fieldLabel: string
  fieldType: 'image' | 'icon' | 'file'
  invalid?: boolean
  nullable?: boolean
  value?: AssetReference | null
  onUpload: (file: File) => Promise<AssetItem>
  onChange: (value: {kind: 'asset'; id: string; alt?: string} | null) => void
}

export function AssetField({assets, error, fieldId, fieldLabel, fieldType, invalid, nullable, value, onUpload, onChange}: AssetFieldProps) {
  const [query, setQuery] = useState('')
  const [category, setCategory] = useState<AssetCategory>('all')
  const [uploading, setUploading] = useState(false)
  const [uploadMessage, setUploadMessage] = useState('')
  const compatibleAssets = assets.filter(asset => fieldType === 'file' || asset.type === fieldType)
  const categories = availableAssetCategories(compatibleAssets)
  const visibleAssets = filterAssets(compatibleAssets, query, category)
  const selectedAsset = compatibleAssets.find(asset => asset.id === value?.id)
  const selectionVisible = visibleAssets.some(asset => asset.id === value?.id)

  const selectAsset = (id: string) => {
    if (id === '__none') {
      onChange(null)
      return
    }
    const asset = compatibleAssets.find(item => item.id === id)
    if (asset) onChange({kind: 'asset', id: asset.id, ...(value?.alt ? {alt: value.alt} : {})})
  }

  const upload = async (file?: File) => {
    if (!file || uploading) return
    setUploading(true)
    setUploadMessage('')
    try {
      const asset=await onUpload(file)
      onChange({kind:'asset',id:asset.id,...(value?.alt?{alt:value.alt}:{})})
      setUploadMessage(`Added ${asset.id}`)
    } catch (uploadError) {
      setUploadMessage(uploadError instanceof Error?uploadError.message:'Asset upload failed')
    } finally {
      setUploading(false)
    }
  }

  return (
    <div className="asset-field">
      <input
        type="search"
        aria-label={`${fieldLabel} asset search`}
        placeholder="Search assets by ID, path or format"
        value={query}
        onChange={event => setQuery(event.target.value)}
        disabled={Boolean(error) || compatibleAssets.length === 0}
      />
      {categories.length > 1 && (
        <select
          aria-label={`${fieldLabel} asset category`}
          value={category}
          onChange={event => setCategory(event.target.value as AssetCategory)}
        >
          <option value="all">All categories</option>
          {categories.map(item => <option key={item} value={item}>{assetCategoryLabel(item)}</option>)}
        </select>
      )}
      <select
        id={fieldId}
        aria-invalid={invalid}
        aria-label={`${fieldLabel} asset`}
        value={value?.id ?? (nullable && value === null ? '__none' : '')}
        disabled={Boolean(error) || compatibleAssets.length === 0}
        onChange={event => selectAsset(event.target.value)}
      >
        <option value="" disabled={!nullable}>Select an asset…</option>
        {nullable && <option value="__none">No asset</option>}
        {value && !selectedAsset && <option value={value.id}>Missing: {value.id}</option>}
        {selectedAsset && !selectionVisible && <option value={selectedAsset.id}>Current selection · {selectedAsset.id}</option>}
        {visibleAssets.map(asset => <option key={asset.id} value={asset.id}>{asset.id} · {asset.path}</option>)}
      </select>
      <input
        type="file"
        aria-label={`${fieldLabel} upload image`}
        accept={fieldType==='icon'?'image/svg+xml,.svg':fieldType==='image'?'image/png,image/jpeg,image/webp,image/gif,.png,.jpg,.jpeg,.webp,.gif':'image/png,image/jpeg,image/webp,image/gif,image/svg+xml,.png,.jpg,.jpeg,.webp,.gif,.svg'}
        disabled={Boolean(error)||uploading}
        onChange={event=>{void upload(event.target.files?.[0]);event.currentTarget.value=''}}
      />
      {uploading&&<small role="status">Uploading…</small>}
      {uploadMessage&&<small role="status">{uploadMessage}</small>}
      {error
        ? <small role="status">{error}</small>
        : !compatibleAssets.length
          ? <small role="status">No registered {fieldType} assets in this project.</small>
          : !visibleAssets.length
            ? <small role="status">No assets match this search/category.</small>
            : null}
      {selectedAsset && fieldType === 'image' && (
        <img
          src={`/${selectedAsset.path.replace(/^public\//, '')}`}
          alt={value?.alt ?? ''}
          loading="lazy"
          style={{display:'block',width:'100%',maxHeight:180,marginTop:8,border:'1px solid #414b5d',borderRadius:5,objectFit:'contain',background:'#11151d'}}
        />
      )}
      {fieldType === 'image' && (
        <input
          aria-label={`${fieldLabel} alternative text`}
          type="text"
          placeholder="Alternative text"
          value={value?.alt ?? ''}
          onChange={event => value && onChange({kind:'asset',id:value.id,alt:event.target.value})}
        />
      )}
    </div>
  )
}
