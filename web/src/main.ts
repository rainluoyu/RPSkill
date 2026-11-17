import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { consumeFragment } from './lib/api'
import { useWorkbenchStore } from './stores/workbench'
import './style.css'

let token = consumeFragment()
const pinia = createPinia()
createApp(App).use(pinia).mount('#app')
void useWorkbenchStore(pinia).bootstrap(token)
token = ''

function fragmentLogin() {
  const fragmentToken = consumeFragment()
  if (fragmentToken) void useWorkbenchStore(pinia).bootstrap(fragmentToken)
}
window.addEventListener('hashchange', fragmentLogin)
if (import.meta.hot) import.meta.hot.dispose(() => window.removeEventListener('hashchange', fragmentLogin))
