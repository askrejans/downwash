package tui

import (
	"github.com/askrejans/downwash/internal/pipeline"
)

// stepUpdateMsg is sent from the pipeline goroutine to update step progress.
type stepUpdateMsg struct {
	Step   pipeline.StepName
	Status pipeline.StepStatus
	Msg    string
}

// pipelineDoneMsg is sent when the pipeline finishes (successfully or not).
type pipelineDoneMsg struct {
	Result pipeline.Result
	Err    error
}

// fileSelectedMsg is sent when the user picks a file from the file picker.
type fileSelectedMsg struct {
	Path string
}
