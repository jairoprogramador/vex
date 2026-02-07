package logger

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/jairoprogramador/vex/internal/application/ports"
	"github.com/jairoprogramador/vex/internal/domain/logger/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/logger/entities"
	"github.com/jairoprogramador/vex/internal/domain/logger/vos"
)

type failedInfo struct {
	failedName   string
	failedErr    error
	failedOutput string
}

type ConsolePresenterService struct {
	writer io.Writer

	header     *color.Color
	ctxKey     *color.Color
	ctxValue   *color.Color
	success    *color.Color
	failure    *color.Color
	running    *color.Color
	subtle     *color.Color
	errorTitle *color.Color
	errorBody  *color.Color
}

func NewConsolePresenterService() ports.PresenterService {
	return &ConsolePresenterService{
		writer:     os.Stdout,
		header:     color.New(color.FgCyan, color.Bold),
		ctxKey:     color.New(color.FgYellow),
		ctxValue:   color.New(color.FgWhite),
		success:    color.New(color.FgGreen),
		failure:    color.New(color.FgRed),
		running:    color.New(color.FgBlue),
		subtle:     color.New(color.Faint),
		errorTitle: color.New(color.FgRed, color.Bold),
		errorBody:  color.New(color.FgWhite),
	}
}

func (p *ConsolePresenterService) Header(log *aggregates.Logger, revision string) {
	p.Line()
	p.header.Fprintf(p.writer, "Release ID: %s\n", revision)
	p.Line()
	ctx := log.Context()
	if len(ctx) > 0 {
		keys := make([]string, 0, len(ctx))
		longestKey := 0
		for key := range ctx {
			keys = append(keys, key)
			if len(key) > longestKey {
				longestKey = len(key)
			}
		}
		sort.Strings(keys)

		for _, key := range keys {
			format := fmt.Sprintf("  %%-%ds: %%s\n", longestKey)
			p.ctxKey.Fprintf(p.writer, format, key, p.ctxValue.Sprint(ctx[key]))
		}
	}
	p.Line()
}

func (p *ConsolePresenterService) Step(step *entities.StepRecord) {
	if step.Status() == vos.Skipped {
		p.subtle.Fprintf(p.writer, "<%s>: <OMITTED>\n", strings.ToUpper(step.Name()))
		return
	}
	if step.Status() == vos.Cached {
		p.subtle.Fprintf(p.writer, "<%s>: <CACHED> (%s)\n", strings.ToUpper(step.Name()), step.Reason())
		return
	}
	if step.Status() == vos.Running {
		p.running.Fprintf(p.writer, "<%s>: <STARTING>\n", strings.ToUpper(step.Name()))
		return
	}
	if step.Status() == vos.Success {
		p.success.Fprintf(p.writer, "<%s>: <COMPLETE>\n", strings.ToUpper(step.Name()))
		return
	}
	if step.Status() == vos.Failure {
		p.Line()
		p.failure.Fprintf(p.writer, "<%s>: <FAILED>\n", strings.ToUpper(step.Name()))
		return
	}
}

func (p *ConsolePresenterService) Task(task *entities.TaskRecord, step *entities.StepRecord) {
	switch task.Status() {
	case vos.Success:
		p.success.Fprintf(p.writer, "<%s>: <%s> (%s)\n", strings.ToUpper(step.Name()), strings.ToUpper(task.Name()), strings.ToUpper(task.Status().String()))
	case vos.Failure:
		p.failure.Fprintf(p.writer, "<%s>: <%s> (%s)\n", strings.ToUpper(step.Name()), strings.ToUpper(task.Name()), strings.ToUpper(task.Status().String()))
		p.failure.Fprintf(p.writer, "<%s>: <%s> (comando: %s)\n", strings.ToUpper(step.Name()), strings.ToUpper(task.Name()), task.Command())
	case vos.Running:
		p.running.Fprintf(p.writer, "<%s>: <%s> (%s)\n", strings.ToUpper(step.Name()), strings.ToUpper(task.Name()), strings.ToUpper(task.Status().String()))
	default:
		p.subtle.Fprintf(p.writer, "<%s>: <%s> (%s)\n", strings.ToUpper(step.Name()), strings.ToUpper(task.Name()), strings.ToUpper(task.Status().String()))
	}
}

func (p *ConsolePresenterService) FinalSummary(log *aggregates.Logger) {
	faileds := []failedInfo{}
	for _, step := range log.Steps() {
		if step.Status() == vos.Failure {
			faileds = append(faileds, failedInfo{
				failedName:   step.Name(),
				failedErr:    step.Error(),
				failedOutput: step.Reason(),
			})
		}

		for _, task := range step.Tasks() {
			if task.Status() == vos.Failure {
				faileds = append(faileds, failedInfo{
					failedName:   task.Name(),
					failedErr:    task.Error(),
					failedOutput: task.OutputString(),
				})
			}
		}
	}

	if len(faileds) > 0 {
		p.Line()
		p.renderErrors(faileds)
	}
}

func (p *ConsolePresenterService) renderErrors(faileds []failedInfo) {
	p.errorTitle.Fprintln(p.writer, "ERRORS:")
	for _, failed := range faileds {
		p.failure.Fprintf(p.writer, "\n● error in: %s\n", failed.failedName)
		if failed.failedErr != nil {
			p.errorBody.Fprintf(p.writer, "%s\n", failed.failedErr.Error())
		}
		if failed.failedOutput != "" {
			p.errorBody.Fprintf(p.writer, "%s\n", failed.failedOutput)
		}
	}
}

func (p *ConsolePresenterService) Line() {
	p.subtle.Fprintln(p.writer, strings.Repeat("-", 70))
}
