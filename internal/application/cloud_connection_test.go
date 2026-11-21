package application

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestContractID_Cloud(t *testing.T) {
	cases := map[ContractID]string{
		"azure/v1":  "azure",
		"aws/v12":   "aws",
		"":          "",
		"azure":     "",
		"azure/1":   "",
		"azure/v0":  "",
		"Azure/v1":  "",
		"azure/v1/": "",
		"/v1":       "",
	}
	for id, want := range cases {
		assert.Equal(t, want, id.Cloud(), "ContractID(%q)", string(id))
	}
}

func TestIssuedToken_NuncaMuestraElValor(t *testing.T) {
	token := IssuedToken{
		Value:        "eyJhbGciOiJSUzI1NiJ9.c2VjcmV0bw.firma",
		ExpiresAt:    time.Date(2026, 11, 20, 15, 10, 0, 0, time.UTC),
		RefreshAfter: time.Date(2026, 11, 20, 15, 5, 0, 0, time.UTC),
	}

	shown := fmt.Sprintf("%v %+v %#v %s %v", token, token, token, token, &token)
	wrapped := fmt.Errorf("fallo con %v", token).Error()

	assert.NotContains(t, shown, "eyJ")
	assert.NotContains(t, shown, "c2VjcmV0bw")
	assert.NotContains(t, wrapped, "eyJ")
	assert.Contains(t, shown, "2026-11-20T15:10:00Z", "los tiempos sí se ven: sirven para diagnosticar")
}
