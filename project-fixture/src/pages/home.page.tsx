import {PreviewInstance,usePreviewContent} from '../vendor/liapoldus/react/index'

export default function HomePage(){const title=usePreviewContent<string>('home','hero-main','title','Untitled');const subtitle=usePreviewContent<string>('home','hero-main','subtitle','');const label=usePreviewContent<string>('home','hero-main','label','');return <PreviewInstance instanceId="hero-main"><main><h1>{title}</h1><p>{subtitle}</p><button>{label}</button></main></PreviewInstance>}
