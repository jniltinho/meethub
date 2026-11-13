// A polite live region for chat announcements (channel opened, new message), shared by the whole app.
import { ref } from 'vue'

export const announcement = ref('')

export function announce(text: string): void {
  // clear first so repeating the same text is announced again
  announcement.value = ''
  setTimeout(() => (announcement.value = text), 50)
}
