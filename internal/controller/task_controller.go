/*
Copyright 2024.

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

package controller

import (
	"context"
	"sort"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"strings"
	customiov1 "task-operator/api/v1"

	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	pkgLog "sigs.k8s.io/controller-runtime/pkg/log"
)

// TaskReconciler reconciles a Task object
type TaskReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	JobStatusPending   = "Pending"
	JobStatusRunning   = "Running"
	JobStatusCompleted = "Completed"
	JobStatusFailed    = "Failed"
	maxRequests        = 1 // Max Request allowed for Demo
	resetDuration      = 30 * time.Second
)

var (
	requests  int
	lastReset time.Time
)

// +kubebuilder:rbac:groups=custom.io.intuit.com,resources=tasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=custom.io.intuit.com,resources=tasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=custom.io.intuit.com,resources=tasks/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// Triggered when ever Task and Cronjob is created/modified
func (r *TaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	res := rateLimit()
	if res {
		fmt.Println("req not processed due to rate limt->", req.Name)
		return ctrl.Result{}, nil

	}
	fmt.Println("req processed ->", req.Name)
	// process task CRD request
	task := &customiov1.Task{}

	log := pkgLog.FromContext(ctx).WithValues("Task", task.Name, "Namespace", task.Namespace)
	log.Info("Reconciling Task")

	// Rate Limt

	// Generate the CronJob object
	cronJob := CreateCronJob(task)

	// Set the owner reference on the CronJob to point to the Task resource
	if err := controllerutil.SetControllerReference(task, cronJob, r.Scheme); err != nil {
		log.Error(err, "Error setting owner reference for cron job")
	}

	err := r.Create(ctx, cronJob)
	if err != nil {
		// log and Metrics

		if errors.IsAlreadyExists(err) {
			log.Info("Task alreay Exist: ")

		} else {
			log.Error(err, "Error Creating cronjob")
			// update status
			task.Status.State = JobStatusFailed
			r.UpdateJobStatus(ctx, task, log)
			taskFailureCount.Inc()
		}

	} else {
		log.Info("Created cronjob")
		taskTotalCount.Inc()
	}

	// Fetch the CronJob
	reconcileCronJob := &batchv1.CronJob{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, reconcileCronJob)
	task = &customiov1.Task{}
	if err != nil {
		// Handle the reconciled CronJob-related logic here
		if errors.IsNotFound(err) {
			log.Info("cronjob not found")
		} else {
			log.Error(err, "Error Fetching reconciled cronjob")
			taskFailureCount.Inc()
		}

	} else {

		log.Info("reconsiling cronjob")

		// update the Task using job status

		err := r.Client.Get(ctx, types.NamespacedName{Name: reconcileCronJob.Name, Namespace: reconcileCronJob.Namespace}, task)

		if err != nil {
			log.Error(err, "Error Fetching Task:")
			taskFailureCount.Inc()

		} else {
			if reconcileCronJob.Status.LastSuccessfulTime != nil {
				task.Status.LastExecutionTime = reconcileCronJob.Status.LastSuccessfulTime.String()
			}

			latestJob, err := r.GetLatestJobByOwner(ctx, reconcileCronJob.Name, reconcileCronJob.Kind, reconcileCronJob.Namespace)
			if err != nil {
				log.Error(err, "Failed to fetch the latest Job")
				taskFailureCount.Inc()

			} else if latestJob != nil {
				log.Info("Found latest Job", "JobName", latestJob.Name)
				status, err := r.GetJobStatus(ctx, latestJob.Name, latestJob.Namespace)
				if err != nil {
					log.Error(err, "Failed to fetch the latest Job")
				}
				task.Status.State = status

			} else {
				log.Info("No Jobs found for owner", "OwnerName", reconcileCronJob.Name)
			}
			log.Info("No Jobs found for owner", "OwnerName", reconcileCronJob.Name)

		}

		// err = r.Status().Update(ctx, initialTask)

		// if err != nil {
		// 	log.Error(err, "Error updating Task with status:")

		// }
		r.UpdateJobStatus(ctx, task, log)

	}
	// update metrics
	taskList := &customiov1.TaskList{}
	err = r.List(ctx, taskList)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	} else {
		TaskMetricUpdater(taskList)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TaskReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&customiov1.Task{}).
		Owns(&batchv1.CronJob{}). // Watches changes to CronJob resources owned by Task
		Complete(r)
}

// CreateCronJob generates a Kubernetes CronJob object
func CreateCronJob(taskResource *customiov1.Task) *batchv1.CronJob {
	commandSlice := strings.Fields(taskResource.Spec.Command)
	labels := map[string]string{
		"generatedBy": "task-operator",
	}
	// Define the CronJob object
	cronJob := &batchv1.CronJob{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CronJob",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskResource.Name,
			Namespace: taskResource.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule: taskResource.Spec.Schedule,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					//TTLSecondsAfterFinished: int32Ptr(10), // commenting for Demo
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{
								{
									Name:    taskResource.Name,
									Image:   "busybox", // Example image
									Command: commandSlice,
								},
							},
						},
					},
				},
			},
		},
	}

	return cronJob
}

// GetJobStatus fetches the Job associated with a CronJob and retrieves its status
func (r *TaskReconciler) GetJobStatus(ctx context.Context, jobName string, namespace string) (string, error) {
	// Define the Job object
	taskStatus := JobStatusPending // default to Pending
	job := &batchv1.Job{}

	// Get the Job using the client
	err := r.Client.Get(ctx, types.NamespacedName{Name: jobName, Namespace: namespace}, job)
	if err != nil {
		return taskStatus, err
	}

	// Access the status field of the Job
	jobStatus := job.Status

	// Print or log various parts of the status just for informational
	fmt.Printf("Job Status for Job: %s\n", job.Name)
	fmt.Printf("Active Pods: %d\n", jobStatus.Active)
	fmt.Printf("Succeeded Pods: %d\n", jobStatus.Succeeded)
	fmt.Printf("Failed Pods: %d\n", jobStatus.Failed)
	fmt.Printf("CompletionTime: %v\n", jobStatus.CompletionTime)
	if jobStatus.Active != 0 {
		taskStatus = JobStatusRunning
		return taskStatus, nil
	} else if jobStatus.Failed != 0 {
		taskStatus = JobStatusFailed
		return taskStatus, nil
	} else if jobStatus.Succeeded == 1 {
		taskStatus = JobStatusCompleted
		return taskStatus, nil
	}

	return taskStatus, nil
}

// GetLatestJobByOwner retrieves the latest Job associated with the given owner (e.g., CronJob).
func (r *TaskReconciler) GetLatestJobByOwner(ctx context.Context, ownerName string, ownerKind string, namespace string) (*batchv1.Job, error) {
	// List all Jobs in the namespace
	jobList := &batchv1.JobList{}
	err := r.Client.List(ctx, jobList, client.InNamespace(namespace))
	if err != nil {
		return nil, err
	}

	// Filter Jobs by the owner reference
	var ownedJobs []batchv1.Job
	for _, job := range jobList.Items {
		for _, ownerRef := range job.OwnerReferences {
			if ownerRef.Name == ownerName && ownerRef.Kind == ownerKind {
				ownedJobs = append(ownedJobs, job)
				break
			}
		}
	}

	if len(ownedJobs) == 0 {
		return nil, nil // No jobs found
	}

	// Sort the jobs by creation timestamp to get the latest one
	sort.Slice(ownedJobs, func(i, j int) bool {
		return ownedJobs[i].CreationTimestamp.After(ownedJobs[j].CreationTimestamp.Time)
	})

	// Return the latest Job
	return &ownedJobs[0], nil
}

// UpdateJobStatus fetches the Job associated with a CronJob and retrieves its status
func (r *TaskReconciler) UpdateJobStatus(ctx context.Context, task *customiov1.Task, log logr.Logger) {

	err := r.Status().Update(ctx, task)

	if err != nil {
		log.Error(err, "Error updating task status")
	} else {
		log.Info("Updated status for task.")
	}

}

func int32Ptr(i int32) *int32 {
	return &i
}

// Metrics Updater
func TaskMetricUpdater(taskList *customiov1.TaskList) {
	fmt.Println(" len(taskList.Items)->", len(taskList.Items))
	taskPendingCount.Set(0)
	taskRunningCount.Set(0)
	taskCompletedCount.Set(0)
	taskFailedCount.Set(0)
	if len(taskList.Items) > 0 {
		for _, task := range taskList.Items {
			if task.Status.State == JobStatusPending {
				taskPendingCount.Add(1)
			} else if task.Status.State == JobStatusRunning {
				taskRunningCount.Add(1)
			} else if task.Status.State == JobStatusCompleted {
				taskCompletedCount.Add(1)
			} else if task.Status.State == JobStatusFailed {
				taskFailedCount.Add(1)
			}
		}

	}

}

func rateLimit() bool {
	// Reset the counter if the time window has passed
	fmt.Println("maxRequests->", maxRequests)
	fmt.Println("resetDuration->", resetDuration)
	fmt.Println("requests->", requests)
	fmt.Println("lastReset->", lastReset)

	if time.Since(lastReset) > resetDuration {
		requests = 0
		lastReset = time.Now()
	}

	// Check if the request can be allowed
	if requests < maxRequests {
		requests++
		return true
	}
	return false
}
