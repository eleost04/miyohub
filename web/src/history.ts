import { nextTick } from 'vue'

type Entry = { page: string; layers: string[]; session: string }
const session = crypto.randomUUID()
const layers = new Map<string, () => void>()
let current: Entry = { page: location.hash.slice(1) || 'dashboard', layers: [], session }
let queue = Promise.resolve()
let internalPop: ((entry: Entry) => void) | undefined
let route: (page: string) => boolean = () => true

function read(state: { miyohub?: Entry } | null): Entry {
  const entry = state?.miyohub
  return { page: entry?.page || location.hash.slice(1) || 'dashboard', layers: entry?.session === session ? entry.layers : [], session }
}
function write(entry: Entry, push: boolean) {
  current = entry
  history[push ? 'pushState' : 'replaceState']({ miyohub: entry }, '', location.pathname + location.search + '#' + entry.page)
}
function enqueue(action: () => void | Promise<void>) {
  queue = queue.then(action)
  return queue
}
async function trimClosedLayers() {
  let count = 0
  for (let i = current.layers.length - 1; i >= 0 && !layers.has(current.layers[i]!); i--) count++
  if (!count) return
  await new Promise<void>(resolve => {
    internalPop = entry => { current = entry; resolve() }
    history.go(-count)
  })
}
function popped(event: PopStateEvent) {
  const target = read(event.state)
  if (internalPop) { const finish = internalPop; internalPop = undefined; finish(target); return }
  void enqueue(async () => {
    const previous = current
    current = target
    const removed = [...previous.layers].reverse().find(id => layers.has(id) && !target.layers.includes(id))
    if (removed) {
      layers.get(removed)?.()
      await nextTick()
      // A busy editor or unsaved-change confirmation can decline to close.
      const remaining = previous.layers.filter(id => layers.has(id))
      if (remaining.some(id => !target.layers.includes(id))) write({ ...previous, layers: remaining }, true)
      return
    }
    if (target.page !== previous.page && !route(target.page)) {
      write({ ...previous, layers: previous.layers.filter(id => layers.has(id)) }, true)
      return
    }
    // Forward navigation must not resurrect an already dismissed modal.
    write({ ...target, layers: target.layers.filter(id => layers.has(id)) }, false)
  })
}

export function initHistory(onRoute: (page: string) => boolean) {
  route = onRoute
  write(current, false)
  window.addEventListener('popstate', popped)
  return () => window.removeEventListener('popstate', popped)
}

export function registerBackLayer(close: () => void) {
  const id = crypto.randomUUID()
  layers.set(id, close)
  void enqueue(() => { if (layers.has(id)) write({ ...current, layers: [...current.layers, id] }, true) })
  return () => { layers.delete(id); void enqueue(trimClosedLayers) }
}

export async function changePage(page: string, replace = false) {
  await nextTick() // Let closing dialogs unregister before changing the route.
  await enqueue(async () => {
    await trimClosedLayers()
    if (current.page !== page || replace) write({ page, layers: [], session }, !replace)
  })
}
