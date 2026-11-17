import { describe, expect, it } from 'vitest'
import { addressProblem, blocked, canGo, passwordProblem, STEPS, tomlLines } from '../../src/views/setupRules'

describe('setup wizard rules', () => {
  it('has seven steps, the account third', () => {
    expect(STEPS).toHaveLength(7)
    expect(STEPS[2]).toBe('account')
  })

  it('only a problem blocks the account step', () => {
    expect(
      blocked([
        { id: 'email', level: 'note' },
        { id: 'https', level: 'warning' },
      ]),
    ).toBe(false)
    expect(
      blocked([
        { id: 'database', level: 'ok' },
        { id: 'redis', level: 'problem' },
      ]),
    ).toBe(true)
  })

  it('goes back to finished steps, never to the token once the account exists', () => {
    expect(canGo('access', 'account', false, false)).toBe(true)
    expect(canGo('review', 'account', false, false)).toBe(false) // not reached yet
    expect(canGo('access', 'review', true, false)).toBe(false)
    expect(canGo('account', 'review', true, false)).toBe(false)
    expect(canGo('recordings', 'review', true, false)).toBe(true)
    expect(canGo('recordings', 'finish', true, true)).toBe(false) // Finish is running
  })

  it('checks the password and its repetition', () => {
    expect(passwordProblem('short', 'short')).toBe('short')
    expect(passwordProblem('a long password!', 'a long password?')).toBe('mismatch')
    expect(passwordProblem('a long password!', 'a long password!')).toBe('')
  })

  it('checks the shape of an AI address and leaves private hosts to the server', () => {
    for (const ok of [
      'https://api.openai.com/v1',
      'http://127.0.0.1:8081/v1',
      'http://whisper:8000/v1',
      'http://localhost/v1',
    ])
      expect(addressProblem(ok), ok).toBe(false)
    for (const bad of [
      '',
      'api.openai.com',
      'https://user:pw@ai.example.com/v1',
      'https://ai.example.com/v1?key=x',
      'ftp://x',
    ])
      expect(addressProblem(bad), bad).toBe(true)
  })

  it('writes the lines for config.toml by table', () => {
    expect(
      tomlLines({
        'recordings.enabled': true,
        'ai.summary_model': 'gpt-4.1-mini',
        'recordings.mode': 'video',
        'ai.transcription.base_url': 'http://127.0.0.1:8081/v1',
        'recordings.retention_days': 30,
      }),
    ).toBe(
      '[ai]\nsummary_model = "gpt-4.1-mini"\n\n[ai.transcription]\nbase_url = "http://127.0.0.1:8081/v1"\n\n' +
        '[recordings]\nenabled = true\nmode = "video"\nretention_days = 30',
    )
    expect(tomlLines({})).toBe('')
  })
})
