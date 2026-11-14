// A throwaway OpenID Connect provider for the browser tests only (make test-e2e): discovery, keys, a login page that
// asks for the e-mail to assert (and whether it is verified), and a token endpoint that checks the client secret and
// PKCE. Never a real provider: everything lives in memory, the TLS certificate is made at start by the container.
import { createHash, createSign, generateKeyPairSync, randomBytes } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:https'

const ISSUER = process.env.IDP_ISSUER ?? 'https://idp:9443'
const CLIENT_ID = process.env.IDP_CLIENT_ID ?? 'meethub-e2e'
const CLIENT_SECRET = process.env.IDP_CLIENT_SECRET ?? 'e2e-throwaway-client-secret'
const { privateKey, publicKey } = generateKeyPairSync('rsa', { modulusLength: 2048 })
const jwk = { ...publicKey.export({ format: 'jwk' }), kid: 'e2e', alg: 'RS256', use: 'sig' }
const codes = new Map() // code → what the token endpoint answers

const b64 = (v) => Buffer.from(typeof v === 'string' ? v : JSON.stringify(v)).toString('base64url')
function sign(claims) {
  const head = b64({ alg: 'RS256', typ: 'JWT', kid: 'e2e' }) + '.' + b64(claims)
  return head + '.' + createSign('RSA-SHA256').update(head).sign(privateKey).toString('base64url')
}
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`)

function json(res, status, body) {
  res.writeHead(status, { 'Content-Type': 'application/json' })
  res.end(JSON.stringify(body))
}

async function form(req) {
  let body = ''
  for await (const chunk of req) body += chunk
  return new URLSearchParams(body)
}

const server = createServer(
  { key: readFileSync('/idp/key.pem'), cert: readFileSync('/idp/cert.pem') },
  async (req, res) => {
    const url = new URL(req.url, ISSUER)
    if (url.pathname === '/.well-known/openid-configuration') {
      return json(res, 200, {
        issuer: ISSUER,
        authorization_endpoint: ISSUER + '/auth',
        token_endpoint: ISSUER + '/token',
        jwks_uri: ISSUER + '/keys',
        response_types_supported: ['code'],
        subject_types_supported: ['public'],
        id_token_signing_alg_values_supported: ['RS256'],
        code_challenge_methods_supported: ['S256'],
      })
    }
    if (url.pathname === '/keys') return json(res, 200, { keys: [jwk] })
    if (url.pathname === '/auth' && req.method === 'GET') {
      const q = url.searchParams
      if (q.get('client_id') !== CLIENT_ID || q.get('code_challenge_method') !== 'S256') {
        res.writeHead(400)
        return res.end('bad request')
      }
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
      return res.end(`<!doctype html><title>Test provider</title><h1>Test provider</h1>
<form method="post" action="/auth">
${['redirect_uri', 'state', 'nonce', 'code_challenge'].map((k) => `<input type="hidden" name="${k}" value="${esc(q.get(k) ?? '')}">`).join('')}
<label>E-mail <input name="email" type="email"></label>
<label><input name="verified" type="checkbox" checked> Verified</label>
<button>Continue</button></form>`)
    }
    if (url.pathname === '/auth' && req.method === 'POST') {
      const f = await form(req)
      const code = randomBytes(16).toString('hex')
      codes.set(code, {
        email: f.get('email'),
        verified: f.get('verified') === 'on',
        nonce: f.get('nonce'),
        challenge: f.get('code_challenge'),
      })
      const back = new URL(f.get('redirect_uri'))
      back.searchParams.set('code', code)
      back.searchParams.set('state', f.get('state'))
      res.writeHead(302, { Location: back.toString() })
      return res.end()
    }
    if (url.pathname === '/token' && req.method === 'POST') {
      const f = await form(req)
      const basic = Buffer.from((req.headers.authorization ?? '').replace(/^Basic /, ''), 'base64').toString()
      const [id, secret] = basic
        ? basic.split(':').map(decodeURIComponent)
        : [f.get('client_id'), f.get('client_secret')]
      const c = codes.get(f.get('code') ?? '')
      codes.delete(f.get('code') ?? '')
      const verifier = createHash('sha256')
        .update(f.get('code_verifier') ?? '')
        .digest('base64url')
      if (id !== CLIENT_ID || secret !== CLIENT_SECRET || !c || verifier !== c.challenge) {
        return json(res, 400, { error: 'invalid_grant' })
      }
      const now = Math.floor(Date.now() / 1000)
      return json(res, 200, {
        access_token: randomBytes(16).toString('hex'),
        token_type: 'Bearer',
        expires_in: 300,
        id_token: sign({
          iss: ISSUER,
          aud: CLIENT_ID,
          sub: 'e2e-' + c.email,
          email: c.email,
          email_verified: c.verified,
          name: c.email.split('@')[0],
          nonce: c.nonce,
          iat: now,
          exp: now + 300,
        }),
      })
    }
    res.writeHead(404)
    res.end('not found')
  },
)
server.listen(9443, () => writeFileSync('/idp/ready', 'ok'))
