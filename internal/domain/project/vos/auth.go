package vos

type Auth struct {
	plugin string
	params AuthParams
}

func NewAuth(plugin string, params AuthParams) Auth {
	return Auth{plugin: plugin, params: params}
}

func (a Auth) Plugin() string     { return a.plugin }
func (a Auth) Params() AuthParams { return a.params }
