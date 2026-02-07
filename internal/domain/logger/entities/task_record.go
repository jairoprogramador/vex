package entities

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jairoprogramador/vex/internal/domain/logger/vos"
)

type TaskRecord struct {
	id        uuid.UUID
	name      string
	status    vos.Status
	command   string
	startTime time.Time
	endTime   time.Time
	output    []vos.OutputLine
	err       error
}

func NewTaskRecord(name string) (*TaskRecord, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("could not generate uuid for task record: %w", err)
	}
	if name == "" {
		return nil, fmt.Errorf("task name cannot be empty")
	}
	return &TaskRecord{
		id:     id,
		name:   name,
		status: vos.Pending,
		output: make([]vos.OutputLine, 0),
	}, nil
}

func HydrateTaskRecord(
	name string,
	status vos.Status,
	command string,
	startTime time.Time,
	endTime time.Time,
	output []vos.OutputLine,
	err error) (*TaskRecord, error) {

	id, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("could not generate uuid for task record: %w", err)
	}
	return &TaskRecord{
		id:        id,
		name:      name,
		status:    status,
		command:   command,
		startTime: startTime,
		endTime:   endTime,
		output:    output,
		err:       err,
	}, nil
}

func (t *TaskRecord) ID() uuid.UUID {
	return t.id
}

func (t *TaskRecord) Name() string {
	return t.name
}

func (t *TaskRecord) Status() vos.Status {
	return t.status
}

func (t *TaskRecord) Command() string {
	return t.command
}

func (t *TaskRecord) SetCommand(command string) {
	t.command = command
}

func (t *TaskRecord) MarkAsRunning() {
	if t.status == vos.Pending {
		t.status = vos.Running
		t.startTime = time.Now()
	}
}

func (t *TaskRecord) MarkAsSuccess() {
	if t.status == vos.Running {
		t.status = vos.Success
		t.endTime = time.Now()
	}
}

func (t *TaskRecord) MarkAsFailure(err error) {
	if t.status == vos.Running {
		t.status = vos.Failure
		t.endTime = time.Now()
		t.err = err
	}
}

func (t *TaskRecord) AddOutput(line string) {
	if t.status == vos.Running {
		t.output = append(t.output, vos.NewOutputLine(line))
	}
}

func (t *TaskRecord) Output() []vos.OutputLine {
	return t.output
}

func (t *TaskRecord) OutputString() string {
	output := ""
	for _, line := range t.output {
		output += line.Line() + "\n"
	}
	return output
}

func (t *TaskRecord) Error() error {
	return t.err
}

func (t *TaskRecord) StartTime() time.Time {
	return t.startTime
}

func (t *TaskRecord) EndTime() time.Time {
	return t.endTime
}
