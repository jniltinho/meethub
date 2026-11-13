import type { Catalog } from './index'

// Portuguese (Brazil). Keys are the English source strings; "error.<code>" keys translate API error codes.
const catalog: Catalog = {
  'error.bad_request': 'A requisição é inválida.',
  'error.invalid_input': 'Alguns campos são inválidos.',
  'error.weak_password': 'A senha deve ter pelo menos 12 caracteres.',
  'error.invalid_theme': 'Tema desconhecido.',
  'error.invalid_language': 'Idioma não suportado.',
  'error.unauthenticated': 'Você precisa entrar.',
  'error.session_expired': 'Sua sessão expirou. Entre novamente.',
  'error.invalid_credentials': 'E-mail ou senha inválidos.',
  'error.forbidden': 'Você não tem permissão para isso.',
  'error.csrf_failed': 'A requisição foi bloqueada por segurança. Recarregue a página.',
  'error.invalid_setup_token': 'O token de instalação não é válido.',
  'error.not_found': 'Não encontrado.',
  'error.already_initialized': 'O MeetHub já está configurado.',
  'error.email_taken': 'Este e-mail já está em uso.',
  'error.last_owner': 'A organização precisa manter pelo menos um proprietário ativo.',
  'error.rate_limited': 'Muitas tentativas sem sucesso. Tente mais tarde.',
  'error.busy': 'O servidor está ocupado. Tente novamente em instantes.',
  'error.network': 'Não foi possível contatar o servidor.',
}

export default catalog
