package auth

import (
	"context"
	"fmt"

	"github.com/jairoprogramador/vex/internal/application/ports"
	"github.com/jairoprogramador/vex/internal/fdplugin"
)

type authService struct{}

func NewAuthService() ports.AuthService {
	return &authService{}
}

func (s *authService) Authenticate(ctx context.Context, provider string, request *fdplugin.AuthenticateRequest) (*fdplugin.AuthenticateResponse, error) {
	conn, err := NewPluginConnector(ctx, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to auth plugin '%s': %w", provider, err)
	}
	defer conn.Close()

	client := fdplugin.NewAuthServiceClient(conn.GRPCConn())

	response, err := client.Authenticate(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("gRPC authentication call failed: %w", err)
	}

	if response.Token == nil {
		return nil, fmt.Errorf("plugin returned a nil token")
	}

	return response, nil
}
