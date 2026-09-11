import { computed, shallowRef, type InjectionKey } from 'vue'

let locks = 0
let originalOverflow = ''
const owners = shallowRef<symbol[]>([])
export const overlayOwner: InjectionKey<symbol> = Symbol('overlay-owner')
export const topOverlay = computed(() => owners.value[owners.value.length - 1])

// Shared by modal dialogs and teleported selectors. Closing a selector inside
// a dialog must not unlock the page, or restore an obsolete scroll-lock value.
export function lockPageScroll(owner = Symbol('overlay')) {
  if (!locks++) { originalOverflow = document.body.style.overflow; document.body.style.overflow = 'hidden' }
  owners.value = [...owners.value, owner]
  let released = false
  return () => {
    if (released) return
    released = true
    owners.value = owners.value.filter(item => item !== owner)
    if (--locks === 0) document.body.style.overflow = originalOverflow
  }
}
