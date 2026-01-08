/*
Copyright 2025.

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
	"fmt"
	"math/rand"
	"time"

	chaosv1alpha1 "github.com/emzm17/chaos-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"                   //  For Pod, PodList types
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1" // For metav1.Now() timestamps
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ChaosExperimentReconciler reconciles a ChaosExperiment object
type ChaosExperimentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=chaos.chaos.example.com,resources=chaosexperiments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=chaos.chaos.example.com,resources=chaosexperiments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=chaos.chaos.example.com,resources=chaosexperiments/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ChaosExperiment object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *ChaosExperimentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling ChaosExperiment", "namespace", req.Namespace, "name", req.Name)

	// Fetch the ChaosExperiment instance
	experiment := &chaosv1alpha1.ChaosExperiment{}
	err := r.Get(ctx, req.NamespacedName, experiment)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Resource was deleted, nothing to do
			logger.Info("ChaosExperiment resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		logger.Error(err, "Failed to get ChaosExperiment")
		return ctrl.Result{}, err
	}

	// State machine based on current phase
	switch experiment.Status.Phase {
	case "":
		// Initialize new experiment
		return r.initializeExperiment(ctx, experiment)

	case "Pending":
		// Start the experiment
		return r.startExperiment(ctx, experiment)

	case "Running":
		// Check if experiment is complete
		return r.checkExperimentCompletion(ctx, experiment)

	case "Completed", "Failed":
		// Terminal states - no further action needed
		logger.Info("Experiment in terminal state", "phase", experiment.Status.Phase)
		return ctrl.Result{}, nil

	default:
		// Unknown phase - reset to pending
		logger.Info("Unknown phase, resetting to Pending", "phase", experiment.Status.Phase)
		experiment.Status.Phase = "Pending"
		experiment.Status.Message = "Phase reset due to unknown state"
		if err := r.Status().Update(ctx, experiment); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}
}

func (r *ChaosExperimentReconciler) startExperiment(ctx context.Context, experiment *chaosv1alpha1.ChaosExperiment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Starting chaos experiment", "type", experiment.Spec.Type)

	// Parse duration (default 60s if invalid)
	duration, err := time.ParseDuration(experiment.Spec.Duration)
	if err != nil {
		logger.Error(err, "Invalid duration, using default 60s", "duration", experiment.Spec.Duration)
		duration = 60 * time.Second
	}

	// Execute first chaos round
	affectedPods, err := r.executeChaos(ctx, experiment)
	if err != nil {
		logger.Error(err, "Failed to execute initial chaos")
		experiment.Status.Phase = "Failed"
		experiment.Status.Message = fmt.Sprintf("Failed to start: %v", err)
		if updateErr := r.Status().Update(ctx, experiment); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, err
	}

	// Update status to Running
	nowMeta := metav1.Now()
	experiment.Status.Phase = "Running"
	experiment.Status.StartTime = &nowMeta
	experiment.Status.LastChaosTime = &nowMeta
	experiment.Status.AffectedPods = affectedPods
	experiment.Status.ChaosRounds = 1
	experiment.Status.Message = fmt.Sprintf("Round 1: Affecting %d pods", len(affectedPods))

	if err := r.Status().Update(ctx, experiment); err != nil {
		logger.Error(err, "Failed to update status to Running")
		return ctrl.Result{}, err
	}

	logger.Info("Experiment started", "affectedPods", len(affectedPods), "duration", duration, "round", 1)
	// Status update triggers immediate reconciliation, which will call checkExperimentCompletion
	return ctrl.Result{}, nil
}

func (r *ChaosExperimentReconciler) checkExperimentCompletion(ctx context.Context, experiment *chaosv1alpha1.ChaosExperiment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Parse duration (default 60s if invalid)
	duration, err := time.ParseDuration(experiment.Spec.Duration)
	if err != nil {
		duration = 60 * time.Second
	}

	// Check if StartTime is set
	if experiment.Status.StartTime == nil {
		logger.Info("StartTime not set, moving back to Pending")
		experiment.Status.Phase = "Pending"
		if err := r.Status().Update(ctx, experiment); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	now := time.Now()
	totalElapsed := now.Sub(experiment.Status.StartTime.Time)

	// Check if total duration has expired
	if totalElapsed >= duration {
		// Experiment completed
		endTime := metav1.Now()
		experiment.Status.Phase = "Completed"
		experiment.Status.EndTime = &endTime
		experiment.Status.Message = fmt.Sprintf("Chaos completed after %s (%d rounds)", duration, experiment.Status.ChaosRounds)

		if err := r.Status().Update(ctx, experiment); err != nil {
			logger.Error(err, "Failed to update status to Completed")
			return ctrl.Result{}, err
		}

		logger.Info("Experiment completed successfully", "duration", duration, "rounds", experiment.Status.ChaosRounds)
		return ctrl.Result{}, nil
	}

	// Check if continuous chaos is enabled
	if experiment.Spec.Interval != "" {
		interval, err := time.ParseDuration(experiment.Spec.Interval)
		if err != nil {
			logger.Error(err, "Invalid interval, defaulting to single execution")
			interval = duration // Effectively disables continuous mode
		}

		// Check if it's time for next chaos round
		if experiment.Status.LastChaosTime != nil {
			timeSinceLastChaos := now.Sub(experiment.Status.LastChaosTime.Time)

			if timeSinceLastChaos >= interval {
				// Time to execute another chaos round
				logger.Info("Executing next chaos round", "round", experiment.Status.ChaosRounds+1, "timeSinceLastChaos", timeSinceLastChaos)

				// Execute chaos
				affectedPods, err := r.executeChaos(ctx, experiment)
				if err != nil {
					logger.Error(err, "Failed to execute chaos round")
					experiment.Status.Message = fmt.Sprintf("Failed chaos round %d: %v", experiment.Status.ChaosRounds+1, err)
					if updateErr := r.Status().Update(ctx, experiment); updateErr != nil {
						return ctrl.Result{}, updateErr
					}
					// Retry after 10 seconds
					return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
				}

				// Update status with new chaos round
				nowMeta := metav1.Now()
				experiment.Status.ChaosRounds++
				experiment.Status.LastChaosTime = &nowMeta
				experiment.Status.AffectedPods = affectedPods
				experiment.Status.Message = fmt.Sprintf("Round %d: Affecting %d pods", experiment.Status.ChaosRounds, len(affectedPods))

				if err := r.Status().Update(ctx, experiment); err != nil {
					logger.Error(err, "Failed to update status after chaos round")
					return ctrl.Result{}, err
				}

				logger.Info("Chaos round executed", "round", experiment.Status.ChaosRounds, "affectedPods", len(affectedPods))

				// Requeue after interval for next round
				remaining := duration - totalElapsed
				nextCheck := interval
				if nextCheck > remaining {
					nextCheck = remaining
				}
				return ctrl.Result{RequeueAfter: nextCheck}, nil
			}

			// Not yet time for next round, wait
			nextRoundIn := interval - timeSinceLastChaos
			remaining := duration - totalElapsed
			if nextRoundIn > remaining {
				nextRoundIn = remaining
			}
			logger.Info("Waiting for next chaos round", "nextRoundIn", nextRoundIn, "round", experiment.Status.ChaosRounds)
			return ctrl.Result{RequeueAfter: nextRoundIn}, nil
		}
	}

	// Single execution mode - just wait for duration to complete
	remaining := duration - totalElapsed
	logger.Info("Experiment still running", "elapsed", totalElapsed, "remaining", remaining)
	return ctrl.Result{RequeueAfter: remaining}, nil
}

func (r *ChaosExperimentReconciler) executeChaos(ctx context.Context, experiment *chaosv1alpha1.ChaosExperiment) ([]string, error) {
	logger := log.FromContext(ctx)

	// Get target namespace
	targetNs := experiment.Spec.TargetNamespace
	if targetNs == "" {
		targetNs = experiment.Namespace
	}

	// List pods matching the selector
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(experiment.Spec.Selector) // convert into a selector
	listOpts := &client.ListOptions{
		Namespace:     targetNs,
		LabelSelector: labelSelector,
	}

	if err := r.List(ctx, podList, listOpts); err != nil { //  Asking for podlist with matching labels and storing the list in podList
		logger.Error(err, "Failed to list pods")
		return nil, err
	}

	if len(podList.Items) == 0 {
		logger.Info("No pods found matching selector")
		return nil, fmt.Errorf("no pods found matching selector")
	}

	// Calculate pods to affect
	percentage := experiment.Spec.Percentage
	if percentage == 0 {
		percentage = 100
	}
	numToAffect := (len(podList.Items) * percentage) / 100 //  Calculate No of Pods
	if numToAffect <= 0 {
		numToAffect = 1
	}

	// Shuffle and pick pods
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(podList.Items), func(i, j int) {
		podList.Items[i], podList.Items[j] = podList.Items[j], podList.Items[i]
	})

	// Execute chaos on selected pods
	affectedPods := []string{}
	for i := 0; i < numToAffect && i < len(podList.Items); i++ {
		pod := &podList.Items[i]
		switch experiment.Spec.Type {
		case "pod-kill":
			if err := r.killPod(ctx, pod); err != nil {
				logger.Error(err, "Failed to kill pod", "pod", pod.Name)
			} else {
				affectedPods = append(affectedPods, pod.Name)
			}
		case "network-delay":
			if err := r.addNetworkDelay(ctx, pod, experiment); err != nil {
				logger.Error(err, "Failed to add network delay to pod", "pod", pod.Name)
			} else {
				affectedPods = append(affectedPods, pod.Name)
			}
		case "cpu-stress":
			if err := r.increaseStress(ctx, pod, experiment); err != nil {
				logger.Error(err, "Failed to stress pod", "pod", pod.Name)
			} else {
				affectedPods = append(affectedPods, pod.Name)
			}
		case "memory-stress":
			if err := r.increaseMemoryStress(ctx, pod, experiment); err != nil {
				logger.Error(err, "Failed to add memory stress to pod", "pod", pod.Name)
			} else {
				affectedPods = append(affectedPods, pod.Name)
			}
		default:
			logger.Info("Unknown chaos type", "type", experiment.Spec.Type)
		}
	}

	return affectedPods, nil
}

func (r *ChaosExperimentReconciler) increaseMemoryStress(ctx context.Context, pod *corev1.Pod, experiment *chaosv1alpha1.ChaosExperiment) error {
	logger := log.FromContext(ctx)
	logger.Info("Adding memory stress to Pod", "pod", pod.Name, "namespace", pod.Namespace)

	// Get memory configuration
	memory := experiment.Spec.MemoryStress.Memory
	if memory == "" {
		memory = "256M" // Default memory to consume
	}

	workers := experiment.Spec.MemoryStress.Workers
	if workers <= 0 {
		workers = 1
	}

	duration, err := time.ParseDuration(experiment.Spec.Duration)
	if err != nil {
		logger.Error(err, "Invalid duration, using default 60s", "duration", experiment.Spec.Duration)
		duration = 60 * time.Second
	}
	// Convert duration to int32 seconds for TTL
	ttl := int32(duration.Seconds())

	memoryStressJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("chaos-memory-stress-%s", pod.Name),
			Namespace: pod.Namespace,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeName:      pod.Spec.NodeName,
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:    "memory-stress",
						Image:   "polinux/stress-ng:latest",
						Command: []string{"stress-ng", "--vm", fmt.Sprintf("%d", workers), "--vm-bytes", memory, "--timeout", experiment.Spec.Duration},
					}},
				},
			},
		},
	}

	if err := r.Create(ctx, &memoryStressJob); err != nil {
		logger.Error(err, "Failed to create memory stress job")
		return err
	}

	logger.Info("Memory stress job created", "memory", memory, "workers", workers, "duration", experiment.Spec.Duration)
	return nil
}

func (r *ChaosExperimentReconciler) increaseStress(ctx context.Context, pod *corev1.Pod, experiment *chaosv1alpha1.ChaosExperiment) error {
	logger := log.FromContext(ctx)
	logger.Info("Addding stress to Pod", "pod", pod.Name, "namespace", pod.Namespace)

	worker := experiment.Spec.CPUStress.Workers

	if worker <= 0 {
		worker = 1
	}

	ttl := int32(60) // Delete job 60 seconds after completion

	stressJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("chaos-stress-%s", pod.Name),
			Namespace: pod.Namespace,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeName:      pod.Spec.NodeName,
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:    "stress",
						Image:   "polinux/stress-ng:latest",
						Command: []string{"stress-ng", "--cpu", fmt.Sprintf("%d", worker), "--timeout", experiment.Spec.Duration},
					}},
				},
			},
		},
	}

	if err := r.Create(ctx, &stressJob); err != nil {
		logger.Error(err, "Failed to create stress job")
		return err
	}

	return nil
}

func (r *ChaosExperimentReconciler) addNetworkDelay(ctx context.Context, pod *corev1.Pod, experiment *chaosv1alpha1.ChaosExperiment) error {
	logger := log.FromContext(ctx)
	logger.Info("Adding network delay to Pod", "pod", pod.Name, "namespace", pod.Namespace)

	// Check if NetworkDelay spec is provided
	if experiment.Spec.NetworkDelay == nil {
		return fmt.Errorf("NetworkDelay configuration is required for network-delay chaos type")
	}

	latency := experiment.Spec.NetworkDelay.Latency
	if latency == "" {
		latency = "100ms" // Default latency
	}

	jitter := experiment.Spec.NetworkDelay.Jitter
	if jitter == "" {
		jitter = "10ms" // Default jitter
	}

	ttl := int32(60) // Delete job 60 seconds after completion

	// Build tc command to add network delay
	// tc qdisc add dev eth0 root netem delay <latency> <jitter>
	tcCommand := fmt.Sprintf("tc qdisc add dev eth0 root netem delay %s %s && sleep %s && tc qdisc del dev eth0 root || true",
		latency, jitter, experiment.Spec.Duration)

	// Create a Job that runs on the same node with host network and privileges
	networkDelayJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("chaos-network-delay-%s", pod.Name),
			Namespace: pod.Namespace,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeName:      pod.Spec.NodeName,
					HostNetwork:   true, // It can modify the node network interfaces and any network changes can affect other pods resides in that node
					HostPID:       true, //  Show all process running on that node
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:  "network-delay",
						Image: "nicolaka/netshoot:latest",
						SecurityContext: &corev1.SecurityContext{
							Privileged: func() *bool { b := true; return &b }(),
							Capabilities: &corev1.Capabilities{
								Add: []corev1.Capability{"NET_ADMIN"},
							},
						},
						Command: []string{"/bin/sh", "-c", tcCommand},
					}},
				},
			},
		},
	}

	if err := r.Create(ctx, &networkDelayJob); err != nil {
		logger.Error(err, "Failed to create network delay job")
		return err
	}

	logger.Info("Network delay job created", "latency", latency, "jitter", jitter, "duration", experiment.Spec.Duration)
	return nil
}

func (r *ChaosExperimentReconciler) killPod(ctx context.Context, pod *corev1.Pod) error {
	logger := log.FromContext(ctx)
	logger.Info("Killing pod", "pod", pod.Name, "namespace", pod.Namespace)

	return r.Delete(ctx, pod, &client.DeleteOptions{
		GracePeriodSeconds: new(int64), // Immediate deletion
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *ChaosExperimentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&chaosv1alpha1.ChaosExperiment{}).
		Complete(r)
}

func (r *ChaosExperimentReconciler) initializeExperiment(ctx context.Context, experiment *chaosv1alpha1.ChaosExperiment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Initializing chaos experiment", "name", experiment.Name)

	experiment.Status.Phase = "Pending" // Updating the phase.
	experiment.Status.Message = "Experiment initialized"

	if err := r.Status().Update(ctx, experiment); err != nil {
		return ctrl.Result{}, err
	}

	// After updating the phase , k8s controller runtime to immedaitely re-concile this same resources.
	return ctrl.Result{}, nil
}
