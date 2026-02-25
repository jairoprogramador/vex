package common

import (
	"os"

	"github.com/AlecAivazis/survey/v2"
	"github.com/jairoprogramador/vex-client/internal/domain/common/ports"
)

type surveyUserInputService struct{}

func NewSurveyUserInputService() ports.UserInputService {
	return &surveyUserInputService{}
}

func (s *surveyUserInputService) Ask(question, defaultValue string) (string, error) {
	var response string
	prompt := &survey.Input{
		Message: question,
		Default: defaultValue,
	}
	err := survey.AskOne(prompt, &response, survey.WithStdio(os.Stdin, os.Stderr, os.Stderr))
	if err != nil {
		return "", err
	}
	return response, nil
}

func (s *surveyUserInputService) AskSelect(question string, options []string, descriptions []string) (int, error) {
	var selectedIndex int
	prompt := &survey.Select{
		Message: question,
		Options: options,
		Description: func(value string, index int) string {
			return descriptions[index]
		},
	}
	survey.SelectQuestionTemplate = `
{{- define "option"}}
    {{- if eq .SelectedIndex .CurrentIndex }}{{color .Config.Icons.SelectFocus.Format }}   {{ .Config.Icons.SelectFocus.Text }} {{else}}{{color "default"}}     {{end}}
    {{- .CurrentOpt.Value}}{{ if ne ($.GetDescription .CurrentOpt) "" }}: {{ if eq .SelectedIndex .CurrentIndex }}{{color "yellow"}}{{ else }}{{color "white"}}{{ end }}{{ $.GetDescription .CurrentOpt }}{{end}}
    {{- color "reset"}}
{{end}}
{{- if .ShowHelp }}{{- color .Config.Icons.Help.Format }}{{ .Config.Icons.Help.Text }} {{ .Help }}{{color "reset"}}{{"\n"}}{{end}}
{{- color .Config.Icons.Question.Format }}{{ .Config.Icons.Question.Text }} {{color "reset"}}
{{- color "white+hb"}}{{ .Message }}{{ .FilterMessage }}{{color "reset"}}
{{- if .ShowAnswer}}{{color "green+b"}} {{.Answer}}{{color "reset"}}{{"\n"}}
{{- else}}
  {{- "\n"}}
  {{- range $ix, $option := .PageEntries}}
    {{- template "option" $.IterateOption $ix $option}}
  {{- end}}
{{- end}}`

	err := survey.AskOne(prompt, &selectedIndex,
		survey.WithStdio(os.Stdin, os.Stderr, os.Stderr),
		customIcons())
	if err != nil {
		return 0, err
	}
	return selectedIndex, nil
}

func customIcons() survey.AskOpt {
	return survey.WithIcons(func(icons *survey.IconSet) {
		icons.Question.Text = "→"
		icons.Question.Format = "cyan+b"
		icons.SelectFocus.Text = "▸"
		icons.SelectFocus.Format = "green+b"
	})
}
