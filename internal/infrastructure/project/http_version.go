package project

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jairoprogramador/vex/internal/domain/project/ports"
)

type GitHubRelease struct {
	TagName string `json:"tag_name"`
}

type HttpVersion struct {
}

func NewHttpVersion() ports.Version {
	return &HttpVersion{}
}

func (g *HttpVersion) GetLatest() (string, error) {
	apiUrl := "https://api.github.com/repos/jairoprogramador/vex/releases/latest"

	resp, err := http.Get(apiUrl)
	if err != nil {
		return "", fmt.Errorf("error al llamar a la API de GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("la API de GitHub devolvió un estado no exitoso: %s", resp.Status)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("error al decodificar la respuesta JSON: %w", err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("no se encontró 'tag_name' en la respuesta de la API")
	}

	version := strings.TrimPrefix(release.TagName, "v")

	return version, nil
}
