package application

import "context"

// SourceCloner deja disponible en disco una copia local de un repositorio git en
// un ref concreto. vex-engine solo abre repositorios locales: la CLI los clona y
// los monta en el contenedor.
type SourceCloner interface {
	// Ensure clona o actualiza repoURL y deja el árbol de trabajo en ref (rama,
	// tag o commit). Devuelve la ruta local y el commit exacto resuelto.
	Ensure(ctx context.Context, repoURL, ref string) (Source, error)
}

// Source es un repositorio local fijado a un commit.
type Source struct {
	Path   string
	Commit string
}
