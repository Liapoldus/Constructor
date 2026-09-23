import type {ReactNode} from 'react'

export default function ShellLayout({children}: {children: ReactNode}) {
  return <div className="site-shell"><header>Liapoldus Site</header>{children}</div>
}
