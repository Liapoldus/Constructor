import {defineConfig, loadEnv} from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig(({mode}) => {
  const env=loadEnv(mode, '.', '')
  const apiTarget=env.CONSTRUCTOR_API_PROXY || 'http://127.0.0.1:8787'
  return {plugins:[react()],server:{proxy:{'/api':apiTarget}}}
})
