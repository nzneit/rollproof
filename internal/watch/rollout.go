/*
Copyright 2016 The Kubernetes Authors.

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

// The rules of Complete are copied from
// k8s.io/kubectl/pkg/polymorphichelpers/rollout_status.go at kubectl v0.37.0
// (Kubernetes v1.37.0), with two changes: they take typed objects, and where
// kubectl returns an error, Complete answers "not complete". Read them again
// at every Kubernetes minor; TestCompleteAgreesWithKubectl compares them.

package watch

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Complete reports whether the rollout of a Deployment, StatefulSet or DaemonSet is complete, by
// the rule that kubectl rollout status uses for its kind, and a sentence that says what is still
// missing. A Deployment past its progress deadline is not complete and is not an error.
func Complete(obj runtime.Object) (bool, string) {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		return deploymentStatus(o)
	case *appsv1.StatefulSet:
		return statefulSetStatus(o)
	case *appsv1.DaemonSet:
		return daemonSetStatus(o)
	}
	return false, "not a watched kind"
}

// Rolling reports whether the workload updates by rolling. A Deployment always does, with either
// of its strategies; a StatefulSet or DaemonSet with OnDelete does not.
func Rolling(obj runtime.Object) bool {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		return true
	case *appsv1.StatefulSet:
		return o.Spec.UpdateStrategy.Type == appsv1.RollingUpdateStatefulSetStrategyType
	case *appsv1.DaemonSet:
		return o.Spec.UpdateStrategy.Type == appsv1.RollingUpdateDaemonSetStrategyType
	}
	return false
}

// deploymentStatus is DeploymentStatusViewer.Status. kubectl first compares a revision when one is
// asked for; Complete asks for none, as kubectl rollout status does without --revision.
func deploymentStatus(deployment *appsv1.Deployment) (bool, string) {
	if deployment.Generation <= deployment.Status.ObservedGeneration {
		cond := getDeploymentCondition(deployment.Status, appsv1.DeploymentProgressing)
		if cond != nil && cond.Reason == timedOutReason {
			// kubectl returns the error "deployment NAME exceeded its progress deadline" here.
			return false, fmt.Sprintf("deployment %q exceeded its progress deadline (condition %s, reason %s): %s", deployment.Name, cond.Type, cond.Reason, cond.Message)
		}
		if deployment.Spec.Replicas != nil && deployment.Status.UpdatedReplicas < *deployment.Spec.Replicas {
			return false, fmt.Sprintf("Waiting for deployment %q rollout to finish: %d out of %d new replicas have been updated...", deployment.Name, deployment.Status.UpdatedReplicas, *deployment.Spec.Replicas)
		}
		if deployment.Status.Replicas > deployment.Status.UpdatedReplicas {
			return false, fmt.Sprintf("Waiting for deployment %q rollout to finish: %d old replicas are pending termination...", deployment.Name, deployment.Status.Replicas-deployment.Status.UpdatedReplicas)
		}
		if deployment.Status.AvailableReplicas < deployment.Status.UpdatedReplicas {
			return false, fmt.Sprintf("Waiting for deployment %q rollout to finish: %d of %d updated replicas are available...", deployment.Name, deployment.Status.AvailableReplicas, deployment.Status.UpdatedReplicas)
		}
		return true, fmt.Sprintf("deployment %q successfully rolled out", deployment.Name)
	}
	return false, "Waiting for deployment spec update to be observed..."
}

// daemonSetStatus is DaemonSetStatusViewer.Status.
func daemonSetStatus(daemon *appsv1.DaemonSet) (bool, string) {
	if daemon.Spec.UpdateStrategy.Type != appsv1.RollingUpdateDaemonSetStrategyType {
		// kubectl returns done true with this text as its error.
		return false, fmt.Sprintf("rollout status is only available for %s strategy type", appsv1.RollingUpdateStatefulSetStrategyType)
	}
	if daemon.Generation <= daemon.Status.ObservedGeneration {
		if daemon.Status.UpdatedNumberScheduled < daemon.Status.DesiredNumberScheduled {
			return false, fmt.Sprintf("Waiting for daemon set %q rollout to finish: %d out of %d new pods have been updated...", daemon.Name, daemon.Status.UpdatedNumberScheduled, daemon.Status.DesiredNumberScheduled)
		}
		if daemon.Status.NumberAvailable < daemon.Status.DesiredNumberScheduled {
			return false, fmt.Sprintf("Waiting for daemon set %q rollout to finish: %d of %d updated pods are available...", daemon.Name, daemon.Status.NumberAvailable, daemon.Status.DesiredNumberScheduled)
		}
		return true, fmt.Sprintf("daemon set %q successfully rolled out", daemon.Name)
	}
	return false, "Waiting for daemon set spec update to be observed..."
}

// statefulSetStatus is StatefulSetStatusViewer.Status.
func statefulSetStatus(sts *appsv1.StatefulSet) (bool, string) {
	if sts.Spec.UpdateStrategy.Type != appsv1.RollingUpdateStatefulSetStrategyType {
		// kubectl returns done true with this text as its error.
		return false, fmt.Sprintf("rollout status is only available for %s strategy type", appsv1.RollingUpdateStatefulSetStrategyType)
	}
	if sts.Status.ObservedGeneration == 0 || sts.Generation > sts.Status.ObservedGeneration {
		return false, "Waiting for statefulset spec update to be observed..."
	}
	if sts.Spec.Replicas != nil && sts.Status.ReadyReplicas < *sts.Spec.Replicas {
		return false, fmt.Sprintf("Waiting for %d pods to be ready...", *sts.Spec.Replicas-sts.Status.ReadyReplicas)
	}
	if sts.Spec.UpdateStrategy.Type == appsv1.RollingUpdateStatefulSetStrategyType && sts.Spec.UpdateStrategy.RollingUpdate != nil {
		if sts.Spec.Replicas != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
			if sts.Status.UpdatedReplicas < (*sts.Spec.Replicas - *sts.Spec.UpdateStrategy.RollingUpdate.Partition) {
				return false, fmt.Sprintf("Waiting for partitioned roll out to finish: %d out of %d new pods have been updated...",
					sts.Status.UpdatedReplicas, *sts.Spec.Replicas-*sts.Spec.UpdateStrategy.RollingUpdate.Partition)
			}
		}
		return true, fmt.Sprintf("partitioned roll out complete: %d new pods have been updated...",
			sts.Status.UpdatedReplicas)
	}
	if sts.Status.UpdateRevision != sts.Status.CurrentRevision {
		return false, fmt.Sprintf("waiting for statefulset rolling update to complete %d pods at revision %s...",
			sts.Status.UpdatedReplicas, sts.Status.UpdateRevision)
	}
	return true, fmt.Sprintf("statefulset rolling update complete %d pods at revision %s...", sts.Status.CurrentReplicas, sts.Status.CurrentRevision)
}

// timedOutReason and getDeploymentCondition are copied from
// k8s.io/kubectl/pkg/util/deployment/deployment.go at kubectl v0.37.0, under the
// licence header above.

// timedOutReason is added in a deployment when its newest replica set fails to show any progress
// within the given deadline (progressDeadlineSeconds).
const timedOutReason = "ProgressDeadlineExceeded"

// getDeploymentCondition returns the condition with the provided type.
func getDeploymentCondition(status appsv1.DeploymentStatus, condType appsv1.DeploymentConditionType) *appsv1.DeploymentCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}
