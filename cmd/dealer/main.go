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

package main

import (
	"flag"
	"fmt"
	"github.com/nuclio/logger"
	"github.com/nuclio/nuclio/cmd/dealer/app"
	"github.com/nuclio/nuclio/pkg/dealer/jobs"
	"github.com/nuclio/nuclio/pkg/dealer/kubewatch"
	"github.com/nuclio/nuclio/pkg/dealer/portal"
	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
	"os"
	"time"
	"github.com/nuclio/zap"
)

func run() error {
	verbose := flag.Bool("d", true, "Verbose")
	kubeconf := flag.String("k", "config", "Path to a kube config. Only required if out-of-cluster.")
	//kubeconf := flag.String("k", "", "Path to a kube config. Only required if out-of-cluster.")
	namespace := flag.String("n", "", "Namespace")
	nopush := flag.Bool("np", false, "Disable push updates to process")
	spath := flag.String("f", "", "job files dir")
	flag.Parse()

	logger, _ := createLogger(*verbose)

	dealer, err := app.NewDealer(logger, &jobs.ManagerContextConfig{
		DisablePush: *nopush,
		StorePath:   *spath,
	})
	if err != nil {
		return err
	}

	var kubeClient *kubernetes.Clientset
	config, err := kubewatch.GetClientConfig(*kubeconf)
	if err != nil {
		logger.Warn("Did not find kubernetes config")
	} else {
		kubeClient, err = kubernetes.NewForConfig(config)
		if err != nil {
			logger.ErrorWith("Did not manage to create kubernetes NewForConfig", "config", config, "err", err)
			kubeClient = nil
		}
	}

	if kubeClient != nil {
		// Recover previous task state in case of restart/failure
		// List Deployments & Init, List Jobs & Init, List Processes & Init
		depList, err := kubewatch.ListDeployments(kubeClient, logger, *namespace)
		if err != nil {
			return err
		}

		newDepList := []*jobs.Deployment{}
		for _, dep := range depList {
			logger.DebugWith("Init, UpdateDeployment", "deploy", dep)
			newDep, err := dealer.DeployMap.UpdateDeployment(dep)
			if err == nil {
				newDepList = append(newDepList, newDep)
			}
		}

		err = dealer.InitJobs(*namespace)
		if err != nil {
			logger.ErrorWith("Did not manage to InitJobs", "err", err)
		}

		procList, err := kubewatch.ListPods(kubeClient, logger, *namespace)
		if err != nil {
			logger.ErrorWith("Did not manage to ListPods", "err", err)
		}
		dealer.InitProcesses(procList)
		dealer.RebalanceNewDeps(newDepList)
	}

	err = dealer.Start()
	if err != nil {
		return err
	}

	if kubeClient != nil {
		err = kubewatch.NewDeployWatcher(kubeClient, dealer.Ctx, logger, *namespace)
		if err != nil {
			return err
		}

		time.Sleep(time.Second)

		err = kubewatch.NewPodWatcher(kubeClient, dealer.Ctx, logger, *namespace)
		if err != nil {
			return err
		}

	}

	listenPort := 3000
	portal, err := portal.NewPortal(logger, dealer.Ctx, listenPort)
	if err != nil {
		return err
	}

	return portal.Start()
}

func main() {

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to run dealer: %s", err)

		os.Exit(1)
	}
}

func createLogger(verbose bool) (logger.Logger, error) {
	var loggerLevel nucliozap.Level

	if verbose {
		loggerLevel = nucliozap.DebugLevel
	} else {
		loggerLevel = nucliozap.InfoLevel
	}

	logger, err := nucliozap.NewNuclioZapCmd("dealer", loggerLevel)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to create logger")
	}

	return logger, nil

}