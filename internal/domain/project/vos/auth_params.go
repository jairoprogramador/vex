package vos

import "errors"

type AuthParams struct {
	clientID     string
	grantType    string
	clientSecret string
	scope        string
	extra        map[string]string
}

func NewAuthParams(
	clientID, grantType,
	clientSecret, scope string,
	extra map[string]string) (AuthParams, error) {

	if clientID == "" {
		return AuthParams{}, errors.New("clientID is required")
	}
	if grantType == "" {
		return AuthParams{}, errors.New("grantType is required")
	}
	if clientSecret == "" {
		return AuthParams{}, errors.New("clientSecret is required")
	}
	if scope == "" {
		return AuthParams{}, errors.New("scope is required")
	}

	return AuthParams{
		clientID:     clientID,
		grantType:    grantType,
		clientSecret: clientSecret,
		scope:        scope,
		extra:        extra}, nil
}

func (a AuthParams) ClientID() string         { return a.clientID }
func (a AuthParams) GrantType() string        { return a.grantType }
func (a AuthParams) ClientSecret() string     { return a.clientSecret }
func (a AuthParams) Scope() string            { return a.scope }
func (a AuthParams) Extra() map[string]string { return a.extra }
