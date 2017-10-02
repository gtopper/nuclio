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
	"github.com/nuclio/nuclio/pkg/dealer/jobs"
	"github.com/go-chi/render"
	"fmt"
	"github.com/go-chi/chi"
	"net/http"
	"github.com/nuclio/nuclio-sdk"
)

func NewProcPortal(logger nuclio.Logger, managerCtx *jobs.ManagerContext) (*ProcPortal, error) {
	newProcPortal := ProcPortal{logger:logger, managerContext:managerCtx}
	return &newProcPortal, nil
}

type ProcPortal struct {
	logger nuclio.Logger
	managerContext *jobs.ManagerContext
}

func (pp *ProcPortal) getProcess(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	procID := chi.URLParam(r, "procID")

	proc, err := pp.managerContext.SubmitReq(&jobs.RequestMessage{
		Name:procID, Namespace:namespace, Type:jobs.RequestTypeProcGet})

	if err != nil  {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	if err := render.Render(w, r, proc.(*jobs.ProcessMessage)); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}

func (pp *ProcPortal) deleteProcess(w http.ResponseWriter, r *http.Request) {

	namespace := chi.URLParam(r, "namespace")
	procID := chi.URLParam(r, "procID")

	_, err := pp.managerContext.SubmitReq(&jobs.RequestMessage{
		Name:procID, Namespace:namespace, Type:jobs.RequestTypeProcDel})

	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	w.Write([]byte(fmt.Sprintf("Deleted process: %s",procID)))
}

func (pp *ProcPortal) listProcess(w http.ResponseWriter, r *http.Request) {

	namespace := chi.URLParam(r, "namespace")
	list := []render.Renderer{}

	procList, err := pp.managerContext.SubmitReq(&jobs.RequestMessage{ Name:"",
		Namespace:namespace, Type:jobs.RequestTypeProcList})

	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	for _, p := range procList.([]*jobs.Process) {
		list = append(list, &ProcessRequest{Process:p})
	}

	if err := render.RenderList(w, r, list ); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}

}

func (pp *ProcPortal) createProcess(w http.ResponseWriter, r *http.Request) {
	data := &ProcessRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	proc, err := pp.managerContext.SubmitReq(&jobs.RequestMessage{
		Object:data.Process, Type:jobs.RequestTypeProcCreate})

	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	render.Status(r, http.StatusCreated)
	if err := render.Render(w, r, proc.(*jobs.ProcessMessage)); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}

func (pp *ProcPortal) updateProcess(w http.ResponseWriter, r *http.Request) {

	namespace := chi.URLParam(r, "namespace")
	procID := chi.URLParam(r, "procID")

	data := &ProcessRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	proc, err := pp.managerContext.SubmitReq(&jobs.RequestMessage{
		Name:procID, Namespace:namespace, Object:data.Process, Type:jobs.RequestTypeProcUpdate})

	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	if err := render.Render(w, r, proc.(*jobs.ProcessMessage)); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}


type ProcessRequest struct {
	*jobs.Process
	Tasks []jobs.Task
}

func (p *ProcessRequest) Bind(r *http.Request) error {
	return nil
}

func (p *ProcessRequest) Render(w http.ResponseWriter, r *http.Request) error {
	p.Tasks = []jobs.Task{}
	for _, task := range p.Process.GetTasks(false) {
		p.Tasks = append(p.Tasks, jobs.Task{Id:task.Id, State:task.State})
	}
	return nil
}




