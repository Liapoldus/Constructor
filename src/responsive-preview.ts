export const previewViewports = {
  desktop: {label: 'Desktop', width: 1440},
  tablet: {label: 'Tablet', width: 768},
  mobile: {label: 'Mobile', width: 390},
} as const

export type PreviewViewport = keyof typeof previewViewports

export function previewViewportStyle(viewport: PreviewViewport): {width: string; maxWidth: string} {
  return {
    width: `min(${previewViewports[viewport].width}px, 100%)`,
    maxWidth: '100%',
  }
}
