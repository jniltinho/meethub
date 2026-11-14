import { describe, expect, it } from 'vitest'
import { desktopPreference, shouldNotify, type Incoming } from '@/chat/desktop'

const mention: Incoming = { mine: false, conversation: false, mentioned: true, channelId: 'projects' }
const plain: Incoming = { ...mention, mentioned: false }
const direct: Incoming = { ...plain, conversation: true, channelId: 'dm' }
const elsewhere = { hidden: false, openChannelId: 'general' }

describe('desktop notifications', () => {
  it('a mention or a direct message, in the background or in another conversation', () => {
    expect(shouldNotify('mentions', mention, { hidden: true, openChannelId: 'projects' })).toBe(true)
    expect(shouldNotify('mentions', mention, elsewhere)).toBe(true)
    expect(shouldNotify('mentions', direct, elsewhere)).toBe(true)
    expect(shouldNotify('mentions', plain, elsewhere)).toBe(false)
  })
  it('never while looking at the conversation, never for my own messages, never with none', () => {
    expect(shouldNotify('all', mention, { hidden: false, openChannelId: 'projects' })).toBe(false)
    expect(shouldNotify('all', { ...mention, mine: true }, elsewhere)).toBe(false)
    expect(shouldNotify('none', direct, elsewhere)).toBe(false)
  })
  it('all: every message of my conversations', () => {
    expect(shouldNotify('all', plain, elsewhere)).toBe(true)
  })
  it('the stored preference, with mentions by default', () => {
    expect(desktopPreference('')).toBe('mentions')
    expect(desktopPreference(undefined)).toBe('mentions')
    expect(desktopPreference('all')).toBe('all')
    expect(desktopPreference('none')).toBe('none')
  })
})
