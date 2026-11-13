import type { Catalog } from './index'

// Spanish. Keys are the English source strings; "error.<code>" keys translate API error codes.
const catalog: Catalog = {
  'error.bad_request': 'La solicitud no es válida.',
  'error.invalid_input': 'Algunos campos no son válidos.',
  'error.weak_password': 'La contraseña debe tener al menos 12 caracteres.',
  'error.invalid_theme': 'Tema desconocido.',
  'error.invalid_language': 'Idioma no admitido.',
  'error.unauthenticated': 'Debe iniciar sesión.',
  'error.session_expired': 'Su sesión expiró. Inicie sesión de nuevo.',
  'error.invalid_credentials': 'Correo electrónico o contraseña no válidos.',
  'error.forbidden': 'No tiene permiso para hacer esto.',
  'error.csrf_failed': 'La solicitud se bloqueó por seguridad. Recargue la página.',
  'error.invalid_setup_token': 'El token de instalación no es válido.',
  'error.not_found': 'No encontrado.',
  'error.already_initialized': 'MeetHub ya está configurado.',
  'error.email_taken': 'Este correo electrónico ya está en uso.',
  'error.last_owner': 'La organización debe conservar al menos un propietario activo.',
  'error.rate_limited': 'Demasiados intentos fallidos. Inténtelo más tarde.',
  'error.busy': 'El servidor está ocupado. Inténtelo de nuevo en breve.',
  'error.network': 'No se pudo contactar con el servidor.',
}

export default catalog
