import {createRoot} from 'react-dom/client'
import {App} from './app'
import './style.css'

const root=document.getElementById('root')
if(!root)throw new Error('Constructor root element is missing')

createRoot(root).render(<App/>)
