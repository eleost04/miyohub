/* An opaque sandbox receives public challenge parameters (including V4's
   short-lived risk session), never account/site login credentials. Third-party
   scripts are loaded only for the active widget, not on the main page. */
(() => {
  const parentOrigin = new URL(location.href).origin
  let id = '', widget, started = false, timer, version = 3
  const send = (type, data = {}) => parent.postMessage({ type: 'miyohub:captcha:' + type, id, ...data }, parentOrigin)
  const resize = () => {
    const root = document.getElementById('scale')
    const scale = Math.min(1, innerWidth / 300)
    root.style.transform = 'scale(' + scale + ')'
    root.style.marginLeft = Math.max(0, (innerWidth - 300) / 2) + 'px'
    send('height', { height: Math.max(version === 4 ? 520 : 0, Math.ceil(root.scrollHeight * scale) + 12) })
  }
  addEventListener('resize', resize)
  new ResizeObserver(resize).observe(document.getElementById('scale'))
  addEventListener('message', event => {
    const data = event.data
    if (event.source !== parent || event.origin !== parentOrigin || started || data?.type !== 'miyohub:captcha:init' || typeof data.id !== 'string' || typeof data.gt !== 'string') return
    version = data.version === 4 ? 4 : 3
    if (version === 3 && typeof data.challenge !== 'string' || version === 4 && typeof data.session_id !== 'string') return
    started = true; id = data.id
    const script = document.createElement('script')
    script.src = version === 4 ? 'https://static.geetest.com/v4/gt4.js' : 'https://static.geetest.com/static/tools/gt.js'
    script.referrerPolicy = 'no-referrer'
    const fail = () => { clearTimeout(timer); send('error') }
    script.onerror = fail
    script.onload = () => {
      const initialize = version === 4 ? window.initGeetest4 : window.initGeetest
      if (typeof initialize !== 'function') { fail(); return }
      const options = version === 4
        ? { captchaId: data.gt, riskType: data.risk_type || undefined, product: 'popup', nextWidth: Math.max(220, Math.min(300, innerWidth - 16)) + 'px', lang: 'zho', userInfo: JSON.stringify({ session_id: data.session_id }), https: true, protocol: 'https' }
        : { gt: data.gt, challenge: data.challenge, new_captcha: data.new_captcha !== false, offline: false, product: 'embed', width: '300px', lang: 'zho', https: true }
      initialize(options, instance => {
        widget = instance
        widget.appendTo('#captcha')
        widget.onReady(() => { clearTimeout(timer); resize(); send('loaded') })
        widget.onError(fail)
        widget.onSuccess(() => {
          const result = widget.getValidate()
          if (!result) return
          if (version === 4) {
            const generatedAt = typeof result.gen_time === 'number' && Number.isSafeInteger(result.gen_time) ? String(result.gen_time) : result.gen_time
            if (typeof result.captcha_id === 'string' && typeof result.lot_number === 'string' && typeof result.captcha_output === 'string' && typeof result.pass_token === 'string' && typeof generatedAt === 'string') send('solution', { captcha_id: result.captcha_id, lot_number: result.lot_number, captcha_output: result.captcha_output, pass_token: result.pass_token, gen_time: generatedAt })
          } else if (typeof result.geetest_challenge === 'string' && typeof result.geetest_validate === 'string') send('solution', { challenge: result.geetest_challenge, validate: result.geetest_validate })
        })
      })
    }
    timer = setTimeout(fail, 20000)
    document.head.append(script)
  })
  addEventListener('pagehide', () => { clearTimeout(timer); widget?.destroy?.() })
  send('ready')
})()
