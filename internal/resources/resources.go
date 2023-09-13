/*
Copyright 2023 DragonflyDB authors.

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

package resources

import (
	"context"
	"fmt"

	resourcesv1 "github.com/dragonflydb/dragonfly-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// GetDragonflyResources returns the resources required for a Dragonfly
// Instance
func GetDragonflyResources(ctx context.Context, df *resourcesv1.Dragonfly) ([]client.Object, error) {
	log := log.FromContext(ctx)
	log.Info(fmt.Sprintf("Creating resources for %s", df.Name))

	var resources []client.Object

	image := df.Spec.Image
	if image == "" {
		image = fmt.Sprintf("%s:%s", DragonflyImage, Version)
	}

	// Create a StatefulSet, Headless Service
	statefulset := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      df.Name,
			Namespace: df.Namespace,
			// Useful for automatically deleting the resources when the Dragonfly object is deleted
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: df.APIVersion,
					Kind:       df.Kind,
					Name:       df.Name,
					UID:        df.UID,
				},
			},
			Labels: map[string]string{
				KubernetesAppComponentLabelKey: "dragonfly",
				KubernetesAppInstanceNameLabel: df.Name,
				KubernetesAppNameLabelKey:      "dragonfly",
				KubernetesAppVersionLabelKey:   Version,
				KubernetesPartOfLabelKey:       "dragonfly",
				KubernetesManagedByLabelKey:    DragonflyOperatorName,
				"app":                          df.Name,
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &df.Spec.Replicas,
			ServiceName: df.Name,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":                     df.Name,
					KubernetesPartOfLabelKey:  "dragonfly",
					KubernetesAppNameLabelKey: "dragonfly",
				},
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":                     df.Name,
						KubernetesPartOfLabelKey:  "dragonfly",
						KubernetesAppNameLabelKey: "dragonfly",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "dragonfly",
							Image: image,
							Ports: []corev1.ContainerPort{
								{
									Name:          DragonflyPortName,
									ContainerPort: DragonflyPort,
								},
							},
							Args: DefaultDragonflyArgs,
							Env:  df.Spec.Env,
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{
											"/bin/sh",
											"/usr/local/bin/healthcheck.sh",
										},
									},
								},
								FailureThreshold:    3,
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
								SuccessThreshold:    1,
								TimeoutSeconds:      5,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{
											"/bin/sh",
											"/usr/local/bin/healthcheck.sh",
										},
									},
								},
								FailureThreshold:    3,
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
								SuccessThreshold:    1,
								TimeoutSeconds:      5,
							},
							ImagePullPolicy: corev1.PullAlways,
						},
					},
				},
			},
		},
	}

	var memoryRequest *resource.Quantity

	// set only if resources are specified
	if df.Spec.Resources != nil {
		statefulset.Spec.Template.Spec.Containers[0].Resources = *df.Spec.Resources

		if mem := df.Spec.Resources.Requests.Memory(); mem != nil {
			memoryRequest = mem
		} else if mem := df.Spec.Resources.Limits.Memory(); mem != nil {
			memoryRequest = mem
		}
	}

	if df.Spec.Args != nil {
		statefulset.Spec.Template.Spec.Containers[0].Args = append(statefulset.Spec.Template.Spec.Containers[0].Args, df.Spec.Args...)
	}

	if df.Spec.Annotations != nil {
		statefulset.Spec.Template.ObjectMeta.Annotations = df.Spec.Annotations
	}

	if df.Spec.Affinity != nil {
		statefulset.Spec.Template.Spec.Affinity = df.Spec.Affinity
	}

	if df.Spec.Tolerations != nil {
		statefulset.Spec.Template.Spec.Tolerations = df.Spec.Tolerations
	}

	if df.Spec.ServiceAccountName != "" {
		statefulset.Spec.Template.Spec.ServiceAccountName = df.Spec.ServiceAccountName
	}

	if p := df.Spec.Persistence; p != nil {
		volumeName := "data"

		if memoryRequest != nil {
			minStorage := int64(float64(memoryRequest.Value()) * 1.5)

			if p.Size.Value() < minStorage {
				return nil, fmt.Errorf("storage request must be at least 1.5x the memory request (or limit if request is not set)")
			}
		}

		statefulset.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: volumeName,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					StorageClassName: &p.StorageClass,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: p.Size,
						},
					},
				},
			},
		}

		for i, c := range statefulset.Spec.Template.Spec.Containers {
			c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: "/data",
			})

			statefulset.Spec.Template.Spec.Containers[i] = c
		}
	}

	resources = append(resources, &statefulset)

	service := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      df.Name,
			Namespace: df.Namespace,
			// Useful for automatically deleting the resources when the Dragonfly object is deleted
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: df.APIVersion,
					Kind:       df.Kind,
					Name:       df.Name,
					UID:        df.UID,
				},
			},
			Labels: map[string]string{
				KubernetesAppComponentLabelKey: "Dragonfly",
				KubernetesAppInstanceNameLabel: df.Name,
				KubernetesAppNameLabelKey:      "dragonfly",
				KubernetesAppVersionLabelKey:   Version,
				KubernetesPartOfLabelKey:       "dragonfly",
				KubernetesManagedByLabelKey:    DragonflyOperatorName,
				"app":                          df.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app":                     df.Name,
				KubernetesAppNameLabelKey: "dragonfly",
				Role:                      Master,
			},
			Ports: []corev1.ServicePort{
				{
					Name: DragonflyPortName,
					Port: DragonflyPort,
				},
			},
		},
	}

	resources = append(resources, &service)

	return resources, nil
}
