package eviction

import (
	"k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// EvictionKind is kind of Eviction resource
	EvictionKind = "Eviction"
	// EvictionSubresource is path of Eviction resource
	EvictionSubresource = "pods/eviction"
)

// SupportEviction uses Discovery API to find out if the server support eviction subresource
// If support, it will return its groupVersion; Otherwise, it will return ""
// Copied from k8s.io/kubernetes/pkg/kubectl/cmd/drain.SupportEviction
func SupportEviction(clientset kubernetes.Interface) (string, error) {
	discoveryClient := clientset.Discovery()
	groupList, err := discoveryClient.ServerGroups()
	if err != nil {
		return "", err
	}
	foundPolicyGroup := false
	var policyGroupVersion string
	for _, group := range groupList.Groups {
		if group.Name == "policy" {
			foundPolicyGroup = true
			policyGroupVersion = group.PreferredVersion.GroupVersion
			break
		}
	}
	if !foundPolicyGroup {
		return "", nil
	}
	resourceList, err := discoveryClient.ServerResourcesForGroupVersion("v1")
	if err != nil {
		return "", err
	}
	for _, resource := range resourceList.APIResources {
		if resource.Name == EvictionSubresource && resource.Kind == EvictionKind {
			return policyGroupVersion, nil
		}
	}
	return "", nil
}

// EvictPod calls eviction API for given pod.
func EvictPod(clientset kubernetes.Interface, pod *v1.Pod, policyGroupVersion string, gracePeriodSeconds int64) error {
	deleteOptions := &metav1.DeleteOptions{}
	deleteOptions.GracePeriodSeconds = &gracePeriodSeconds
	eviction := &policyv1beta1.Eviction{
		TypeMeta: metav1.TypeMeta{
			APIVersion: policyGroupVersion,
			Kind:       EvictionKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		DeleteOptions: deleteOptions,
	}
	return clientset.PolicyV1beta1().Evictions(eviction.Namespace).Evict(eviction)
}
