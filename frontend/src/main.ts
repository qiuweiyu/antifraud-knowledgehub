import '@unocss/reset/tailwind.css'
import 'uno.css'
import 'element-plus/dist/index.css'
import './styles/app.css'

import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './App.vue'
import router from './router'

createApp(App).use(createPinia()).use(router).mount('#app')
