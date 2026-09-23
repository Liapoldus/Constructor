import test from 'node:test'
import assert from 'node:assert/strict'
import {disableSiteLocale,enableSiteLocale} from '../src/site-locales.ts'

test('Site locale additions normalize input, preserve order and reject duplicates',()=>{
  assert.deepEqual(enableSiteLocale(['ru-RU'],' en-US '),['ru-RU','en-US'])
  assert.throws(()=>enableSiteLocale(['en-US'],'en-US'),/already enabled/)
  assert.throws(()=>enableSiteLocale([],'en-us'),/language-region form/)
})

test('disabling a locale preserves the remaining order and always keeps one enabled',()=>{
  assert.deepEqual(disableSiteLocale(['en-US','ru-RU','fr-FR'],'ru-RU'),['en-US','fr-FR'])
  assert.throws(()=>disableSiteLocale(['en-US'],'en-US'),/at least one/)
  assert.throws(()=>disableSiteLocale(['en-US'],'ru-RU'),/not enabled/)
})
