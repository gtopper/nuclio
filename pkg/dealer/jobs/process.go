/*
Copyright 2017 The Nuclio Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package jobs

import (
	"encoding/json"
	"fmt"
	"github.com/nuclio/nuclio/pkg/dealer/client"
	"github.com/pkg/errors"
	"net/http"
	"time"
	"github.com/nuclio/logger"
)

type ProcessState int8

const (
	ProcessStateUnknown  ProcessState = 0
	ProcessStateReady    ProcessState = 1
	ProcessStateNotReady ProcessState = 2
	ProcessStateFailed   ProcessState = 3
	ProcessStateDeleted  ProcessState = 4
)

const DEFAULT_PORT = 8077

type BaseProcess struct {
	Name        string       `json:"name"`
	Namespace   string       `json:"namespace"`
	Function    string       `json:"function"`
	Version     string       `json:"version,omitempty"`
	Alias       string       `json:"alias,omitempty"`
	IP          string       `json:"ip"`
	Port        int          `json:"port"`
	State       ProcessState `json:"state"`
	LastEvent   time.Time    `json:"lastEvent,omitempty"`
	TotalEvents int          `json:"totalEvents,omitempty"`
}

type Process struct {
	BaseProcess
	deployment *Deployment
	LastUpdate time.Time `json:"lastUpdate,omitempty"`

	//BaseProcess
	logger        logger.Logger
	ctx           *ManagerContext
	removingTasks bool
	//tasks         []*Task
	jobs map[string]*procJob
}

// Process request and response for the REST API
type ProcessMessage struct {
	BaseProcess

	DealerURL string `json:"dealerURL,omitempty"`
	//Tasks     []TaskMessage       `json:"tasks,omitempty"`
	Jobs map[string]JobShort `json:"jobs,omitempty"`
}

// TODO: should be aligned with Event definition struct
type JobShort struct {
	TotalTasks int           `json:"totalTasks"`
	Tasks      []TaskMessage `json:"tasks,omitempty"`
	Metadata   interface{}   `json:"metadata,omitempty"`
}

type procJob struct {
	job   *Job
	tasks []*Task
}

func (p *ProcessMessage) Bind(r *http.Request) error {
	return nil
}

func (p *ProcessMessage) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func NewProcess(logger logger.Logger, context *ManagerContext, newProc *ProcessMessage) (*Process, error) {
	proc := &Process{BaseProcess: newProc.BaseProcess}
	if proc.Namespace == "" {
		proc.Namespace = "default"
	}
	proc.LastUpdate = time.Now()
	proc.ctx = context
	proc.logger = logger
	proc.jobs = map[string]*procJob{}
	return proc, nil
}

func ProcessKey(name, namespace string) string { return name + "." + namespace }

func (p *Process) AsString() string {
	tasks := ""
	for _, job := range p.jobs {
		active:=0
		for _, task := range job.tasks {
			if task.state == TaskStateRunning || task.state == TaskStateAlloc {
				active += 1
			}
		}
		tasks += fmt.Sprintf("%s(%d) ", job.job.Name, active)
	}
	return fmt.Sprintf("%s-%d: %s", p.Name, p.State, tasks)
}

func (p *Process) GetDeployment() *Deployment {
	return p.deployment
}

// Clear Deployment and Jobs (when deleting the deployment and the processes remain)
func (p *Process) ClearAll() {
	p.deployment = nil
	p.jobs = map[string]*procJob{}
}

// force remove a process: mark its tasks unassigned, remove from job, rebalance (assign the tasks to other procs)
func (p *Process) Remove() error {

	for _, job := range p.jobs {
		for _, task := range job.tasks {
			task.SetState(TaskStateUnassigned)
			task.SetProcess(nil)
			task.LastUpdate = time.Now()
		}
		if job.job.GetState() == JobStateStopping && job.job.assignedTasks == 0 {
			job.job.UpdateState(JobStateSuspended)
		}
	}

	p.removingTasks = true
	return nil
}

// Request to stop all process tasks
func (p *Process) ClearTasks() error {
	hadTasks := false
	p.State = ProcessStateDeleted

	for _, job := range p.jobs {
		for _, task := range job.tasks {
			task.SetState(TaskStateStopping)
			hadTasks = true
		}
	}

	if hadTasks {
		p.removingTasks = true
		return p.PushUpdates()
	}

	return nil
}

// Request to stop all process tasks
func (p *Process) ClearJobTasks(job string) error {
	j, ok := p.jobs[job]
	if ok {
		for _, task := range j.tasks {
			task.SetState(TaskStateStopping)
		}

		if len(j.tasks) > 0 {
			p.removingTasks = true
			return p.PushUpdates()
		}
	}

	return nil
}

// return list of tasks assigned to this proc
func (p *Process) GetTasks(active bool) []*Task {
	list := []*Task{}
	for _, job := range p.jobs {
		for _, task := range job.tasks {
			if !active || task.GetState() != TaskStateStopping {
				list = append(list, task)
			}
		}
	}
	return list
}

// return list of tasks assigned to this proc
func (p *Process) GetJobTasksLen(job string, active bool) int {
	total := 0
	j, ok := p.jobs[job]
	if ok {
		if !active {
			return len(j.tasks)
		}
		for _, task := range j.tasks {
			if task.GetState() != TaskStateStopping {
				total += 1
			}
		}
	}

	return total
}

// return task based on Id and Job name
func (p *Process) GetTask(job string, id int) *Task {
	j, ok := p.jobs[job]
	if ok {
		for _, task := range j.tasks {
			if task.Id == id {
				return task
			}
		}
	}
	return nil
}

// add list of tasks to process
func (p *Process) AddTasks(tasks []*Task) {
	for _, task := range tasks {
		task.SetState(TaskStateAlloc)
		task.SetProcess(p)
		task.LastUpdate = time.Now()

		jobName := task.job.Name
		_, ok := p.jobs[jobName]
		if !ok {
			p.jobs[jobName] = &procJob{job: task.job}
		}
		p.jobs[jobName].tasks = append(p.jobs[jobName].tasks, task)
	}

}

// remove specific task from Process
func (p *Process) RemoveTask(job string, id int) {
	j, ok := p.jobs[job]
	if ok {
		for i, task := range j.tasks {
			if task.Id == id {
				p.jobs[job].tasks = append(p.jobs[job].tasks[:i], p.jobs[job].tasks[i+1:]...)
				task.SetProcess(nil)
				if len(p.jobs[job].tasks) == 0 {
					delete(p.jobs, job)
				}
				return
			}
		}
	}
}

// move N Tasks to state Stopping
func (p *Process) StopNTasks(toDelete int) {
	if toDelete <= 0 {
		return
	}

	taskStopped := 0
	// TODO Balance stop tasks across jobs (currently will stop all per job & move to next, maybe ok)
loop:
	for _, job := range p.jobs {
		for _, task := range job.tasks {
			task.SetState(TaskStateStopping)
			taskStopped += 1
			if taskStopped == toDelete {
				break loop
			}
		}
	}
	p.logger.DebugWith("StopNTasks", "processor", p.Name, "delete", toDelete, "stopped", taskStopped)
}

func (p *Process) getProcessURL() string {
	port := p.Port
	if port == 0 {
		port = DEFAULT_PORT
	}
	return fmt.Sprintf("%s:%d", p.IP, port)
}

// send updates to process
func (p *Process) PushUpdates() error {

	p.logger.DebugWith("Push updates to processor", "processor", p.Name, "state", p.AsString())

	// if process IP is unknown or unset or push disabled return without sending
	if p.IP == "" || p.ctx.DisablePush {
		p.emulateProcess()
		return nil
	}

	message := p.GetProcessState()
	body, err := json.Marshal(message)
	if err != nil {
		return errors.Wrap(err, "Failed to Marshal process for update")
	}

	host := p.getProcessURL()
	request := client.ChanRequest{
		Method:     "POST",
		HostURL:    host,
		Url:        fmt.Sprintf("http://%s/triggers", host), //TODO: have proper URL
		Body:       body,
		NeedResp:   false,
		ReturnChan: p.ctx.ProcRespChannel,
	}

	p.ctx.Client.Submit(&request)

	return nil
}

// handle task update requests from process, or responses from process following Push Update ops
func (p *Process) HandleTaskUpdates(msg *ProcessMessage, isRequest, isInit bool) error {

	tasksDeleted := false
	tasksStopping := false
	moreTasksToDelete := false
	forceRebalance := false
	jobsToSave := []*Job{}

	// Update state of currently allocated tasks
	for jobName, msgJob := range msg.Jobs {
		for _, msgTask := range msgJob.Tasks {
			taskID := msgTask.Id
			job, ok := p.deployment.jobs[jobName]
			if !ok {
				p.logger.ErrorWith("Task job (name) not found under deployment", "processor", p.Name, "task", taskID, "job", jobName)
				continue
			}
			if taskID >= job.TotalTasks {
				p.logger.ErrorWith("Illegal TaskID, greater than total tasks #", "processor", p.Name, "task", taskID, "job", jobName)
				continue
			}

			task := job.GetTask(taskID)

			// TODO: if task.process = nil, after dealer restart, we need to assign this task to the process

			// verify the reporting process is the true owner of that task, we may have already re-alocated it
			if task.process != nil && task.process.Name != p.Name {
				p.logger.ErrorWith("Task process is mapped to a different process",
					"processor", p.Name, "task", taskID, "job", jobName, "task-proc", task.process.Name)
				continue
			}

			// Do we need to persist job metadata ?
			if task.CheckPoint != nil && !job.NeedToSave() {
				jobsToSave = append(jobsToSave, job)
			}

			task.LastUpdate = time.Now()
			task.CheckPoint = msgTask.CheckPoint
			task.Progress = msgTask.Progress
			task.Delay = msgTask.Delay

			if isInit && msgTask.State != TaskStateDeleted {
				if task.process != nil {
					p.logger.WarnWith("Task process is not null during init", "processor", p.Name, "task", taskID, "job", jobName)
				}
				task.SetProcess(p)
				_, ok := p.jobs[jobName]
				if !ok {
					p.jobs[jobName] = &procJob{job: job}
				}
				p.jobs[jobName].tasks = append(p.jobs[jobName].tasks, task)
			}

			switch msgTask.State {
			case TaskStateDeleted:
				if !isInit {
					task.SetState(TaskStateUnassigned)
					p.RemoveTask(jobName, taskID)
					tasksDeleted = true
					if job.GetState() == JobStateStopping && job.assignedTasks != 0 {
						// wait with re-balance until all Job tasks are removed
						moreTasksToDelete = true
					}
					if job.GetState() == JobStateStopping && job.assignedTasks == 0 {
						job.UpdateState(JobStateSuspended)
						forceRebalance = true
						moreTasksToDelete = false
					}
				}
			case TaskStateStopping:
				// Tasks are still in Stopping state, so we keep the process in removingTasks state
				task.SetState(TaskStateStopping)
				tasksStopping = true
			case TaskStateCompleted:
				if task.GetState() != TaskStateCompleted {
					// if this is the first time we get completion we add the task to completed and save list
					job.CompletedTasks = append(job.CompletedTasks, taskID)
					if !job.NeedToSave() {
						jobsToSave = append(jobsToSave, job)
					}
				}
				task.SetState(TaskStateCompleted)
				p.RemoveTask(jobName, taskID)
			case TaskStateRunning:
				// verify its a legal transition (e.g. we didnt ask to stop and got an old update)
				if isInit || task.GetState() == TaskStateRunning || task.GetState() == TaskStateAlloc {
					task.SetState(msgTask.State)
				}
			default:
				p.logger.ErrorWith("illegal returned state in task ID", "processor", p.Name, "task", taskID, "job", jobName, "state", msgTask.State)
				continue
			}

		}

	}

	// persist critical changes to modified Jobs (had completions or checkpoints)
	p.ctx.SaveJobs(jobsToSave)

	// if it is a request from the process, check if need to allocate tasks (will respond with updated task list)
	if isRequest && !p.removingTasks {
		err := p.deployment.AllocateTasks(p)
		if err != nil {
			p.logger.ErrorWith("Failed to allocate tasks", "processor", p.Name)
			return errors.Wrap(err, "Failed to allocate tasks")
		}
	}

	// if some tasks deleted (returned to pool) rebalance
	p.logger.DebugWith("HandleTaskUpdates", "processor", p.Name, "forceRebalance", forceRebalance, "tasksDeleted", tasksDeleted, "tasksStopping", tasksStopping)
	if tasksDeleted && !moreTasksToDelete {
		p.removingTasks = true
		p.deployment.Rebalance() //TODO: verify no circular dep
	}

	p.removingTasks = tasksStopping

	return nil

}

// return an enriched process struct for API
func (p *Process) GetProcessState() *ProcessMessage {
	msg := ProcessMessage{BaseProcess: p.BaseProcess}
	msg.Jobs = map[string]JobShort{}

	for jobName, job := range p.jobs {
		taskList := []TaskMessage{}
		for _, task := range job.tasks {
			taskList = append(taskList, task.ToMessage(false))
		}
		msg.Jobs[jobName] = JobShort{TotalTasks: job.job.TotalTasks, Metadata: job.job.Metadata, Tasks: taskList}
	}

	return &msg
}

// emulate a process locally, may be broken
func (p *Process) emulateProcess() {

	msg := ProcessMessage{BaseProcess: p.BaseProcess}
	msg.Jobs = map[string]JobShort{}

	for jobName, job := range p.jobs {
		taskList := []TaskMessage{}
		for _, task := range job.tasks {
			taskmsg := task.ToMessage(false)
			switch task.GetState() {
			case TaskStateStopping:
				taskmsg.State = TaskStateDeleted
			default:
				taskmsg.State = TaskStateRunning
			}
			taskList = append(taskList, taskmsg)
		}
		msg.Jobs[jobName] = JobShort{TotalTasks: job.job.TotalTasks, Metadata: job.job.Metadata, Tasks: taskList}
	}
	p.logger.DebugWith("emulateProcess", "processor", p.Name, "jobs", msg.Jobs)

	go func() {
		time.Sleep(time.Millisecond)
		body, _ := json.Marshal(msg)
		resp := client.EmulatedResp(body)
		p.ctx.ProcRespChannel <- resp
		//p.ctx.SubmitReq(&RequestMessage{
		//	Object: &msg, Type: RequestTypeProcUpdate})
	}()

}
