package helpers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

func GetPodCondition(conditions []corev1.PodCondition, conditionType corev1.PodConditionType) *corev1.PodCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}

	return nil
}

func IsPodReady(pod *corev1.Pod) bool {
	condition := GetPodCondition(pod.Status.Conditions, corev1.PodReady)
	return condition != nil && condition.Status == corev1.ConditionTrue
}

func IsPodReadyWithPositiveLiveCheck(ctx context.Context, client corev1client.PodsGetter, pod *corev1.Pod) (bool, *corev1.Pod, error) {
	if !IsPodReady(pod) {
		return false, pod, nil
	}

	// Verify readiness with a live call.
	fresh, err := client.Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		return false, pod, err
	}

	return IsPodReady(fresh), fresh, nil
}
