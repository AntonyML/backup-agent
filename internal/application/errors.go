package application

import "errors"

var (
	// ErrLocked indica que otra instancia del agente está corriendo con el lock adquirido.
	ErrLocked = errors.New("otra instancia del agente está en ejecución")

	// ErrAlreadyRanToday indica que ya existe un backup válido del día de hoy.
	ErrAlreadyRanToday = errors.New("ya se ejecutó un backup el día de hoy")

	// ErrPendingSync indica que el backup local fue exitoso pero quedó pendiente la sincronización remota.
	ErrPendingSync = errors.New("backup local exitoso pero sincronización remota pendiente")

	// ErrInvalidConfig indica que la configuración no superó la validación.
	ErrInvalidConfig = errors.New("configuración inválida")

	// ErrNoPendingBackup indica que no se encontró archivo pendiente para sincronizar.
	ErrNoPendingBackup = errors.New("no hay sincronizaciones pendientes")
)
