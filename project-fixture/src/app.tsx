import {createRoot} from 'react-dom/client'
import {createBrowserRouter, RouterProvider} from 'react-router-dom'
import {createRoutes} from './generated/routes'
import './generated/theme.css'
import {PreviewRuntimeProvider} from './vendor/liapoldus/react/index'
import initialContent from './generated/content/ru-RU.json'
import React from 'react'

createRoot(document.getElementById('root')!).render(<PreviewRuntimeProvider initialContent={initialContent}><RouterProvider router={createBrowserRouter(createRoutes(() => false))}/></PreviewRuntimeProvider>)
