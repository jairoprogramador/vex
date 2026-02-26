package ports

type UserInputService interface {
	Ask(question, defaultValue string) (string, error)
	AskSelect(question string, options, descriptions []string) (int, error)
}
