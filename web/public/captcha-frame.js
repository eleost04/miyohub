/* An opaque sandbox receives only public challenge parameters, never sessions
   or account credentials. The main page never loads third-party scripts. */
(() => {
  const parentOrigin = new URL(location.href).origin
  let id = '', widget, started = false, timer
  const send = (type, data = {}) => parent.postMessage({ type: 'miyohub:captcha:' + type, id, ...data }, parentOrigin)
  const resize = () => {
    const root = document.getElementById('scale')
    const scale = Math.min(1, innerWidth / 300)
    root.style.transform = 'scale(' + scale + ')'
    root.style.marginLeft = Math.max(0, (innerWidth - 300) / 2) + 'px'
    send('height', { height: Math.ceil(root.scrollHeight * scale) + 12 })
  }
  addEventListener('resize', resize)
  new ResizeObserver(resize).observe(document.getElementById('scale'))
  addEventListener('message', event => {
    const data = event.data
    if (event.source !== parent || event.origin !== parentOrigin || started || data?.type !== 'miyohub:captcha:init' || typeof data.id !== 'string' || typeof data.gt !== 'string' || typeof data.challenge !== 'string') return
    started = true; id = data.id
    const script = document.createElement('script')
    script.src = 'https://static.geetest.com/static/tools/gt.js'
    script.referrerPolicy = 'no-referrer'
    const fail = () => { clearTimeout(timer); send('error') }
    script.onerror = fail
    script.onload = () => {
      if (typeof window.initGeetest !== 'function') { fail(); return }
      window.initGeetest({ gt: data.gt, challenge: data.challenge, new_captcha: data.new_captcha !== false, offline: false, product: 'embed', width: '300px', lang: 'zho', https: true }, instance => {
        widget = instance
        widget.appendTo('#captcha')
        widget.onReady(() => { clearTimeout(timer); resize(); send('loaded') })
        widget.onError(fail)
        widget.onSuccess(() => {
          const result = widget.getValidate()
          if (result && typeof result.geetest_challenge === 'string' && typeof result.geetest_validate === 'string') send('solution', { challenge: result.geetest_challenge, validate: result.geetest_validate })
        })
      })
    }
    timer = setTimeout(fail, 20000)
    document.head.append(script)
  })
  addEventListener('pagehide', () => { clearTimeout(timer); widget?.destroy?.() })
  send('ready')
})()
