import test from 'node:test'
import assert from 'node:assert/strict'
import {assetCategoryLabel,availableAssetCategories,filterAssets} from '../src/asset-picker.ts'

const assets=[
  {id:'brand-mark',type:'image',path:'public/assets/brand/mark.png',mimeType:'image/png',size:12,sha256:'a'.repeat(64)},
  {id:'arrow-right',type:'icon',path:'public/assets/icons/arrow.svg',mimeType:'image/svg+xml',size:18,sha256:'b'.repeat(64)},
  {id:'body-font',type:'font',path:'public/assets/type/body.woff2',mimeType:'font/woff2',size:32,sha256:'c'.repeat(64)},
  {id:'guide',type:'document',path:'public/assets/docs/guide.pdf',mimeType:'application/pdf',size:64,sha256:'d'.repeat(64)},
]

test('asset picker searches IDs, paths, formats and type without changing catalog order',()=>{
  assert.deepEqual(filterAssets(assets,' BRAND ').map(asset=>asset.id),['brand-mark'])
  assert.deepEqual(filterAssets(assets,'icons/arrow').map(asset=>asset.id),['arrow-right'])
  assert.deepEqual(filterAssets(assets,'woff2').map(asset=>asset.id),['body-font'])
  assert.deepEqual(filterAssets(assets,'document').map(asset=>asset.id),['guide'])
  assert.deepEqual(filterAssets(assets,'').map(asset=>asset.id),assets.map(asset=>asset.id))
})

test('asset categories are available types in canonical order and filter independently',()=>{
  assert.deepEqual(availableAssetCategories(assets),['image','icon','font','document'])
  assert.equal(assetCategoryLabel('image'),'Images')
  assert.deepEqual(filterAssets(assets,'','icon').map(asset=>asset.id),['arrow-right'])
  assert.deepEqual(filterAssets(assets,'PDF','document').map(asset=>asset.id),['guide'])
  assert.deepEqual(filterAssets(assets,'PDF','image'),[])
})
