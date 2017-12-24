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
	"github.com/nuclio/nuclio-sdk"
	"github.com/nuclio/nuclio/pkg/dealer/asyncflow"
	"github.com/nuclio/nuclio/pkg/dealer/client"
)

type ManagerContext struct {
	Logger            nuclio.Logger
	RequestsChannel   chan *RequestMessage
	ProcRespChannel   chan *client.Response
	AsyncTasksChannel chan *asyncflow.AsyncWorkflowTask
	Client            *client.AsyncClient
	JobStore          JobStore
	DisablePush       bool
}

type ManagerContextConfig struct {
	DisablePush bool
	StorePath   string
}

func NewManagerContext(logger nuclio.Logger, asyncClient *client.AsyncClient, config *ManagerContextConfig) *ManagerContext {
	newContext := ManagerContext{
		Logger:            logger.GetChild("jobMng").(nuclio.Logger),
		RequestsChannel:   make(chan *RequestMessage, 100),
		ProcRespChannel:   make(chan *client.Response, 100),
		AsyncTasksChannel: make(chan *asyncflow.AsyncWorkflowTask, 100),
		Client:            asyncClient,
		DisablePush:       config.DisablePush,
	}

	newContext.JobStore = NewJobFileStore(config.StorePath, logger)
	return &newContext
}

func (mc *ManagerContext) SubmitReq(request *RequestMessage) (interface{}, error) {
	respChan := make(chan *RespChanType)
	request.ReturnChan = respChan
	mc.RequestsChannel <- request
	resp := <-respChan
	return resp.Object, resp.Err
	return nil, nil
}

func (mc *ManagerContext) SaveJobs(jobs map[string]*Job) {
	if len(jobs) == 0 {
		return
	}

	mc.Logger.DebugWith("Saving Jobs", "jobs", len(jobs))
	for _, job := range jobs {
		err := mc.JobStore.SaveJob(job)
		if err != nil {
			mc.Logger.ErrorWith("Error Saving Job", "jobs", job.Name, "err", err)
		}
	}
}

func (mc *ManagerContext) DeleteJobRecords(jobs map[string]*Job) {
	if len(jobs) == 0 {
		return
	}

	mc.Logger.DebugWith("Deleting Jobs", "jobs", jobs)
	// TODO: delete jobs state from persistent storage
}

func (mc *ManagerContext) NewWorkflowTask(task asyncflow.AsyncWorkflowTask) *asyncflow.AsyncWorkflowTask {
	return asyncflow.NewWorkflowTask(mc.Logger, &mc.AsyncTasksChannel, task)
}

type RequestType int

const (
	RequestTypeUnknown RequestType = iota

	RequestTypeJobGet
	RequestTypeJobDel
	RequestTypeJobList
	RequestTypeJobCreate
	RequestTypeJobUpdate

	RequestTypeProcGet
	RequestTypeProcDel
	RequestTypeProcList
	RequestTypeProcUpdateState
	RequestTypeProcUpdate
	RequestTypeProcHealth

	RequestTypeDeployUpdate
	RequestTypeDeployRemove
	RequestTypeDeployList
)

type RespChanType struct {
	Err    error
	Object interface{}
}

type RequestMessage struct {
	Namespace  string
	Function   string // for jobs
	Name       string
	Type       RequestType
	Object     interface{}
	ReturnChan chan *RespChanType
}
