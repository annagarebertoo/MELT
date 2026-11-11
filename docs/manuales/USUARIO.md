# Manual de uso de MELT

## Acceso

La aplicación local se abre en <http://localhost:8080> tras iniciar Compose. El registro público crea un cliente; para usar la cuenta, ingresar desde «Mi cuenta». No hay recuperación de contraseña documentada en el servicio actual. Los roles internos son asignados por personal autorizado y no se eligen desde el registro.

## Navegación pública

- **Inicio / Selección MELT:** accesos al catálogo y a la reserva.
- **Tratamientos:** buscar por texto/categoría/precio, ordenar y paginar; abrir detalle y consultar profesionales habilitados.
- **Profesionales:** revisar perfiles públicos y acceder a los tratamientos relacionados.
- **Reservar turno:** elegir tratamiento, profesional y fecha, consultar horarios, seleccionar uno y agregarlo al carrito. Consultar disponibilidad no inicia pago.

Los resultados dependen de categorías, perfiles, asociaciones y agenda activa en la base del entorno. Catálogo vacío no se completa automáticamente con datos demo.

## Cliente

En «Mi cuenta» el cliente puede consultar su saldo/historial de puntos, recompensas y canjes, sus reservas, GhostKitchen (si la integración está configurada) y cerrar sesión. Un horario agregado queda retenido durante diez minutos. «Mi carrito» muestra retenciones y resumen; quitar un item libera su retención. Confirmar compra inicia checkout agrupado, cuyo resultado de pago local es simulado. Ningún cargo real se ejecuta.

Ante un rechazo, el carrito se conserva para reintentar dentro del vencimiento, sin crear reservas confirmadas. Los horarios expiran y deben seleccionarse de nuevo. Una compra confirmada genera reservas consultables en «Mis turnos».

## Profesional

Una cuenta con rol profesional necesita vincularse desde administración a un perfil de Catálogo. La sección «Mi agenda» permite consultar/configurar disponibilidad y ver turnos propios; acciones permitidas incluyen marcar realizado o registrar no asistencia según estado y hora del turno. Un profesional no debe operar sobre turnos de otro perfil.

## Recepción

El rol se conserva en Auth y tiene permisos de operación concretos en Reservas; el frontend mantiene el comportamiento existente y no agrega un panel de recepción separado. Solicitudes se limitan por los endpoints, estados y propiedad aplicables.

## Administrador y superadministrador

- **Administrador:** administra categorías, tratamientos, perfiles/vinculación profesional, recompensas/reglas de puntos y mensajes fallidos según sus permisos.
- **Superadministrador:** «Mi cuenta» presenta administración de usuarios/roles. Sólo este rol puede listar cuentas o cambiar roles. La creación de usuario requiere endpoint administrativo existente.

La visibilidad de secciones no reemplaza la autorización de backend. Si una acción devuelve 403, revisar que la cuenta tenga el rol adecuado; no modificar roles desde la consola del navegador.

## Integraciones externas

El endpoint del gimnasio se consume desde el backend del partner con `X-Partner-Key`; el usuario final no debe compartir claves. La vista GhostKitchen requiere una cuenta cliente y una integración configurada. Ninguna integración se simula con datos inventados cuando el proveedor no está disponible.
