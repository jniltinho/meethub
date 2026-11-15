// A fake OpenAI-compatible AI service for the browser tests only (make test-e2e, add-meeting-summaries): timed
// transcription that answers one sentence for any audio, and chat completions that answer the connection test and a
// summary carrying hostile parts (the page must keep them inert). GET /stats says what it received; nothing of it
// is a real model.
import { createServer } from 'node:http'

const KEY = process.env.AI_KEY ?? 'e2e-throwaway-ai-key'
const stats = { transcriptions: 0, chats: 0, pixel: 0, unauthorized: 0 }

const summary = [
  '## Summary',
  'The team agreed to ship the release on Friday. <script>window.__pwned = 1</script>',
  '![tracker](http://ai:8090/pixel.png) @all',
  '',
  '## Decisions',
  '- Ship on Friday.',
].join('\n')

function json(res, status, body) {
  res.writeHead(status, { 'Content-Type': 'application/json' })
  res.end(JSON.stringify(body))
}

createServer(async (req, res) => {
  let body = ''
  for await (const chunk of req) body += chunk
  const url = new URL(req.url, 'http://ai')
  if (req.method === 'GET' && url.pathname === '/stats') return json(res, 200, stats)
  if (req.method === 'GET' && url.pathname === '/pixel.png') {
    stats.pixel++
    res.writeHead(404)
    return res.end()
  }
  if (req.headers.authorization !== `Bearer ${KEY}`) {
    stats.unauthorized++
    return json(res, 401, { error: { message: 'invalid key' } })
  }
  if (req.method === 'POST' && url.pathname === '/v1/audio/transcriptions') {
    stats.transcriptions++
    return json(res, 200, {
      text: 'Let us ship on Friday.',
      segments: [{ id: 0, start: 0.2, end: 1.6, text: ' Let us ship on Friday.' }],
    })
  }
  if (req.method === 'POST' && url.pathname === '/v1/chat/completions') {
    stats.chats++
    const msgs = JSON.parse(body || '{}').messages ?? []
    const user = msgs.find((m) => m.role === 'user')?.content ?? ''
    const content = user === 'ok' ? 'ok' : summary
    return json(res, 200, { choices: [{ finish_reason: 'stop', message: { role: 'assistant', content } }] })
  }
  json(res, 404, { error: 'not found' })
}).listen(8090, '0.0.0.0')
