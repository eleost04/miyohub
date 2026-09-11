import { createApp } from 'vue'
import App from './App.vue'
import './styles.css'

const ua = navigator.userAgent.toLowerCase()
const device = /android/.test(ua) ? 'android' : /iphone|ipad/.test(ua) ? 'ios' : 'desktop'
document.documentElement.dataset.device = device

createApp(App).mount('#app')
