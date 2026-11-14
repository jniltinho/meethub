// The side panel's state lives in the layout; the open channel's header shows its toggle.
import type { ComputedRef, InjectionKey } from 'vue'

export const sidePanelKey: InjectionKey<{ visible: ComputedRef<boolean>; toggle: () => void }> = Symbol('sidePanel')
