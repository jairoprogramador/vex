package ports

import (
	"errors"
	"time"
)

var (
	// ErrCatalogSignature: el catálogo de plantillas no trae una firma válida de quien lo publica. No se usa.
	ErrCatalogSignature = errors.New("la firma del catálogo de plantillas no es válida")
	// ErrCatalogRollback: el catálogo descargado está bien firmado, pero es más antiguo que el último aceptado.
	ErrCatalogRollback = errors.New("el catálogo de plantillas es más antiguo que el ya verificado")
	// ErrCatalogUnavailable: no hay catálogo que usar (sin red y sin una copia verificada).
	ErrCatalogUnavailable = errors.New("el catálogo de plantillas no está disponible")
	// ErrCatalogStale: no se pudo descargar el catálogo y la copia verificada de este equipo es demasiado antigua
	// para seguir fiándose de ella (quien bloquea la descarga no puede congelar el catálogo para siempre).
	ErrCatalogStale = errors.New("la copia guardada del catálogo de plantillas es demasiado antigua")
	// ErrCatalogCacheBroken: hay una copia guardada del catálogo pero falta, o no se entiende, la marca con la
	// fecha del último catálogo aceptado. Sin ella no se puede saber si un catálogo retrocede.
	ErrCatalogCacheBroken = errors.New("la copia guardada del catálogo de plantillas está incompleta")
)

// CatalogVerifier comprueba la firma del catálogo de plantillas.
type CatalogVerifier interface {
	// Verify devuelve cuándo se firmó data. Cualquier fallo es ErrCatalogSignature.
	Verify(data, bundle []byte) (signedAt time.Time, err error)
}
