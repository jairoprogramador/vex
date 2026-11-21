package bootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// vectorsSHA256 es el sha256 de testdata/hash-vectors.json. El archivo es un contrato con el backend, que
// implementa la misma huella: si cambia, cambian los dos lados.
const vectorsSHA256 = "076a4173d3817c3f2e77b57f72403c7538c2588430e9a8f7efe053e6cfade4bc"

type vectorFile struct {
	Path       string `json:"path"`
	ContentB64 string `json:"content_b64"`
}

type vector struct {
	ID    string       `json:"id"`
	Files []vectorFile `json:"files"`
	Hash  *string      `json:"hash"`
	Error string       `json:"error"`
}

type vectorSet struct {
	Version int      `json:"version"`
	Vectors []vector `json:"vectors"`
	Invalid []vector `json:"invalid"`
}

func loadVectors(t *testing.T) vectorSet {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hash-vectors.json"))
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	require.Equal(t, vectorsSHA256, hex.EncodeToString(sum[:]), "el archivo de vectores cambió")

	var set vectorSet
	require.NoError(t, json.Unmarshal(data, &set))
	require.Equal(t, 1, set.Version)
	return set
}

func (v vector) files(t *testing.T) []File {
	t.Helper()
	files := make([]File, len(v.Files))
	for i, f := range v.Files {
		content, err := base64.StdEncoding.DecodeString(f.ContentB64)
		require.NoError(t, err)
		files[i] = File{Path: f.Path, Content: content}
	}
	return files
}

func (s vectorSet) hashOf(t *testing.T, id string) string {
	t.Helper()
	for _, v := range s.Vectors {
		if v.ID == id {
			hash, err := Hash(v.files(t))
			require.NoError(t, err)
			return hash
		}
	}
	t.Fatalf("no existe el vector %s", id)
	return ""
}

func TestHashVectores(t *testing.T) {
	set := loadVectors(t)
	require.Len(t, set.Vectors, 13)
	require.Len(t, set.Invalid, 8)

	for _, v := range set.Vectors {
		t.Run(v.ID, func(t *testing.T) {
			got, err := Hash(v.files(t))

			require.NoError(t, err)
			want := ""
			if v.Hash != nil {
				want = *v.Hash
			}
			assert.Equal(t, want, got)
		})
	}
	for _, v := range set.Invalid {
		t.Run(v.ID, func(t *testing.T) {
			got, err := Hash(v.files(t))

			var hashErr *HashError
			require.ErrorAs(t, err, &hashErr)
			assert.Equal(t, v.Error, hashErr.Code)
			assert.Empty(t, got, "un error nunca trae huella")
		})
	}

	t.Run("fin de línea y BOM no cambian la huella", func(t *testing.T) {
		assert.Equal(t, set.hashOf(t, "V2-orden"), set.hashOf(t, "V3-crlf"))
		assert.Equal(t, set.hashOf(t, "V2-orden"), set.hashOf(t, "V4-bom"))
	})
	t.Run("la frontera entre ruta y contenido cuenta", func(t *testing.T) {
		assert.NotEqual(t, set.hashOf(t, "V10-limite-ruta-contenido-a"), set.hashOf(t, "V11-limite-ruta-contenido-b"))
	})
}

func TestHash_NoDependeDelOrdenDeEntradaYSiDeCadaByte(t *testing.T) {
	var subdir vector
	for _, v := range loadVectors(t).Vectors {
		if v.ID == "V5-subdir-unicode" {
			subdir = v
		}
	}
	files := subdir.files(t)
	want, err := Hash(files)
	require.NoError(t, err)

	for range 20 {
		rand.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })
		got, err := Hash(files)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}

	for i := range files {
		changed := subdir.files(t)
		changed[i].Content = append(bytes.Clone(changed[i].Content), 'x')
		got, err := Hash(changed)
		require.NoError(t, err)
		assert.NotEqual(t, want, got, "cambiar %s debe cambiar la huella", changed[i].Path)
	}
}

func TestHash_NoModificaLaEntrada(t *testing.T) {
	files := []File{{Path: "b", Content: []byte("\xEF\xBB\xBFuno\r\n")}, {Path: "a", Content: []byte("dos")}}

	_, err := Hash(files)

	require.NoError(t, err)
	assert.Equal(t, "b", files[0].Path)
	assert.Equal(t, []byte("\xEF\xBB\xBFuno\r\n"), files[0].Content)
}

func TestHash_LimiteDeTamano(t *testing.T) {
	exact := []File{{Path: "a.txt", Content: bytes.Repeat([]byte("a"), MaxTotalBytes)}}
	hash, err := Hash(exact)
	require.NoError(t, err, "exactamente 1 MiB se acepta")
	assert.Len(t, hash, 64)

	over := []File{{Path: "a.txt", Content: bytes.Repeat([]byte("a"), MaxTotalBytes)}, {Path: "b.txt", Content: []byte("b")}}
	_, err = Hash(over)
	var hashErr *HashError
	require.ErrorAs(t, err, &hashErr)
	assert.Equal(t, CodeTooLarge, hashErr.Code)
}
