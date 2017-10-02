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

package portal

import (
	"github.com/go-chi/render"
	"fmt"
	"github.com/go-chi/chi"
	"github.com/nuclio/nuclio/pkg/dealer/jobs"
	"net/http"
	"github.com/nuclio/nuclio-sdk"
)

func NewJobsPortal(logger nuclio.Logger, managerCtx *jobs.ManagerContext) (*JobsPortal, error) {
	newJobsPortal := JobsPortal{logger:logger, managerContext:managerCtx}
	return &newJobsPortal, nil
}

type JobsPortal struct {
	logger nuclio.Logger
	managerContext *jobs.ManagerContext
}

func (jp *JobsPortal) getJob(w http.ResponseWriter, r *http.Request) {
	//ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")
	jobID := chi.URLParam(r, "jobID")

	job, err := jp.managerContext.SubmitReq(&jobs.RequestMessage{
		Name:jobID, Namespace:namespace, Type:jobs.RequestTypeJobGet})

	if err != nil  {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	if err := render.Render(w, r, &JobRequest{Job:job.(*jobs.Job)}); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}

func (jp *JobsPortal) deleteJob(w http.ResponseWriter, r *http.Request) {

	namespace := chi.URLParam(r, "namespace")
	jobID := chi.URLParam(r, "jobID")

	_, err := jp.managerContext.SubmitReq(&jobs.RequestMessage{
		Name:jobID, Namespace:namespace, Type:jobs.RequestTypeJobDel})

	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	w.Write([]byte(fmt.Sprintf("Deleted job: %s",jobID)))
}

func (jp *JobsPortal) listJobs(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	list := []render.Renderer{}

	jobList, err := jp.managerContext.SubmitReq(&jobs.RequestMessage{ Name:"",
		Namespace:namespace, Type:jobs.RequestTypeJobList})

	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	for _, j := range jobList.([]*jobs.Job) {
		list = append(list, &JobRequest{Job:j})
	}

	if err := render.RenderList(w, r, list ); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}

func (jp *JobsPortal) createJob(w http.ResponseWriter, r *http.Request) {
	data := &JobRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	_, err := jp.managerContext.SubmitReq(&jobs.RequestMessage{
		Object:data.Job, Type:jobs.RequestTypeJobCreate})

	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	render.Status(r, http.StatusCreated)
	render.Render(w, r, data)
}

func (jp *JobsPortal) updateJob(w http.ResponseWriter, r *http.Request) {

	namespace := chi.URLParam(r, "namespace")
	jobID := chi.URLParam(r, "jobID")

	data := &JobRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	job, err := jp.managerContext.SubmitReq(&jobs.RequestMessage{
		Name:jobID, Namespace:namespace, Object:data.Job, Type:jobs.RequestTypeJobUpdate})

	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	if err := render.Render(w, r, &JobRequest{Job:job.(*jobs.Job)}); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}


type JobRequest struct {
	*jobs.Job
	Tasks  []JobTask  `json:"tasks"`

}

type JobTask struct {
	Id      int      `json:"id"`
	State   string   `json:"state"`
	Process string   `json:"process"`
}

func (j *JobRequest) Bind(r *http.Request) error {
	return nil
}

func (j *JobRequest) Render(w http.ResponseWriter, r *http.Request) error {
	j.Tasks = []JobTask{}
	for _, task := range j.Job.GetTasks() {
		pname := ""
		if task.GetProcess() != nil {
			pname = task.GetProcess().Name
		}
		j.Tasks = append(j.Tasks, JobTask{Id:task.Id, State:jobs.StateNames[task.State], Process:pname} )
	}
	return nil
}

