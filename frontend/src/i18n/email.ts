// The texts of the notification e-mails (notifications spec). The server writes them (internal/notify, whose list
// must match this one); their translations live in the catalogs with every other string, and the build hands the
// server those translations as i18n/email.json (vite.config.ts).
export const emailStrings = [
  '{name} wrote to you in MeetHub',
  '{n} new messages for you in MeetHub',
  'Messages for you while you were away',
  '{name} in {place}',
  'a direct message',
  'a group conversation',
  'And {n} more.',
  'Open in MeetHub',
  'You receive this e-mail because you were sent a direct message or mentioned while away.',
  'Turn off these e-mails',
] as const
