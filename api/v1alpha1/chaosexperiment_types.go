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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ChaosExperimentSpec defines the desired state of ChaosExperiment
type ChaosExperimentSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of ChaosExperiment. Edit chaosexperiment_types.go to remove/update
	Type string `json:"type"`

	Selector map[string]string `json:"selector"`

	Duration string `json:"duration"`

	TargetNamespace string `json:"targetNamespace,omitempty"`

	// Percentage of pods to affect (1-100)
	Percentage int `json:"percentage,omitempty"`

	// Network delay configuration (for network-delay type)
	NetworkDelay *NetworkDelaySpec `json:"networkDelay,omitempty"`

	// CPU stress configuration (for cpu-stress type)
	CPUStress *CPUStressSpec `json:"cpuStress,omitempty"`

	// Memory stress configuration (for memory-stress type)
	MemoryStress *MemoryStressSpec `json:"memoryStress,omitempty"`

	// Interval between chaos rounds (e.g., "30s", "1m")
	// If not set, chaos runs only once
	Interval string `json:"interval,omitempty"`
}

type MemoryStressSpec struct {
	// Amount of memory to consume (e.g., "256M", "512M", "1G")
	Memory string `json:"memory"`

	// Number of workers to spawn
	Workers int `json:"workers"`
}

type NetworkDelaySpec struct {
	// Latency in milliseconds
	Latency string `json:"latency"`
	// Jitter in milliseconds
	Jitter string `json:"jitter,omitempty"`
}

type CPUStressSpec struct {
	// Number of CPU workers
	Workers int `json:"workers"`
	// Load percentage per worker
	Load int `json:"load"`
}

// ChaosExperimentStatus defines the observed state of ChaosExperiment
type ChaosExperimentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Phase of the experiment (Pending, Running, Completed, Failed)
	Phase string `json:"phase,omitempty"`

	// Start time of the experiment
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// End time of the experiment
	EndTime *metav1.Time `json:"endTime,omitempty"`

	// Affected pods
	AffectedPods []string `json:"affectedPods,omitempty"`

	// Message with additional information
	Message string `json:"message,omitempty"`

	// Number of chaos rounds executed
	ChaosRounds int `json:"chaosRounds,omitempty"`

	// Last chaos execution time
	LastChaosTime *metav1.Time `json:"lastChaosTime,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed ChaosExperiment spec
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ChaosExperiment is the Schema for the chaosexperiments API
type ChaosExperiment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ChaosExperimentSpec   `json:"spec,omitempty"`
	Status ChaosExperimentStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ChaosExperimentList contains a list of ChaosExperiment
type ChaosExperimentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ChaosExperiment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ChaosExperiment{}, &ChaosExperimentList{})
}
