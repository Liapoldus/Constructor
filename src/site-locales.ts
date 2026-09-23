const localeIDPattern=/^[a-z]{2}(-[A-Z]{2})?$/

export function enableSiteLocale(locales:readonly string[], input:string):string[] {
  const locale=input.trim()
  if(!localeIDPattern.test(locale))throw new Error('Locale must use language or language-region form, such as en or en-US.')
  if(locales.includes(locale))throw new Error(`Locale ${locale} is already enabled.`)
  return [...locales,locale]
}

export function disableSiteLocale(locales:readonly string[], locale:string):string[] {
  if(!locales.includes(locale))throw new Error(`Locale ${locale} is not enabled.`)
  if(locales.length<=1)throw new Error('A Site must keep at least one enabled locale.')
  return locales.filter(value=>value!==locale)
}
