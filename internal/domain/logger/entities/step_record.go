package entities

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jairoprogramador/vex/internal/domain/logger/vos"
)

type StepRecord struct {
	id        uuid.UUID
	name      string
	status    vos.Status
	startTime time.Time
	endTime   time.Time
	reason    string
	tasks     []*TaskRecord
	err       error
}

func NewStepRecord(name string) (*StepRecord, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("could not generate uuid for step record: %w", err)
	}
	if name == "" {
		return nil, fmt.Errorf("step name cannot be empty")
	}
	return &StepRecord{
		id:     id,
		name:   name,
		status: vos.Pending,
		tasks:  []*TaskRecord{},
	}, nil
}

func HydrateStepRecord(
	name string,
	status vos.Status,
	startTime time.Time,
	endTime time.Time,
	reason string,
	tasks []*TaskRecord,
	StepErr error) (*StepRecord, error) {

	id, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("could not generate uuid for step record: %w", err)
	}
	return &StepRecord{
		id:        id,
		name:      name,
		status:    status,
		startTime: startTime,
		endTime:   endTime,
		reason:    reason,
		tasks:     tasks,
		err:       StepErr,
	}, nil
}

func (s *StepRecord) ID() uuid.UUID {
	return s.id
}

func (s *StepRecord) Name() string {
	return s.name
}

func (s *StepRecord) Reason() string {
	return s.reason
}

func (s *StepRecord) StartTime() time.Time {
	return s.startTime
}

func (s *StepRecord) EndTime() time.Time {
	return s.endTime
}

func (s *StepRecord) MarkAsRunning() {
	if s.status == vos.Pending {
		s.status = vos.Running
		s.startTime = time.Now()
	}
}

func (t *StepRecord) MarkAsSuccess() {
	if t.status == vos.Running {
		t.status = vos.Success
		t.endTime = time.Now()
	}
}

func (t *StepRecord) MarkAsSkipped() {
	if t.status == vos.Pending {
		t.status = vos.Skipped
		t.endTime = time.Now()
	}
}

func (t *StepRecord) MarkAsCached(reason string) {
	if t.status == vos.Pending {
		t.status = vos.Cached
		t.endTime = time.Now()
		t.reason = reason
	}
}

func (t *StepRecord) MarkAsFailure(err error) {
	if t.status == vos.Running {
		t.status = vos.Failure
		t.endTime = time.Now()
		t.err = err
	}
}

func (s *StepRecord) AddTask(task *TaskRecord) {
	s.tasks = append(s.tasks, task)
}

func (s *StepRecord) Tasks() []*TaskRecord {
	return s.tasks
}

func (s *StepRecord) Status() vos.Status {
	s.recalculateStatus()
	return s.status
}

func (s *StepRecord) Error() error {
	return s.err
}

func (s *StepRecord) recalculateStatus() {
	if s.status == vos.Success || s.status == vos.Failure || s.status == vos.Cached || s.status == vos.Skipped {
		return
	}

	if len(s.tasks) == 0 {
		return
	}

	hasFailure := false
	allFinished := true

	for _, task := range s.tasks {
		if task.Status() == vos.Failure {
			hasFailure = true
			break
		}
		if task.Status() != vos.Success {
			allFinished = false
		}
	}

	if hasFailure {
		if s.status == vos.Running {
			s.endTime = time.Now()
			s.status = vos.Failure
		}
		return
	}

	if allFinished {
		if s.status == vos.Running {
			s.status = vos.Success
			s.endTime = time.Now()
		}
		return
	}

	s.status = vos.Running
}
